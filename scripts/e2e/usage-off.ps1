# usage-off.ps1 (BV-271): whether a test computer has usage statistics on.
# Dot-sourced by run.ps1 and by tests/e2e/usage-off.tests.ps1. Written for
# Set-StrictMode -Version Latest: a state where the person never answered the
# usage question has no `usage` property at all, and reading a missing
# property under StrictMode throws, so the property is looked up, not read.

function Test-UsageOn([string]$StatePath) {
    if (-not (Test-Path $StatePath)) { return $false }
    $st = Get-Content -Raw $StatePath | ConvertFrom-Json
    $u = $st.PSObject.Properties['usage']
    if ($null -eq $u -or $null -eq $u.Value) { return $false }
    $enabled = $u.Value.PSObject.Properties['enabled']
    return ($null -ne $enabled) -and ($enabled.Value -eq $true)
}
