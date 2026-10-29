# generate.ps1: the sample report (decision 2026-10-29, informe de ejemplo;
# BV-112). The released rosmarops.exe runs on a fictitious computer, the
# testdata/sample-home fixture copied to C:\Users\sample, and the report it
# writes is copied to dist/ in English and Spanish. Then it checks the
# report: the expected findings are there, no generated secret appears in
# full, nothing names the runner, and nothing comes from the runner's
# Defender, scheduled tasks or global packages. Any failed check fails the
# script.
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
Copy-Item -Path (Join-Path $reports 'report.en.html') -Destination (Join-Path $OutDir 'sample-report.en.html')
Copy-Item -Path (Join-Path $reports 'report.es.html') -Destination (Join-Path $OutDir 'sample-report.es.html')

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

# 2 and 3. No generated secret in full, and nothing that names the runner,
# in any file RosmarOps wrote or in dist/.
$outputs = @(Get-ChildItem -Path (Join-Path $SampleHome '.rosmarops') -Recurse -File) +
    @(Get-Item (Join-Path $OutDir 'sample-report.en.html'), (Join-Path $OutDir 'sample-report.es.html'))
foreach ($f in $outputs) {
    $text = [System.IO.File]::ReadAllText($f.FullName)
    foreach ($k in $secrets.Keys) {
        if ($text.Contains($secrets[$k])) { $failures.Add("$($f.Name) contains the generated secret $k") }
    }
    foreach ($n in $runnerNames) {
        if ($text.IndexOf($n, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) { $failures.Add("$($f.Name) names the runner ($n)") }
    }
}

# 4. Nothing from the runner's Defender, scheduled tasks, global packages,
# shell profiles or VS Code extensions (the baseline recorded them).
$v1 = Get-ChildItem -Path $reports -Filter 'report-*.json' | Select-Object -First 1
$machine = (Get-Content -Raw -Path $v1.FullName | ConvertFrom-Json).findings |
    Where-Object { $_.category -match '^(av_exclusion|scheduled_task|global_package|npmrc|shell_profile|vscode_extension)' }
foreach ($m in $machine) { $failures.Add("finding from the runner: $($m.category) $($m.title)") }

if ($failures.Count -gt 0) {
    $failures | ForEach-Object { Write-Output "FAIL: $_" }
    exit 1
}
Write-Output "Sample report checked: $OutDir\sample-report.en.html, $OutDir\sample-report.es.html"
