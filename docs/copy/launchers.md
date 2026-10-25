# Textos de los lanzadores

Firmados por Albert en BV-41 (2026-10-24); la pregunta de la primera ejecución, en BV-52. Son del lanzador, no del binario: van literales en cada `.bat`, no en el catálogo. `html.console.open` sigue reservada en el catálogo.

## `scripts/windows/rosmarops-run.bat` (inglés, CRLF)

| Momento | Texto |
| :- | :- |
| `rosmarops.exe` no encontrado | `rosmarops.exe was not found next to this file.` |
| | `Extract the whole zip first (right-click, Extract All), then run this file from the extracted folder.` |
| Primera ejecución (sin `state.json`) | `Where are your projects? Paste the folder that contains your repositories and press Enter (leave it empty to check only your agents): ` |
| La carpeta no existe | `That folder does not exist. Checking only your agents.` |
| Al empezar | `RosmarOps is checking this computer. It takes up to 5 minutes. Nothing leaves this computer.` |
| Al terminar, con `report.html` | `Done. Opening the report.` |
| Al terminar, sin `report.html` | `Done. Opening the results folder.` |

## `scripts/windows/rosmarops-ejecutar.bat` (castellano, `--lang es`, UTF-8 sin BOM, CRLF, `chcp 65001 >nul` tras `@echo off`)

| Momento | Texto |
| :- | :- |
| `rosmarops.exe` no encontrado | `No se ha encontrado rosmarops.exe junto a este archivo.` |
| | `Extrae primero todo el zip (botón derecho, Extraer todo) y ejecuta este archivo desde la carpeta extraída.` |
| Primera ejecución (sin `state.json`) | `¿Dónde están tus proyectos? Pega la carpeta que contiene tus repositorios y pulsa Enter (déjalo vacío para revisar solo tus agentes): ` |
| La carpeta no existe | `Esa carpeta no existe. Reviso solo tus agentes.` |
| Al empezar | `RosmarOps está revisando este ordenador. Tarda como mucho 5 minutos. Nada sale de este ordenador.` |
| Al terminar, con `report.html` | `Terminado. Abriendo el informe.` |
| Al terminar, sin `report.html` | `Terminado. Abriendo la carpeta de resultados.` |

Dentro de un bloque `if ( … )`, los paréntesis del texto se escriben `^(` y `^)`.

Desde BV-52 la salida estándar del binario va a la consola (progreso y pregunta de confirmación); solo el log (stderr) va a `%RO_REPORTS%\last-run-log.txt`. La carpeta vacía o inexistente inicia sin repositorios (`rosmarops.exe init`).
