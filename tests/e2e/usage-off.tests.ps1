# usage-off.tests.ps1 (BV-271): Test-UsageOn under StrictMode, for every
# shape of state.json the e2e run can meet. CI runs it with PowerShell 7 and
# Windows PowerShell 5.1; it exits 1 on the first wrong answer.

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot '..\..\scripts\e2e\usage-off.ps1')

$dir = Join-Path ([System.IO.Path]::GetTempPath()) ("usage-off-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $dir | Out-Null
$cases = @(
    @{ name = 'no state.json'; json = $null; want = $false },
    @{ name = 'never answered (no usage)'; json = '{"schema_version":2,"engine_version":"0.17.0"}'; want = $false },
    @{ name = 'usage null'; json = '{"schema_version":2,"usage":null}'; want = $false },
    @{ name = 'enabled false'; json = '{"usage":{"enabled":false,"decided_at":"2026-11-20T00:00:00Z","text_version":1,"install_id":"x"}}'; want = $false },
    @{ name = 'usage without enabled'; json = '{"usage":{}}'; want = $false },
    @{ name = 'enabled true'; json = '{"usage":{"enabled":true,"decided_at":"2026-11-20T00:00:00Z","text_version":1,"install_id":"x"}}'; want = $true },
    @{ name = 'team mark only'; json = '{"usage_team":true}'; want = $false }
)
$failed = 0
foreach ($c in $cases) {
    $path = Join-Path $dir 'state.json'
    Remove-Item -Force -ErrorAction SilentlyContinue $path
    if ($null -ne $c.json) { [System.IO.File]::WriteAllText($path, $c.json) }
    try {
        $got = Test-UsageOn $path
    } catch {
        $got = "threw: $($_.Exception.Message)"
    }
    if ($got -is [bool] -and $got -eq $c.want) {
        Write-Output "PASS  $($c.name)"
    } else {
        Write-Output "FAIL  $($c.name): got $got, want $($c.want)"
        $failed++
    }
}
Remove-Item -Recurse -Force $dir
if ($failed -gt 0) { exit 1 }
Write-Output 'All usage-off checks passed.'
