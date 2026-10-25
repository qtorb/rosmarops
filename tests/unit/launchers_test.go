package unit_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// BV-41: the launchers carry the signed texts of docs/copy/launchers.md,
// with CRLF line ends; the Spanish one is UTF-8 without BOM, switches the
// console to UTF-8 and runs with --lang es; both open report.html when it
// exists, the folder otherwise. BV-52: the console shows the run (only the
// log, stderr, goes to a file), and the first run asks for the projects
// folder: empty or missing, init without repositories.
func TestLaunchers(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "scripts", "windows")
	for name, want := range map[string][]string{
		"rosmarops-run.bat": {
			"echo RosmarOps is checking this computer. It takes up to 5 minutes. Nothing leaves this computer.",
			"  echo Done. Opening the report.", "  echo Done. Opening the results folder.",
			`  start "" "%RO_REPORTS%\report.html"`, `rosmarops.exe run 2> "%RO_REPORTS%\last-run-log.txt"`,
			`set /p "RO_PROJECTS=Where are your projects? Paste the folder that contains your repositories and press Enter (leave it empty to check only your agents): "`,
			"echo That folder does not exist. Checking only your agents.",
			`rosmarops.exe init --repos-under "%RO_PROJECTS%"`, "rosmarops.exe init",
		},
		"rosmarops-ejecutar.bat": {
			"chcp 65001 >nul",
			"  echo No se ha encontrado rosmarops.exe junto a este archivo.",
			"  echo Extrae primero todo el zip ^(botón derecho, Extraer todo^) y ejecuta este archivo desde la carpeta extraída.",
			"echo RosmarOps está revisando este ordenador. Tarda como mucho 5 minutos. Nada sale de este ordenador.",
			"  echo Terminado. Abriendo el informe.", "  echo Terminado. Abriendo la carpeta de resultados.",
			`  start "" "%RO_REPORTS%\report.html"`, `rosmarops.exe run --lang es 2> "%RO_REPORTS%\last-run-log.txt"`,
			`set /p "RO_PROJECTS=¿Dónde están tus proyectos? Pega la carpeta que contiene tus repositorios y pulsa Enter (déjalo vacío para revisar solo tus agentes): "`,
			"echo Esa carpeta no existe. Reviso solo tus agentes.",
			`rosmarops.exe init --repos-under "%RO_PROJECTS%"`, "rosmarops.exe init",
		},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		if strings.HasPrefix(s, "\ufeff") {
			t.Errorf("%s: BOM", name)
		}
		if strings.Count(s, "\n") != strings.Count(s, "\r\n") {
			t.Errorf("%s: not CRLF", name)
		}
		lines := strings.Split(strings.TrimRight(s, "\r\n"), "\r\n")
		if name == "rosmarops-ejecutar.bat" && (len(lines) < 2 || lines[0] != "@echo off" || lines[1] != "chcp 65001 >nul") {
			t.Errorf("%s: chcp is not the first line after @echo off", name)
		}
		for _, w := range want {
			found := false
			for _, l := range lines {
				found = found || l == w
			}
			if !found {
				t.Errorf("%s: no line %q", name, w)
			}
		}
		// Standard output is never redirected: the progress and the
		// configuration question reach the console.
		for _, l := range lines {
			if strings.Contains(l, "rosmarops.exe run") && (strings.Contains(l, " 1>") || strings.Contains(l, " > ") || strings.Contains(l, ">\"")) {
				t.Errorf("%s: standard output redirected: %q", name, l)
			}
		}
	}
}
