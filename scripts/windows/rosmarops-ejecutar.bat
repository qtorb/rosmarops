@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"
if not exist "rosmarops.exe" (
  echo No se ha encontrado rosmarops.exe junto a este archivo.
  echo Extrae primero todo el zip ^(botón derecho, Extraer todo^) y ejecuta este archivo desde la carpeta extraída.
  pause
  exit /b 1
)
set "RO_HOME=%USERPROFILE%\.rosmarops"
set "RO_REPORTS=%RO_HOME%\reports"
if exist "%RO_HOME%\state.json" goto run
set "RO_PROJECTS="
set /p "RO_PROJECTS=¿Dónde están tus proyectos? Pega la carpeta que contiene tus repositorios y pulsa Enter (déjalo vacío para revisar solo tus agentes): "
if not defined RO_PROJECTS goto agentsonly
set "RO_PROJECTS=%RO_PROJECTS:"=%"
if not defined RO_PROJECTS goto agentsonly
if exist "%RO_PROJECTS%\" goto withprojects
echo Esa carpeta no existe. Reviso solo tus agentes.
goto agentsonly
:withprojects
rosmarops.exe init --repos-under "%RO_PROJECTS%"
goto run
:agentsonly
rosmarops.exe init
:run
if not exist "%RO_REPORTS%" mkdir "%RO_REPORTS%"
echo RosmarOps está revisando este ordenador. Tarda como mucho 5 minutos. Nada sale de este ordenador.
rosmarops.exe run --lang es 2> "%RO_REPORTS%\last-run-log.txt"
if exist "%RO_REPORTS%\report.html" (
  echo Terminado. Abriendo el informe.
  start "" "%RO_REPORTS%\report.html"
) else (
  echo Terminado. Abriendo la carpeta de resultados.
  explorer "%RO_REPORTS%"
)
pause
