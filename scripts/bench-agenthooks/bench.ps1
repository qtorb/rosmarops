# BV-226 §0: 20 runs of `rosmarops session-check` and 20 of `rosmarops
# statusline` on this machine, each a fresh process with the integrity check,
# and their median and p95 in milliseconds. It runs in a throwaway user folder
# (USERPROFILE points to a temp folder), so the person's files are never read.
param([Parameter(Mandatory = $true)][string]$Exe)
$ErrorActionPreference = 'Stop'
$exe = (Resolve-Path $Exe).Path
$h = Join-Path $env:RUNNER_TEMP ('bench-home-' + [guid]::NewGuid().ToString('N'))
$proj = Join-Path $h 'proj'
New-Item -ItemType Directory -Force -Path (Join-Path $h '.claude'), (Join-Path $proj '.claude\skills\s1'), (Join-Path $proj '.claude\agents\a1') | Out-Null
Set-Content -LiteralPath (Join-Path $h '.claude\settings.json') -Value '{"permissions":{"allow":["Bash(npm test)","Read"],"defaultMode":"acceptEdits"},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"x"}]}]}}' -Encoding utf8
Set-Content -LiteralPath (Join-Path $proj '.claude\settings.json') -Value '{"permissions":{"allow":["Bash(make)"]},"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/check.sh"}]}]}}' -Encoding utf8
Set-Content -LiteralPath (Join-Path $proj '.mcp.json') -Value '{"mcpServers":{"db":{"command":"psql"},"web":{"url":"https://example.invalid/mcp"}}}' -Encoding utf8
$env:USERPROFILE = $h; $env:HOME = $h
& $exe init | Out-Null
& $exe baseline | Out-Null
$data = Join-Path $h '.rosmarops\reports\data'
New-Item -ItemType Directory -Force -Path $data | Out-Null
[IO.File]::WriteAllText((Join-Path $data 'statusline.txt'), "en`tRosmarOps · 2 red · 72/100 · 13 NOV`nes`tRosmarOps · 2 rojos · 72/100 · 13 NOV`n")
$hookIn = '{"hook_event_name":"SessionStart","source":"startup","cwd":' + (ConvertTo-Json $proj) + '}'
function Measure-Runs($arg, $stdin) {
  $ms = @()
  for ($i = 0; $i -lt 20; $i++) {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $exe; $psi.Arguments = $arg
    $psi.UseShellExecute = $false; $psi.RedirectStandardInput = $true; $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $p = [System.Diagnostics.Process]::Start($psi)
    $p.StandardInput.Write($stdin); $p.StandardInput.Close()
    $null = $p.StandardOutput.ReadToEnd(); $null = $p.StandardError.ReadToEnd()
    $p.WaitForExit()
    $sw.Stop()
    $ms += $sw.Elapsed.TotalMilliseconds
  }
  $s = $ms | Sort-Object
  $median = ($s[9] + $s[10]) / 2
  $p95 = $s[18]  # the 19th of 20 sorted runs
  Write-Output ("{0}: median {1:N1} ms, p95 {2:N1} ms, all [{3}]" -f $arg, $median, $p95, (($s | ForEach-Object { '{0:N0}' -f $_ }) -join ' '))
}
Measure-Runs 'session-check' $hookIn
Measure-Runs 'statusline' ''
