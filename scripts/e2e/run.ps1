# run.ps1 (BV-157): run the published rosmarops.exe on the test computer that
# make-home.ps1 built, time each run, and fail — with a clear message — if the
# result repeats a failure like those of 02-11 (the space package tumbling the
# review) or 03-11 (the time limit). No network: nothing here touches
# app.rosmarops.com (invariant 1 unchanged); the computer is not connected (no
# device credential), so the run seals a package but sends nothing.
#
# The run order is: init --repos-under projects, create the kit, baseline,
# run --lang es, run --unattended. The kit is created here, not in make-home.ps1:
# `rosmarops kit create` needs the state that `init` writes, and `init` refuses
# to run twice. kit create is interactive (it shows a random recovery code and
# asks for its last 4 characters), so a plain stdin pipe cannot drive it; this
# script drives it with a small reader that sends an empty phrase (which makes
# the program generate one) and reads the recovery code back from stdout to
# answer the check. It uses only the published .exe — no code change, no change
# to invariant 1.
#
# No-network check: on windows-latest the runner needs its own outbound access,
# so removing routes is not an option. Instead this script watches the
# rosmarops.exe process with Get-NetTCPConnection while each run executes and
# fails if the process opens any non-loopback connection. (Choice stated for
# BV-157 point 2: Get-NetTCPConnection monitoring, not a routeless adapter.)
#
#   pwsh scripts/e2e/run.ps1 -Exe dist\rosmarops.exe -HomeDir C:\Users\e2e -Manifest <path>.json
param(
    [Parameter(Mandatory = $true)][string]$Exe,
    [string]$HomeDir = 'C:\Users\e2e',
    [string]$Manifest = (Join-Path ([System.IO.Path]::GetTempPath()) 'rosmarops-e2e-manifest.json'),
    # Prebuilt tests/e2e/openpkg helper. The workflow builds it before this
    # script redirects HOME/USERPROFILE, so opening the package needs no Go
    # module fetch (which would need the network). Empty -> `go run` (local use).
    [string]$OpenPkg = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Exe = (Resolve-Path $Exe).Path
if (-not (Test-Path $HomeDir)) { throw "$HomeDir does not exist: run make-home.ps1 first" }
$manifestObj = Get-Content -Raw -Path $Manifest | ConvertFrom-Json
$expectedProjects = [int]$manifestObj.expected
$projectsDir = Join-Path $HomeDir 'projects'

# --- The environment: everything points into the test computer --------------
# (the same redirection make-home's two sample repos were built under).

$env:USERPROFILE = $HomeDir
$env:HOME = $HomeDir
$env:APPDATA = Join-Path $HomeDir 'AppData\Roaming'
$env:LOCALAPPDATA = Join-Path $HomeDir 'AppData\Local'
$env:ProgramFiles = Join-Path $HomeDir 'Program Files'
$env:npm_config_prefix = Join-Path $HomeDir 'AppData\Roaming\npm'
$env:XDG_CONFIG_HOME = Join-Path $HomeDir '.config'
$env:GH_CONFIG_DIR = Join-Path $HomeDir '.config\gh'
$env:AWS_SHARED_CREDENTIALS_FILE = Join-Path $HomeDir '.aws\credentials'
$env:TEMP = Join-Path $HomeDir 'AppData\Local\Temp'
$env:TMP = $env:TEMP
foreach ($name in 'CLAUDE_CONFIG_DIR', 'HERMES_HOME', 'CODEX_HOME', 'XDG_DATA_HOME', 'PNPM_HOME',
    'npm_config_global_dir', 'npm_config_userconfig') {
    Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
}
New-Item -ItemType Directory -Force -Path $env:TEMP | Out-Null

$reports = Join-Path $HomeDir '.rosmarops\reports'
$log = Join-Path $HomeDir '.rosmarops\e2e.log'
$summary = [System.Collections.Generic.List[string]]::new()
$failures = [System.Collections.Generic.List[string]]::new()
# $detail describes the failure and is shown only when the check fails, so a
# PASS line never carries failure-phrased text.
function Check([string]$name, [bool]$ok, [string]$detail = '') {
    if ($ok) {
        $summary.Add("PASS  $name")
    } else {
        $summary.Add("FAIL  $name$(if ($detail) { " — $detail" })")
        $failures.Add("$name$(if ($detail) { " — $detail" })")
    }
}

# --- Running the exe while watching for any outbound connection -------------

function Invoke-Watched {
    param([string[]]$Argv, [string]$OutFile)
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $Exe
    foreach ($a in $Argv) { [void]$psi.ArgumentList.Add($a) }
    $psi.WorkingDirectory = $HomeDir
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $outTask = $null
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $p = [System.Diagnostics.Process]::Start($psi)
    $outTask = $p.StandardOutput.ReadToEndAsync()
    $errTask = $p.StandardError.ReadToEndAsync()
    $remote = [System.Collections.Generic.HashSet[string]]::new()
    while (-not $p.HasExited) {
        try {
            Get-NetTCPConnection -OwningProcess $p.Id -ErrorAction SilentlyContinue |
                Where-Object { $_.RemoteAddress -and $_.RemoteAddress -notin '127.0.0.1', '::1', '0.0.0.0', '::' } |
                ForEach-Object { [void]$remote.Add("$($_.RemoteAddress):$($_.RemotePort)") }
        } catch {}
        Start-Sleep -Milliseconds 100
    }
    $p.WaitForExit()
    $sw.Stop()
    $out = $outTask.Result
    $err = $errTask.Result
    if ($OutFile) { [System.IO.File]::WriteAllText($OutFile, $out + $err, ([System.Text.UTF8Encoding]::new($false))) }
    [pscustomobject]@{
        ExitCode = $p.ExitCode
        Stdout   = $out
        Stderr   = $err
        Seconds  = [math]::Round($sw.Elapsed.TotalSeconds, 1)
        Remote   = @($remote)
    }
}

function Assert-NoNetwork($res, [string]$phase) {
    if ($res.Remote.Count -gt 0) {
        Check "no network ($phase)" $false ("outbound connections: " + ($res.Remote -join ', '))
    } else {
        Check "no network ($phase)" $true
    }
}

# --- init --------------------------------------------------------------------

$r = Invoke-Watched -Argv @('init', '--repos-under', $projectsDir)
if ($r.ExitCode -ne 0) { throw "rosmarops init failed (exit $($r.ExitCode)): $($r.Stderr)" }
Assert-NoNetwork $r 'init'
if ($r.Stdout -match 'Repositories found under .*: (\d+)') {
    Check "init found $expectedProjects repos" ([int]$Matches[1] -eq $expectedProjects) "init reported $($Matches[1])"
} else {
    Check "init found $expectedProjects repos" $false 'init did not print a repository count'
}

# --- kit create (driven non-interactively) ----------------------------------

function New-Kit {
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $Exe
    foreach ($a in 'kit', 'create', '--lang', 'en') { [void]$psi.ArgumentList.Add($a) }
    $psi.WorkingDirectory = $HomeDir
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $p = [System.Diagnostics.Process]::Start($psi)
    $errTask = $p.StandardError.ReadToEndAsync()
    $ascii = [System.Text.Encoding]::ASCII
    function Send-Line($proc, [string]$s) {
        $b = $ascii.GetBytes($s + "`n")  # raw ASCII, no BOM
        $proc.StandardInput.BaseStream.Write($b, 0, $b.Length)
        $proc.StandardInput.BaseStream.Flush()
    }
    # Empty phrase -> the program generates a five-word phrase and continues.
    Send-Line $p ''
    $sent = $false
    while ($null -ne ($line = $p.StandardOutput.ReadLine())) {
        if (-not $sent -and $line -match 'recovery code is:\s*([0-9A-Za-z -]+)') {
            $raw = ($Matches[1] -replace '[^0-9A-Za-z]', '')
            if ($raw.Length -ge 4) {
                Send-Line $p $raw.Substring($raw.Length - 4)
                $p.StandardInput.Close()
                $sent = $true
            }
        }
    }
    $p.WaitForExit()
    $err = $errTask.Result
    if ($p.ExitCode -ne 0) { throw "kit create failed (exit $($p.ExitCode)): $err" }
}
New-Kit
$storeGuess = @(Get-ChildItem -Path (Join-Path $HomeDir '.rosmarops') -Filter 'kit.*' -ErrorAction SilentlyContinue)
Check 'kit created' ($storeGuess.Count -ge 1) ("kit store under .rosmarops: " + ($storeGuess.Name -join ', '))

# --- baseline ----------------------------------------------------------------

$r = Invoke-Watched -Argv @('baseline')
if ($r.ExitCode -ne 0) { throw "rosmarops baseline failed (exit $($r.ExitCode)): $($r.Stderr)" }
Assert-NoNetwork $r 'baseline'

# --- run --lang es -----------------------------------------------------------

$runEs = Invoke-Watched -Argv @('run', '--lang', 'es')
Check 'run --lang es exit 0' ($runEs.ExitCode -eq 0) "exit $($runEs.ExitCode)"
Assert-NoNetwork $runEs 'run --lang es'
$summary.Add("TIME  run --lang es: $($runEs.Seconds)s")

# The console says the limit for 48 projects: «como mucho 12 minutos».
Check 'console states the 48-project limit' `
    ($runEs.Stdout -match "$expectedProjects proyectos, como mucho 12 minutos") `
    'console line "N proyectos, como mucho 12 minutos"'

# The Spanish report.
$esHtml = Join-Path $reports 'report.es.html'
Check 'report.es.html exists' (Test-Path $esHtml) $esHtml
$page = if (Test-Path $esHtml) { [System.IO.File]::ReadAllText($esHtml) } else { '' }

# A normal report, not the red "could not finish" page (BV-93 run_failed).
$runFailed = @('no pudo terminar', 'no se pudo terminar', 'could not finish')
$isRed = $false
foreach ($m in $runFailed) { if ($page.Contains($m)) { $isRed = $true } }
Check 'report is a normal report (not the red page)' (-not $isRed) 'the report shows the run-could-not-finish page'

# The report states the limit: «12 minutos (48 proyectos)».
Check 'report states the 48-project limit' `
    ($page -match "12 minutos \($expectedProjects proyectos\)") `
    'report line "12 minutos (48 proyectos)"'

# Coherence of the limit warning: the top warning «llegó a su límite de tiempo»
# must be present if and only if the run actually hit the limit. The ground
# truth is report.v2.json's coverage.scan_errors containing the global_timeout
# scope. Either outcome is valid; only the incoherence fails.
$warn = $page.Contains('llegó a su límite de tiempo')
$v2Path = Join-Path $reports 'report.v2.json'
$reached = $false
if (Test-Path $v2Path) {
    $v2 = Get-Content -Raw -Path $v2Path | ConvertFrom-Json
    $scanErrors = @()
    if ($v2.PSObject.Properties['coverage'] -and $v2.coverage.PSObject.Properties['scan_errors']) {
        $scanErrors = @($v2.coverage.scan_errors)
    }
    $reached = $scanErrors -contains 'global_timeout'
}
Check 'limit warning matches whether the limit was hit' ($warn -eq $reached) `
    "warning present=$warn, limit hit (v2 scan_errors)=$reached"
$summary.Add("INFO  limit reached this run: $reached")

# --- Expected findings (report.v2.json) --------------------------------------

$findings = @()
if (Test-Path $v2Path) { $findings = @((Get-Content -Raw -Path $v2Path | ConvertFrom-Json).findings) }
function Has-Rule([string]$rule) { return @($findings | Where-Object { $_.rule -eq $rule }).Count -gt 0 }

Check 'finding package_known_malicious present' (Has-Rule 'package_known_malicious')
# Both the Cursor MCP (@ifasn/md-doc-mcp) and the pypi package (claudeopuseni)
# must be named by a package_known_malicious finding's evidence in the HTML.
Check 'malicious @ifasn/md-doc-mcp shown' ($page -match 'md-doc-mcp') '@ifasn/md-doc-mcp not in report.es.html'
Check 'malicious claudeopuseni shown' ($page -match 'claudeopuseni') 'claudeopuseni not in report.es.html'
Check 'finding cred_in_git present' (Has-Rule 'cred_in_git')
# Each finding row names the file first: the «Encontrado en:» line.
Check 'report has the «Encontrado en:» line' ($page.Contains('Encontrado en:')) 'no "Encontrado en:" line in report.es.html'

# --- The sealed space package opens with the kit key ------------------------

$pkgs = @(Get-ChildItem -Path $reports -Filter 'rosmarops-results-*.locked.json' -ErrorAction SilentlyContinue)
Check 'space package sealed' ($pkgs.Count -ge 1) ("packages in reports: " + ($pkgs.Count))
if ($pkgs.Count -ge 1) {
    if ($OpenPkg) {
        $open = & $OpenPkg $pkgs[0].FullName 2>&1
    } else {
        $open = & go run ./tests/e2e/openpkg $pkgs[0].FullName 2>&1
    }
    Check 'space package opens with the kit key' ($LASTEXITCODE -eq 0) ("$open")
    $summary.Add("INFO  openpkg: $open")
}

# --- No full fake secret in any output or in the log ------------------------

$leakFiles = @()
if (Test-Path $reports) { $leakFiles += @(Get-ChildItem -Path $reports -Recurse -File) }
$leaked = [System.Collections.Generic.List[string]]::new()
foreach ($f in $leakFiles) {
    $text = [System.IO.File]::ReadAllText($f.FullName)
    foreach ($s in $manifestObj.secrets) {
        if ($s -and $text.Contains([string]$s)) { $leaked.Add("$($f.Name): $($s.Substring(0,[math]::Min(6,$s.Length)))…") }
    }
}
Check 'no full fake secret in any report/package' ($leaked.Count -eq 0) ($leaked -join '; ')

# --- run --unattended --------------------------------------------------------

$runUn = Invoke-Watched -Argv @('run', '--unattended')
Check 'run --unattended exit 0' ($runUn.ExitCode -eq 0) "exit $($runUn.ExitCode)"
Assert-NoNetwork $runUn 'run --unattended'
$summary.Add("TIME  run --unattended: $($runUn.Seconds)s")
# It wrote a report (either language page is enough).
$wrote = (Test-Path (Join-Path $reports 'report.es.html')) -or (Test-Path (Join-Path $reports 'report.en.html'))
Check 'run --unattended wrote a report' $wrote

# --- Summary -----------------------------------------------------------------

$summary.Add("TIME  total runs: es=$($runEs.Seconds)s, unattended=$($runUn.Seconds)s")
$summaryText = ($summary -join "`n")
[System.IO.File]::WriteAllText($log, $summaryText + "`n", ([System.Text.UTF8Encoding]::new($false)))
Write-Output ''
Write-Output '================ BV-157 e2e summary ================'
Write-Output $summaryText
Write-Output '===================================================='
Write-Output "log: $log"

if ($failures.Count -gt 0) {
    Write-Output ''
    $failures | ForEach-Object { Write-Output "FAILED: $_" }
    exit 1
}
Write-Output 'All BV-157 e2e checks passed.'
