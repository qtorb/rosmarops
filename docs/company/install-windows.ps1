# SIN PROBAR. Fleet lo ejecuta tras descargar el .exe; $env:INSTALLER_PATH apunta a él.
$ErrorActionPreference = 'Stop'
$version = '0.11.2'
$bin = Join-Path $env:ProgramFiles 'RosmarOps'
$cfg = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) 'RosmarOps'
New-Item -ItemType Directory -Force -Path $bin, $cfg | Out-Null
# Solo Administradores y SYSTEM escriben; Users lee. SIDs, para no depender del idioma.
foreach ($d in @($bin, $cfg)) {
  icacls $d /inheritance:r /grant:r '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
  icacls $d /setowner '*S-1-5-32-544' /T | Out-Null
}
$exe = Join-Path $bin 'rosmarops.exe'
Copy-Item -Force $env:INSTALLER_PATH $exe
$hash = (Get-FileHash -Algorithm SHA256 $exe).Hash.ToLower()
# UTF-8 sin BOM: Set-Content -Encoding utf8 de PowerShell 5 añade BOM.
[IO.File]::WriteAllText((Join-Path $cfg 'binaries.sha256'), "$hash  $version`n")
[IO.File]::WriteAllText((Join-Path $cfg 'company.json'),
  '{"schema_version":1,"summary":{"enabled":true},"first_run":{"repo_roots":["~/source","~/projects"]}}')
# Una tarea para todos los usuarios: semanal y al iniciar sesión, sin elevar.
$a = New-ScheduledTaskAction -Execute $exe -Argument 'run --unattended' -WorkingDirectory $bin
$t = @((New-ScheduledTaskTrigger -Weekly -DaysOfWeek Friday -At 09:00), (New-ScheduledTaskTrigger -AtLogOn))
$p = New-ScheduledTaskPrincipal -GroupId 'S-1-5-32-545' -RunLevel Limited
$s = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
       -ExecutionTimeLimit (New-TimeSpan -Minutes 25) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'RosmarOps weekly review (company)' -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null
