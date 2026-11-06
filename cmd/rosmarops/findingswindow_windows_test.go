//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// sampleDismissCards are the shared test set (BV-184): at least six findings —
// two new, three the rest, one dismissed — with reds and yellows, the longest
// (Spanish) texts, to stress the window's layout and scrolling. The «open
// since» dates come from cat.LongDate, so they render in the catalog's long
// form («25 de octubre de 2026» in Spanish) exactly as the real window does.
func sampleDismissCards(cat catalog.Catalog) []report.FindingCard {
	since := func(y int, m time.Month, d int) string {
		return cat.LongDate(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
	}
	return []report.FindingCard{
		{ID: "f_1", Severity: "red", Title: "Una clave de Supabase que un agente puede leer y enviar fuera", WhatItMeans: "El agente puede leer esta clave y mandarla a un servicio de fuera.", WhenApplies: "Si esa clave es de prueba o no da acceso a nada real, aunque el agente pueda enviarla fuera.", New: true, Agent: "Claude Code", Group: report.CardGroupNew},
		{ID: "f_2", Severity: "yellow", Title: "Otro agente puede escribir la configuración de este agente", WhatItMeans: "Otro agente puede cambiar cómo trabaja este.", WhenApplies: "Si usas a propósito este agente para configurar al otro y revisas cada cambio que hace.", New: true, Agent: "Cursor", Group: report.CardGroupNew},
		{ID: "f_3", Severity: "red", Title: "Una credencial dentro de git", WhatItMeans: "Hay una credencial guardada en el historial del repositorio.", WhenApplies: "Si es una clave de prueba, ya revocada o de un proyecto que nunca se publicó, y no abre nada real.", Since: since(2026, time.October, 25), Agent: "Claude Code", Group: report.CardGroupRest},
		{ID: "f_4", Severity: "yellow", Title: "Una clave que un agente puede leer", WhatItMeans: "El agente puede leer esta clave.", WhenApplies: "Si esa clave es de prueba o de un entorno local que no da acceso a nada real.", Since: since(2026, time.November, 1), Agent: "Aider", Group: report.CardGroupRest},
		{ID: "f_5", Severity: "red", Title: "Un hook del proyecto ejecuta un script del repositorio", WhatItMeans: "Un script del repositorio se ejecuta cuando salta un hook.", WhenApplies: "Si el proyecto es tuyo y conoces el script que ejecuta ese hook.", Since: since(2026, time.October, 30), Agent: "Codex", Group: report.CardGroupRest},
		{ID: "cred_6", Severity: "yellow", Title: "Una clave de prueba que descartaste", WhatItMeans: "El agente puede leer esta clave.", WhenApplies: "Si esa clave es de prueba o de un entorno local que no da acceso a nada real.", Since: since(2026, time.October, 20), Agent: "Continue", Group: report.CardGroupDismissed},
	}
}

// TestDismissDriver proves the Go-side driver parses the window's answer:
// "save" then the ids, or "cancel". It uses pipes, so it needs no GUI and runs
// on the normal verify-windows job.
func TestDismissDriver(t *testing.T) {
	ids, saved := driveDismissWindow(strings.NewReader("save\nf_1 cred_6\n"))
	if !saved || strings.Join(ids, ",") != "f_1,cred_6" {
		t.Errorf("save: ids=%v saved=%v", ids, saved)
	}
	if ids, saved := driveDismissWindow(strings.NewReader("save\n\n")); !saved || len(ids) != 0 {
		t.Errorf("save with nothing ticked: ids=%v saved=%v", ids, saved)
	}
	if _, saved := driveDismissWindow(strings.NewReader("cancel\n")); saved {
		t.Error("cancel counted as saved")
	}
	if _, saved := driveDismissWindow(strings.NewReader("")); saved {
		t.Error("a closed stream counted as saved")
	}
}

// TestDismissWindowSelfTest runs the real WinForms window on windows-latest and
// checks that it actually renders the rows (BV-184 §4): one visible row per
// finding, each inside the panel's visible width, no horizontal scrollbar, the
// counter matching the ticked boxes, and a row that grows when its «i» opens.
// Gated on ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN so the normal verify-windows
// job never launches a GUI; the diagnostic-windows.yml findings-window job sets
// it.
func TestDismissWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}
	cat, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	cards := sampleDismissCards(cat)
	cfg := dismissWindowConfigFrom(cat, cards)
	ico, _ := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico"))
	cfg.Icon = ""
	cfg.IconFallback = ico
	ps1 := filepath.Join(t.TempDir(), "findingswindow.ps1")
	if err := os.WriteFile(ps1, []byte(dismissWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
	cmd.Env = append(os.Environ(), "ROSMAROPS_FINDINGS_WINDOW_SELFTEST=1")
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte(dismissWindowInput(cfg) + "\n")); err != nil {
		t.Fatal(err)
	}
	kv := map[string]string{}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			t.Logf("window: %s", l)
			for _, tok := range strings.Fields(l) {
				if i := strings.IndexByte(tok, '='); i > 0 {
					kv[tok[:i]] = tok[i+1:]
				}
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("window self-test timed out")
	}
	_ = stdin.Close()
	_ = cmd.Wait()

	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	nums := func(key string) []int {
		var out []int
		for _, p := range strings.Split(kv[key], ",") {
			out = append(out, atoi(p))
		}
		return out
	}
	if e, ok := kv["SELFTESTERR"]; ok {
		t.Fatalf("window self-test raised an error: %s", e)
	}
	if kv["CLIENT"] == "" {
		t.Fatalf("no CLIENT emitted; collected: %v", kv)
	}
	// (0) The config carried every card, and each became a row.
	if n := atoi(kv["CARDS"]); n != len(cards) {
		t.Errorf("CARDS = %d, want %d (cards lost in the pipeline)", n, len(cards))
	}
	// (1) One visible row per finding.
	if n := atoi(kv["ROWS"]); n != len(cards) {
		t.Errorf("ROWS = %d, want %d (one row per finding)", n, len(cards))
	}
	// Non-ASCII text (·, á, ñ) survives the base64 UTF-8 pipeline to the window.
	if kv["ENCOK"] != "True" {
		t.Errorf("ENCOK = %q, want True (UTF-8 text is garbled)", kv["ENCOK"])
	}
	// The «open since» date is in the catalog's long form: in Spanish it reads
	// «… de … de …» (cat.LongDate), not an abbreviated «25 oct 2026».
	if kv["DATEDE"] != "True" {
		t.Errorf("DATEDE = %q, want True (the ES date is not in long form «… de …»)", kv["DATEDE"])
	}
	// (2) Every row inside the panel's visible width; no horizontal scrollbar.
	listClient := nums("LISTCLIENT")
	if right := atoi(kv["ROWMAXRIGHT"]); right > listClient[0] {
		t.Errorf("a row's right edge %d exceeds the list client width %d (horizontal overflow)", right, listClient[0])
	}
	if kv["HSCROLL"] != "False" {
		t.Errorf("HorizontalScroll.Visible = %q, want False", kv["HSCROLL"])
	}
	// (3) The rows actually render (pixel checks, not just control counts): a red
	// pill background, a yellow pill background, dark title text in the first
	// row, and a cobalt checkbox on the ticked (dismissed) finding. A blank list
	// fails here.
	if kv["PILLRED"] != "True" {
		t.Errorf("no red pill background (#FBE9E5) rendered in any row — the list did not paint")
	}
	if kv["PILLYEL"] != "True" {
		t.Errorf("no yellow pill background (#FDF5DF) rendered in any row")
	}
	if kv["TITLEDARK"] != "True" {
		t.Errorf("no dark title text (#1B1A18) in the first row's text column")
	}
	if kv["CHECKCOBALT"] != "True" {
		t.Errorf("no cobalt (#2346B8) in the ticked finding's checkbox")
	}
	// The pill is rounded: its corner (1 px inside the rectangle) still shows the
	// row background, not the pill fill — i.e. the pill is not a plain rectangle.
	if kv["PILLCORNER"] != "True" {
		t.Errorf("PILLCORNER = %q, want True (the pill corner is filled — the pill is rectangular, not rounded)", kv["PILLCORNER"])
	}
	// (4) Opening an «i» grows the row, draws the detail text, and the icon itself
	// becomes a filled dark disc (#1B1A18).
	io := nums("IOPEN")
	if len(io) != 2 || io[1] <= io[0] {
		t.Errorf("opening an «i» did not grow the row: %v", kv["IOPEN"])
	}
	if kv["DETAILTEXT"] != "True" {
		t.Errorf("opening an «i» drew no detail text (owner-draw did not render)")
	}
	if kv["ICONFILL"] != "True" {
		t.Errorf("ICONFILL = %q, want True (the open «i» is not a filled #1B1A18 disc)", kv["ICONFILL"])
	}
	// Polish (BV-184): the detail box background tracks its row at rest and under
	// the mouse (point 1); the pill corner shows the hover colour, not white, when
	// the row is hovered (point 2); and the agent label sits flush at the end of
	// «New», so the gaps around the «·» are equal (point 3).
	if kv["DETAILBGREST"] != "True" {
		t.Errorf("DETAILBGREST = %q, want True (the detail box background is not the row's at rest)", kv["DETAILBGREST"])
	}
	if kv["DETAILBGHOVER"] != "True" {
		t.Errorf("DETAILBGHOVER = %q, want True (the detail box background does not follow the row on hover)", kv["DETAILBGHOVER"])
	}
	if kv["PILLCORNERHOVER"] != "True" {
		t.Errorf("PILLCORNERHOVER = %q, want True (the pill corner is white on a hovered row, not the row colour)", kv["PILLCORNERHOVER"])
	}
	if kv["GAPFLUSH"] != "True" {
		t.Errorf("GAPFLUSH = %q, want True (the agent label is not flush with «New»; the gaps around «·» differ)", kv["GAPFLUSH"])
	}
	// Chrome.
	if !strings.Contains(kv["BORDER"], "FixedSingle") || kv["MAX"] != "False" || kv["MIN"] != "False" || kv["TOP"] != "True" {
		t.Errorf("chrome: BORDER=%s MAX=%s MIN=%s TOP=%s", kv["BORDER"], kv["MAX"], kv["MIN"], kv["TOP"])
	}
	client := nums("CLIENT")
	if client[0] != 640 || client[1] != 560 {
		t.Errorf("client size = %v, want 640x560", client)
	}
}

// TestDismissWindowScreenshots renders the window in EN and ES at 100 % and
// 150 %, closed and with an «i» open — eight images (BV-184 §4) — using the
// same sample as the self-test. Every capture must land; a missing one fails
// the test. Gated on ROSMAROPS_FINDINGS_WINDOW_SHOT_DIR.
func TestDismissWindowScreenshots(t *testing.T) {
	dir := os.Getenv("ROSMAROPS_FINDINGS_WINDOW_SHOT_DIR")
	if dir == "" {
		t.Skip("set ROSMAROPS_FINDINGS_WINDOW_SHOT_DIR to capture the findings-window screenshots on a Windows runner")
	}
	ps1 := filepath.Join(t.TempDir(), "findingswindow.ps1")
	if err := os.WriteFile(ps1, []byte(dismissWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	ico, _ := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico"))
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		cfg := dismissWindowConfigFrom(cat, sampleDismissCards(cat))
		cfg.Icon = ""
		cfg.IconFallback = ico
		for _, s := range []struct{ scale, pct string }{{"1", "100"}, {"1.5", "150"}} {
			// Four variants per language and scale (BV-184 §4): the list as it
			// opens and with the first «i» open — in both, the first «new» row is
			// ticked so a ticked box and «1 ticked» show on screen (4b); the list
			// scrolled to the bottom, where «the rest» (long date) and «dismissed»
			// are (4a); and a hover view with the «i» open, to check the detail and
			// pill backgrounds (4c).
			for _, v := range []struct {
				suffix string
				env    map[string]string
			}{
				{"", map[string]string{"TICK_NEW": "1"}},
				{"-i", map[string]string{"TICK_NEW": "1", "OPEN_I": "1"}},
				{"-rest", map[string]string{"SCROLL": "bottom"}},
				{"-hover", map[string]string{"TICK_NEW": "1", "OPEN_I": "1", "HOVER": "1"}},
			} {
				name := "findings-" + lang + "-" + s.pct + v.suffix
				out := filepath.Join(dir, name+".png")
				cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
				cmd.Env = append(os.Environ(),
					"ROSMAROPS_FINDINGS_WINDOW_SHOT="+out,
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_SCALE="+s.scale,
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_OPEN_I="+v.env["OPEN_I"],
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_SCROLL="+v.env["SCROLL"],
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_TICK_NEW="+v.env["TICK_NEW"],
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_HOVER="+v.env["HOVER"],
				)
				cmd.Stderr = os.Stderr
				stdin, _ := cmd.StdinPipe()
				if err := cmd.Start(); err != nil {
					t.Errorf("%s: start: %v", name, err)
					continue
				}
				if _, err := stdin.Write([]byte(dismissWindowInput(cfg) + "\n")); err != nil {
					t.Errorf("%s: write: %v", name, err)
					_ = cmd.Process.Kill()
					continue
				}
				doneCh := make(chan error, 1)
				go func() { doneCh <- cmd.Wait() }()
				select {
				case err := <-doneCh:
					if err != nil {
						t.Errorf("%s: window exited with error: %v", name, err)
					}
				case <-time.After(30 * time.Second):
					_ = cmd.Process.Kill()
					t.Errorf("%s: screenshot timed out", name)
				}
				_ = stdin.Close()
				if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
					t.Errorf("%s: no screenshot at %s (err=%v)", name, out, err)
				} else {
					t.Logf("wrote %s", out)
				}
			}
		}
	}
}
