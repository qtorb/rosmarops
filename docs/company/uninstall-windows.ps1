# SIN PROBAR. No toca ~\.rosmarops de nadie: son los datos de cada persona.
Unregister-ScheduledTask -TaskName 'RosmarOps weekly review (company)' -Confirm:$false -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force (Join-Path $env:ProgramFiles 'RosmarOps'), (Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) 'RosmarOps')
