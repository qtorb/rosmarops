# make-home.ps1 (BV-157): build a realistic test computer under C:\Users\e2e,
# deterministically (fixed dates, authors and content, no network), for the
# post-publish end-to-end check. It only writes under the home directory it is
# given and the manifest file; it never runs rosmarops.exe and never touches the
# network. run.ps1 then runs the published rosmarops.exe on this computer.
#
# What it mounts under C:\Users\e2e:
#   - the same agents and traps as testdata/sample-home (Claude Code, Cursor,
#     Hermes, Claude Desktop, .mcp.json, shared skills, a project hook, fake
#     secrets, an .env committed to git, the malicious claudeopuseni in a .venv),
#     with the placeholders replaced by fixed fake secrets;
#   - in .cursor\mcp.json a pinned MCP server `npx -y @ifasn/md-doc-mcp@1.0.4`
#     (the engine matches npm by a pinned npx command, so the version is given);
#   - 48 git repositories directly under projects\ (config.ReposUnder counts the
#     immediate subdirectories that hold a .git, so the run finds exactly 48):
#       * shop-api and landing-page, the two sample projects (they carry the
#         agent configs and the .env-in-git), counted as 2 of the 42 "medium";
#       * 40 generated medium repos (100 commits, 200 files each);
#       * 6 generated large repos (3000 commits, 2000 files each), one with a
#         path longer than 180 characters, and a fake key in the history of 3.
#     The history is built with `git fast-import` (one process per repo), which
#     is the only way to make thousands of fixed-date commits in reasonable time.
#   - the fixed fake secrets are written to the manifest so run.ps1 can prove
#     none of them appears in full in any output. The manifest lives outside the
#     home, so it is never scanned.
#
# No kit is created here: `rosmarops kit create` needs the state that `init`
# writes, and `init` refuses to run twice, so the kit is created in run.ps1
# right after its own `init` (reported for BV-157).
#
#   pwsh scripts/e2e/make-home.ps1 -HomeDir C:\Users\e2e -Manifest <path>.json
param(
    [string]$HomeDir = 'C:\Users\e2e',
    [string]$Fixture = (Join-Path $PSScriptRoot '..\..\testdata\sample-home'),
    [string]$Manifest = (Join-Path ([System.IO.Path]::GetTempPath()) 'rosmarops-e2e-manifest.json'),
    [int]$MediumRepos = 40,
    [int]$LargeRepos = 6,
    [int]$MediumCommits = 100,
    [int]$MediumFiles = 200,
    [int]$LargeCommits = 3000,
    [int]$LargeFiles = 2000
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Fixture = (Resolve-Path $Fixture).Path
if (Test-Path $HomeDir) {
    throw "$HomeDir already exists: the test computer is made from scratch"
}
$utf8 = [System.Text.UTF8Encoding]::new($false)

# --- The sample computer (same layout as testdata/sample-home) --------------

Copy-Item -Path $Fixture -Destination $HomeDir -Recurse -Force
Get-ChildItem -Path $HomeDir -Recurse -Force -Filter '.gitkeep' | Remove-Item -Force

# Fixed fake secrets (deterministic): the prefixes RosmarOps' matchers know,
# with fixed bodies so the fixture is reproducible and run.ps1 can look for the
# exact values. None is a real credential.
$secrets = [ordered]@{
    '__SAMPLE_GITHUB_TOKEN__'      = 'ghp_' + ('E2e0' * 9)                       # 4 + 36
    '__SAMPLE_POSTGRES_PASSWORD__' = 'E2ePgPassw0rd00000000'                     # 20
    '__SAMPLE_OPENAI_KEY__'        = 'sk-proj-' + ('E2e0' * 14)                  # 8 + 56
    '__SAMPLE_STRIPE_KEY__'        = 'sk_live_' + ('E2e0' * 8)                   # 8 + 32
}
foreach ($rel in '.claude.json', '.cursor\mcp.json', 'AppData\Local\hermes\.env', 'projects\shop-api\.env') {
    $path = Join-Path $HomeDir $rel
    $text = [System.IO.File]::ReadAllText($path)
    foreach ($k in $secrets.Keys) { $text = $text.Replace($k, $secrets[$k]) }
    if ($text -match '__SAMPLE_[A-Z_]+__') { throw "placeholder left in $rel" }
    [System.IO.File]::WriteAllText($path, $text, $utf8)
}
[System.IO.File]::WriteAllText((Join-Path $HomeDir 'projects\shop-api\.gitignore'), ".venv/`n", $utf8)
New-Item -ItemType Directory -Force -Path (Join-Path $HomeDir 'AppData\Local\Temp') | Out-Null

# A pinned malicious MCP server in Cursor: @ifasn/md-doc-mcp@1.0.4 (intel
# MAL-2026-18486..18489). The engine matches npm by the pinned npx command.
$cursorPath = Join-Path $HomeDir '.cursor\mcp.json'
$cursor = Get-Content -Raw -Path $cursorPath | ConvertFrom-Json
$cursor.mcpServers | Add-Member -NotePropertyName 'md-doc' -NotePropertyValue ([ordered]@{
        command = 'npx'
        args    = @('-y', '@ifasn/md-doc-mcp@1.0.4')
    })
[System.IO.File]::WriteAllText($cursorPath, ($cursor | ConvertTo-Json -Depth 20), $utf8)

# --- Git identity for the two sample repos (fixed date, like the sample) ----

$env:GIT_CONFIG_NOSYSTEM = '1'
$env:GIT_AUTHOR_DATE = '2026-10-20T10:00:00Z'
$env:GIT_COMMITTER_DATE = '2026-10-20T10:00:00Z'
function Invoke-Git([string]$repo) {
    & git -C $repo -c core.autocrlf=false -c core.longpaths=true -c init.defaultBranch=main `
        -c commit.gpgsign=false -c 'user.name=Sample Developer' -c 'user.email=sample@example.com' @args
    if ($LASTEXITCODE -ne 0) { throw "git $args failed in $repo" }
}
foreach ($name in 'shop-api', 'landing-page') {
    $repo = Join-Path $HomeDir "projects\$name"
    Invoke-Git $repo init -q
    Invoke-Git $repo add -A
    Invoke-Git $repo commit -q -m 'Initial commit'
    Invoke-Git $repo remote add origin "https://git.example.com/sample/$name.git"
}
foreach ($name in 'GIT_AUTHOR_DATE', 'GIT_COMMITTER_DATE') { Remove-Item -Path "Env:$name" }

# --- The generated repositories, via git fast-import ------------------------

# A fast-import stream for one repository: $Commits commits on refs/heads/main,
# each adding or rewriting one of $Files files, with fixed authors and dates.
# When $PlantKey, one early commit adds a file with a fake AWS key and a later
# commit removes it, so the key is in the history but not at HEAD. When
# $LongPath, one file lives at a path longer than 180 characters.
$base = 1577836800 # 2020-01-01T00:00:00Z, fixed unix seconds
function New-Repo {
    param(
        [string]$Path,
        [int]$Commits,
        [int]$Files,
        [bool]$PlantKey = $false,
        [bool]$LongPath = $false,
        [string]$AwsKey = ''
    )
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
    & git -C $Path -c init.defaultBranch=main init -q
    if ($LASTEXITCODE -ne 0) { throw "git init failed in $Path" }
    & git -C $Path config core.longpaths true | Out-Null

    $longRel = (('d' * 10 + '/') * 17) + 'leaf.txt'  # 11*17 + 8 = 195 characters
    $sb = [System.Text.StringBuilder]::new()
    $mark = 0
    $prev = 0
    function Add-Data([System.Text.StringBuilder]$b, [string]$content) {
        $n = [System.Text.Encoding]::UTF8.GetByteCount($content)
        [void]$b.Append("data $n`n")
        [void]$b.Append($content)
        [void]$b.Append("`n")
    }
    for ($i = 1; $i -le $Commits; $i++) {
        $when = ($base + $i * 60).ToString() + ' +0000'

        # The file this commit writes, and its content.
        $idx = (($i - 1) % $Files) + 1
        $rel = "src/f{0:D4}.txt" -f $idx
        $content = "file $idx, revision at commit $i`n"
        $blobMark = ++$mark
        [void]$sb.Append("blob`nmark :$blobMark`n")
        Add-Data $sb $content

        # Optional extra blobs for this commit: the long path and the planted key.
        $extra = [System.Collections.Generic.List[string]]::new()
        if ($LongPath -and $i -eq 2) {
            $lm = ++$mark
            [void]$sb.Append("blob`nmark :$lm`n")
            Add-Data $sb "deep file at a path longer than 180 characters`n"
            $extra.Add("M 100644 :$lm $longRel")
        }
        if ($PlantKey -and $i -eq 5) {
            $km = ++$mark
            [void]$sb.Append("blob`nmark :$km`n")
            Add-Data $sb "aws_access_key_id = $AwsKey`n"
            $extra.Add("M 100644 :$km config/credentials.ini")
        }
        if ($PlantKey -and $i -eq 7) {
            $extra.Add("D config/credentials.ini")
        }

        $commitMark = ++$mark
        [void]$sb.Append("commit refs/heads/main`nmark :$commitMark`n")
        [void]$sb.Append("author Dev Tester <dev@example.com> $when`n")
        [void]$sb.Append("committer Dev Tester <dev@example.com> $when`n")
        Add-Data $sb "commit $i`n"
        if ($prev -ne 0) { [void]$sb.Append("from :$prev`n") }
        [void]$sb.Append("M 100644 :$blobMark $rel`n")
        foreach ($e in $extra) { [void]$sb.Append("$e`n") }
        [void]$sb.Append("`n")
        $prev = $commitMark
    }

    # Feed the stream to fast-import over stdin (ASCII content, so the byte
    # counts in `data` lines match exactly).
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = 'git'
    $psi.Arguments = "-C `"$Path`" fast-import --quiet --done"
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $p = [System.Diagnostics.Process]::Start($psi)
    $errTask = $p.StandardError.ReadToEndAsync()
    # Write raw ASCII bytes to the base stream: no BOM and no newline rewriting,
    # so the byte counts in the `data` lines match exactly (fast-import is strict).
    $bytes = [System.Text.Encoding]::ASCII.GetBytes($sb.ToString() + "done`n")
    $p.StandardInput.BaseStream.Write($bytes, 0, $bytes.Length)
    $p.StandardInput.BaseStream.Flush()
    $p.StandardInput.Close()
    $p.WaitForExit()
    $err = $errTask.Result
    if ($p.ExitCode -ne 0) { throw "git fast-import failed in ${Path}: $err" }

    & git -C $Path -c core.longpaths=true reset --hard main --quiet
    if ($LASTEXITCODE -ne 0) { throw "git reset --hard failed in $Path" }
}

$projects = Join-Path $HomeDir 'projects'
# Deterministic fake AWS keys for the 3 large repos with a key in history.
$awsKeys = @('AKIAE2E0EXAMPLE00001', 'AKIAE2E0EXAMPLE00002', 'AKIAE2E0EXAMPLE00003')

for ($n = 1; $n -le $MediumRepos; $n++) {
    New-Repo -Path (Join-Path $projects ("med-{0:D2}" -f $n)) -Commits $MediumCommits -Files $MediumFiles
}
for ($n = 1; $n -le $LargeRepos; $n++) {
    $plant = $n -le 3
    $aws = ''
    if ($plant) { $aws = $awsKeys[$n - 1] }
    New-Repo -Path (Join-Path $projects ("big-{0:D2}" -f $n)) -Commits $LargeCommits -Files $LargeFiles `
        -LongPath ($n -eq 1) -PlantKey $plant -AwsKey $aws
}

foreach ($name in 'GIT_CONFIG_NOSYSTEM') { Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue }

# --- The secret manifest (outside the home, never scanned) ------------------

$allSecrets = @()
$allSecrets += $secrets.Values
$allSecrets += $awsKeys
$manifestObj = [ordered]@{
    home     = $HomeDir
    projects = $projects
    expected = ($MediumRepos + $LargeRepos + 2)   # 48 git repos under projects\
    secrets  = $allSecrets
}
[System.IO.File]::WriteAllText($Manifest, ($manifestObj | ConvertTo-Json -Depth 10), $utf8)

$repoCount = @(Get-ChildItem -Path $projects -Directory | Where-Object { Test-Path (Join-Path $_.FullName '.git') }).Count
Write-Output "Test computer built at $HomeDir"
Write-Output "  git repositories under projects\: $repoCount (want $($manifestObj.expected))"
Write-Output "  manifest: $Manifest"
if ($repoCount -ne $manifestObj.expected) { throw "built $repoCount repos, want $($manifestObj.expected)" }
