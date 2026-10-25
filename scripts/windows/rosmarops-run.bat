@echo off
setlocal
cd /d "%~dp0"
if not exist "rosmarops.exe" (
  echo rosmarops.exe was not found next to this file.
  echo Extract the whole zip first ^(right-click, Extract All^), then run this file from the extracted folder.
  pause
  exit /b 1
)
set "RO_HOME=%USERPROFILE%\.rosmarops"
set "RO_REPORTS=%RO_HOME%\reports"
if exist "%RO_HOME%\state.json" goto run
set "RO_PROJECTS="
set /p "RO_PROJECTS=Where are your projects? Paste the folder that contains your repositories and press Enter (leave it empty to check only your agents): "
if not defined RO_PROJECTS goto agentsonly
set "RO_PROJECTS=%RO_PROJECTS:"=%"
if not defined RO_PROJECTS goto agentsonly
if exist "%RO_PROJECTS%\" goto withprojects
echo That folder does not exist. Checking only your agents.
goto agentsonly
:withprojects
rosmarops.exe init --repos-under "%RO_PROJECTS%"
goto run
:agentsonly
rosmarops.exe init
:run
if not exist "%RO_REPORTS%" mkdir "%RO_REPORTS%"
echo RosmarOps is checking this computer. It takes up to 5 minutes. Nothing leaves this computer.
rosmarops.exe run 2> "%RO_REPORTS%\last-run-log.txt"
if exist "%RO_REPORTS%\report.html" (
  echo Done. Opening the report.
  start "" "%RO_REPORTS%\report.html"
) else (
  echo Done. Opening the results folder.
  explorer "%RO_REPORTS%"
)
pause
