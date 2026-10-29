# generate.ps1: the sample report (decision 2026-10-29, informe de ejemplo;
# BV-112). The released rosmarops.exe runs on a fictitious computer, the
# testdata/sample-home fixture copied to C:\Users\sample, and the report it
# writes is copied to dist/ in English and Spanish, with the language link
# pointing to the other published page and the sample band at the top. Then it checks the report: the
# expected findings are there, no generated secret appears in full, and the
# published pages neither name the runner nor show anything from the
# runner's Defender, scheduled tasks or global packages. Any failed check
# fails the script.
#
# It writes only under C:\Users\sample and dist/. The fake secrets are made
# here, with the formats RosmarOps' matchers know, and never written to the
# repository: the fixture holds __SAMPLE_*__ placeholders.
#
#   pwsh scripts/sample-report/generate.ps1 -Exe dist\rosmarops.exe
param(
    [Parameter(Mandatory = $true)][string]$Exe,
    [string]$SampleHome = 'C:\Users\sample',
    [string]$Fixture = (Join-Path $PSScriptRoot '..\..\testdata\sample-home'),
    [string]$OutDir = (Join-Path $PSScriptRoot '..\..\dist')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Exe = (Resolve-Path $Exe).Path
$Fixture = (Resolve-Path $Fixture).Path
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = (Resolve-Path $OutDir).Path
if (Test-Path $SampleHome) {
    throw "$SampleHome already exists: the sample computer is made from scratch"
}

# The runner's own names, read before the environment is redirected: the
# report must not carry them.
$runnerNames = @($env:COMPUTERNAME, $env:USERNAME, 'runneradmin', 'D:\a\', 'D:/a/', $env:GITHUB_WORKSPACE) |
    Where-Object { $_ }

# --- The sample computer ---------------------------------------------------

Copy-Item -Path $Fixture -Destination $SampleHome -Recurse -Force
Get-ChildItem -Path $SampleHome -Recurse -Force -Filter '.gitkeep' | Remove-Item -Force

# A fake secret: prefix + n random letters and digits.
function New-Fake([string]$prefix, [int]$n) {
    $chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
    $bytes = [byte[]]::new($n)
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    $prefix + -join ($bytes | ForEach-Object { $chars[$_ % $chars.Length] })
}

$secrets = [ordered]@{
    '__SAMPLE_GITHUB_TOKEN__'      = New-Fake 'ghp_' 36
    '__SAMPLE_POSTGRES_PASSWORD__' = New-Fake '' 20
    '__SAMPLE_OPENAI_KEY__'        = New-Fake 'sk-proj-' 56
    '__SAMPLE_STRIPE_KEY__'        = New-Fake 'sk_live_' 32
}
$utf8 = [System.Text.UTF8Encoding]::new($false)
foreach ($rel in '.claude.json', '.cursor\mcp.json', 'AppData\Local\hermes\.env', 'projects\shop-api\.env') {
    $path = Join-Path $SampleHome $rel
    $text = [System.IO.File]::ReadAllText($path)
    foreach ($k in $secrets.Keys) { $text = $text.Replace($k, $secrets[$k]) }
    if ($text -match '__SAMPLE_[A-Z_]+__') { throw "placeholder left in $rel" }
    [System.IO.File]::WriteAllText($path, $text, $utf8)
}
[System.IO.File]::WriteAllText((Join-Path $SampleHome 'projects\shop-api\.gitignore'), ".venv/`n", $utf8)
New-Item -ItemType Directory -Force -Path (Join-Path $SampleHome 'AppData\Local\Temp') | Out-Null
$sampleExe = Join-Path $SampleHome 'Downloads\rosmarops.exe'
Copy-Item -Path $Exe -Destination $sampleExe

# --- The environment: everything points into the sample computer ----------

$env:USERPROFILE = $SampleHome
$env:HOME = $SampleHome
$env:APPDATA = Join-Path $SampleHome 'AppData\Roaming'
$env:LOCALAPPDATA = Join-Path $SampleHome 'AppData\Local'
$env:ProgramFiles = Join-Path $SampleHome 'Program Files'
$env:npm_config_prefix = Join-Path $SampleHome 'AppData\Roaming\npm'
$env:XDG_CONFIG_HOME = Join-Path $SampleHome '.config'
$env:GH_CONFIG_DIR = Join-Path $SampleHome '.config\gh'
$env:AWS_SHARED_CREDENTIALS_FILE = Join-Path $SampleHome '.aws\credentials'
$env:TEMP = Join-Path $SampleHome 'AppData\Local\Temp'
$env:TMP = $env:TEMP
foreach ($name in 'CLAUDE_CONFIG_DIR', 'HERMES_HOME', 'CODEX_HOME', 'XDG_DATA_HOME', 'PNPM_HOME',
    'npm_config_global_dir', 'npm_config_userconfig') {
    Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
}

# --- The repositories: fixed identity and commit date ----------------------

$env:GIT_CONFIG_NOSYSTEM = '1'
$env:GIT_AUTHOR_DATE = '2026-10-20T10:00:00Z'
$env:GIT_COMMITTER_DATE = '2026-10-20T10:00:00Z'
function Invoke-Git([string]$repo) {
    & git -C $repo -c core.autocrlf=false -c init.defaultBranch=main -c commit.gpgsign=false `
        -c 'user.name=Sample Developer' -c 'user.email=sample@example.com' @args
    if ($LASTEXITCODE -ne 0) { throw "git $args failed in $repo" }
}
foreach ($name in 'shop-api', 'landing-page') {
    $repo = Join-Path $SampleHome "projects\$name"
    Invoke-Git $repo init -q
    Invoke-Git $repo add -A
    Invoke-Git $repo commit -q -m 'Initial commit'
    Invoke-Git $repo remote add origin "https://git.example.com/sample/$name.git"
}
foreach ($name in 'GIT_CONFIG_NOSYSTEM', 'GIT_AUTHOR_DATE', 'GIT_COMMITTER_DATE') {
    Remove-Item -Path "Env:$name"
}

# --- The run: init, baseline, run, with the same rosmarops.exe -------------

Push-Location $SampleHome
try {
    foreach ($argv in @(
            @('init', '--repos-under', (Join-Path $SampleHome 'projects')),
            @('baseline'),
            @('run', '--lang', 'en'))) {
        & $sampleExe @argv
        if ($LASTEXITCODE -ne 0) { throw "rosmarops $($argv -join ' '): exit $LASTEXITCODE" }
    }
}
finally {
    Pop-Location
}

$reports = Join-Path $SampleHome '.rosmarops\reports'
$published = [ordered]@{ en = (Join-Path $OutDir 'sample-report.en.html'); es = (Join-Path $OutDir 'sample-report.es.html') }
Copy-Item -Path (Join-Path $reports 'report.en.html') -Destination $published.en
Copy-Item -Path (Join-Path $reports 'report.es.html') -Destination $published.es

# The links of the published pages: the language link goes to the other
# published page (exactly one replacement per page), and any other relative
# href, to a file that is not published (.md, .json, .ai.md…), loses its
# href and keeps its text. Nothing else in the HTML changes.
$v1Files = @(Get-ChildItem -Path $reports -Filter 'report-*.json')
if ($v1Files.Count -ne 1) { throw "$($v1Files.Count) report-*.json in $reports, want 1" }
$base = $v1Files[0].BaseName # report-<date>__<STATUS>__<counts>, the pages' base
$unlinked = [System.Collections.Generic.List[string]]::new()
foreach ($lang in $published.Keys) {
    $other = if ($lang -eq 'en') { 'es' } else { 'en' }
    $page = [System.IO.File]::ReadAllText($published[$lang])
    $from = "href=`"$base.$other.html`""
    $count = [regex]::Matches($page, [regex]::Escape($from)).Count
    if ($count -ne 1) { throw "sample-report.$lang.html: $count language links to $base.$other.html, want 1" }
    $page = $page.Replace($from, "href=`"sample-report.$other.html`"")
    $page = [regex]::Replace($page, '\shref="(?![a-zA-Z][a-zA-Z0-9+.-]*:|#|//|sample-report\.(?:en|es)\.html")([^"]*)"', {
            param($m)
            $unlinked.Add("sample-report.$($lang).html: $($m.Groups[1].Value)")
            ''
        })
    [System.IO.File]::WriteAllText($published[$lang], $page, $utf8)
}
Write-Output 'Links to files that are not published, href removed:'
if ($unlinked.Count -eq 0) { Write-Output '  (none)' } else { $unlinked | ForEach-Object { Write-Output "  $_" } }

# The sample band (decision 2026-10-29, informe de ejemplo, §2: the label is
# always visible): the first child of <body>, which must appear exactly once.
$bandStyle = "background:#1B1A18;color:#FFFFFF;font:600 14px/1.4 system-ui,-apple-system,'Segoe UI',sans-serif;padding:10px 16px;text-align:center"
$bands = [ordered]@{
    en = "<div role=`"note`" style=`"$bandStyle`">Sample report, made with example data: the computer, the user, the projects and the keys are invented.</div>"
    es = "<div role=`"note`" style=`"$bandStyle`">Informe de ejemplo, hecho con datos de ejemplo: el ordenador, el usuario, los proyectos y las claves son inventados.</div>"
}
foreach ($lang in $published.Keys) {
    $page = [System.IO.File]::ReadAllText($published[$lang])
    $count = [regex]::Matches($page, '<body>').Count
    if ($count -ne 1) { throw "sample-report.$lang.html: <body> appears $count times, want 1" }
    [System.IO.File]::WriteAllText($published[$lang], $page.Replace('<body>', '<body>' + $bands[$lang]), $utf8)
}

# --- The checks ------------------------------------------------------------

$failures = [System.Collections.Generic.List[string]]::new()
$v2 = Get-Content -Raw -Path (Join-Path $reports 'report.v2.json') | ConvertFrom-Json
$enHTML = [System.IO.File]::ReadAllText((Join-Path $OutDir 'sample-report.en.html'))

Write-Output 'Findings (report.v2.json):'
$v2.findings | Group-Object -Property severity, rule | Sort-Object Name |
    ForEach-Object { Write-Output ("  {0,3}  {1}" -f $_.Count, $_.Name) }

# 1. The expected findings: the rule in report.v2.json and its evidence in
# the English HTML.
$expected = @(
    @{ What = 'GitHub token in the MCP server'; Rule = 'cred_read_and_send'; HTML = 'mcpServers/github/env/GITHUB_PERSONAL_ACCESS_TOKEN' },
    @{ What = '.env in git'; Rule = 'cred_in_git'; HTML = 'shop-api[\\/]\.env' },
    @{ What = 'project hook runs a repository script'; Rule = 'project_hook_runs_repo_script'; HTML = '\.github/init\.js' },
    @{ What = 'known malicious package'; Rule = 'package_known_malicious'; HTML = 'claudeopuseni 0\.1\.1' },
    @{ What = 'writable shared skill'; Rule = 'foreign_agent_instructions_writable'; HTML = '\.agents[\\/]skills[\\/]deploy[\\/]SKILL\.md' }
)
foreach ($e in $expected) {
    if (-not ($v2.findings | Where-Object { $_.rule -eq $e.Rule })) { $failures.Add("no $($e.Rule) finding ($($e.What))") }
    if ($enHTML -notmatch $e.HTML) { $failures.Add("the English HTML does not show $($e.What) ($($e.HTML))") }
}

# 1b. Another agent can change shop-api's CLAUDE.md: a
# foreign_agent_instructions_writable finding whose evidence holds that file
# (its node in graph.v2.json), shown in the English HTML.
$graph = Get-Content -Raw -Path (Join-Path $reports 'graph.v2.json') | ConvertFrom-Json
$claudeMD = Join-Path $SampleHome 'projects\shop-api\CLAUDE.md'
$claudeMDNodes = @($graph.nodes | Where-Object { $_.PSObject.Properties['path'] -and $_.path -eq $claudeMD } | ForEach-Object { $_.id })
$onClaudeMD = $v2.findings | Where-Object {
    $f = $_
    $f.rule -eq 'foreign_agent_instructions_writable' -and @($f.evidence.nodes | Where-Object { $claudeMDNodes -contains $_ }).Count -gt 0
}
if (-not $onClaudeMD) { $failures.Add("no foreign_agent_instructions_writable finding on $claudeMD") }
if ($enHTML -notmatch 'shop-api[\\/]CLAUDE\.md') { $failures.Add('the English HTML does not show shop-api\CLAUDE.md') }

# 1c. Claude Desktop is detected, not evaluated, with no note: listed in the
# index's not_evaluated and in the English HTML, and in no coverage gap.
if (@($v2.index.scope.not_evaluated) -notcontains 'Claude Desktop') { $failures.Add('Claude Desktop is not among the detected agents') }
if ($enHTML -notmatch 'Detected, not evaluated yet: [^<]*Claude Desktop') { $failures.Add('the English HTML does not list Claude Desktop as detected') }
$gaps = @($v2.coverage.sources_failed) + @($v2.coverage.agents_unsupported) + @($v2.coverage.scan_errors) | Where-Object { $_ }
if ($gaps | Where-Object { ($_ | ConvertTo-Json -Compress) -match 'Claude' }) { $failures.Add('a coverage note names Claude Desktop') }

# 2. No generated secret in full in anything RosmarOps wrote or in dist/.
$outputs = @(Get-ChildItem -Path (Join-Path $SampleHome '.rosmarops') -Recurse -File) + @(Get-Item $published.en, $published.es)
foreach ($f in $outputs) {
    $text = [System.IO.File]::ReadAllText($f.FullName)
    foreach ($k in $secrets.Keys) {
        if ($text.Contains($secrets[$k])) { $failures.Add("$($f.Name) contains the generated secret $k") }
    }
}

# 3 and 4, on the two published pages (decision of the advisor, BV-112): no
# name of the runner, and nothing from its Defender, scheduled tasks, global
# packages, shell profiles or VS Code extensions. Those are older checks:
# the technical report (.md, .json) lists them, the HTML has no section for
# them, and only the HTML is published.
$v1 = $v1Files[0]
$machine = @((Get-Content -Raw -Path $v1.FullName | ConvertFrom-Json).findings |
        Where-Object { $_.category -match '^(av_exclusion|scheduled_task|global_package|npmrc|shell_profile|vscode_extension)' })
Write-Output "Findings from the runner in the technical report, not published: $($machine.Count)"
$machine | ForEach-Object { Write-Output "  $($_.category): $($_.title)" }
foreach ($lang in $published.Keys) {
    $page = [System.IO.File]::ReadAllText($published[$lang])
    if (-not $page.Contains('<body>' + $bands[$lang])) { $failures.Add("sample-report.$lang.html does not open with the sample band") }
    foreach ($n in $runnerNames) {
        if ($page.IndexOf($n, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) { $failures.Add("sample-report.$lang.html names the runner ($n)") }
    }
    foreach ($m in $machine) {
        $detail = ($m.title -split ': ', 2)[-1]
        if ($page.Contains($m.category) -or ($detail -and $page.Contains($detail))) {
            $failures.Add("sample-report.$lang.html shows a finding from the runner: $($m.category) $($m.title)")
        }
    }
}

if ($failures.Count -gt 0) {
    $failures | ForEach-Object { Write-Output "FAIL: $_" }
    exit 1
}
Write-Output "Sample report checked: $OutDir\sample-report.en.html, $OutDir\sample-report.es.html"
