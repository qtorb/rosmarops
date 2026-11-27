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

// sampleDismissCards are the shared test set (BV-184, BV-236 §5): eight
// findings — two new, three the rest (one a key saved in git), one accepted, one dismissed, one key changed — with reds and yellows, the longest
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
		{ID: "f_3", Severity: "red", Title: "Tu clave de Supabase: en git", WhatItMeans: "Al menos uno de estos repositorios tiene un remoto. RosmarOps no puede saber sin red si el commit con la clave se subió; si se subió, considera la clave comprometida y revócala en el proveedor.", WhenApplies: cat.Text("findings.when.cred_in_git", nil), Since: since(2026, time.October, 25), Group: report.CardGroupRest, CredID: "cred_3", WhenRotated: cat.Text("findings.when.rotated", nil)},
		{ID: "f_4", Severity: "yellow", Title: "Una clave que un agente puede leer", WhatItMeans: "El agente puede leer esta clave.", WhenApplies: "Si esa clave es de prueba o de un entorno local que no da acceso a nada real.", Since: since(2026, time.November, 1), Agent: "Aider", Group: report.CardGroupRest},
		{ID: "f_5", Severity: "red", Title: "Un hook del proyecto ejecuta un script del repositorio", WhatItMeans: "Un script del repositorio se ejecuta cuando salta un hook.", WhenApplies: "Si el proyecto es tuyo y conoces el script que ejecuta ese hook.", Since: since(2026, time.October, 30), Agent: "Codex", Group: report.CardGroupRest},
		{ID: "f_7", Severity: "yellow", Title: "Claude Code puede cambiar la configuración de otros agentes", WhatItMeans: "El agente puede editar los ajustes de otro agente.", WhenApplies: "Si usas a propósito este agente para configurar al otro y revisas cada cambio que hace.", Since: since(2026, time.October, 28), Agent: "Claude Code", Group: report.CardGroupAccepted},
		{ID: "cred_6", Severity: "yellow", Title: "Una clave de prueba que descartaste", WhatItMeans: "El agente puede leer esta clave.", WhenApplies: "Si esa clave es de prueba o de un entorno local que no da acceso a nada real.", Since: since(2026, time.October, 20), Agent: "Continue", Group: report.CardGroupDismissed},
		// BV-314: a key saved in git the person already recorded as changed.
		{ID: "f_8", Severity: "red", Title: "Tu clave de Stripe: en tu historial de git", WhatItMeans: "Ninguno de estos repositorios tiene un remoto declarado, así que probablemente la clave aún no ha salido de tu ordenador. Un push la enviaría con todo el historial.", WhenApplies: cat.Text("findings.when.cred_in_git", nil), Since: since(2026, time.October, 22), Group: report.CardGroupRotated, CredID: "cred_8", WhenRotated: cat.Text("findings.when.rotated", nil)},
	}
}

// TestDismissDriver proves the Go-side driver parses the window's answer:
// "save" then the ids, or "cancel". It uses pipes, so it needs no GUI and runs
// on the normal verify-windows job.
func TestDismissDriver(t *testing.T) {
	ch, saved := driveDismissWindow(strings.NewReader("save\nf_1 cred_6\nf_7\n"))
	if !saved || strings.Join(ch.Dismissed, ",") != "f_1,cred_6" || strings.Join(ch.Accepted, ",") != "f_7" {
		t.Errorf("save: %+v saved=%v", ch, saved)
	}
	// BV-314: a third line carries the ids marked «Ya la he cambiado».
	if ch, saved := driveDismissWindow(strings.NewReader("save\nf_1\n\nf_3\n")); !saved || strings.Join(ch.Rotated, ",") != "f_3" || strings.Join(ch.Dismissed, ",") != "f_1" {
		t.Errorf("save with a key changed: %+v saved=%v", ch, saved)
	}
	if ch, saved := driveDismissWindow(strings.NewReader("save\n\n\n")); !saved || len(ch.Dismissed) != 0 || len(ch.Accepted) != 0 {
		t.Errorf("save with nothing chosen: %+v saved=%v", ch, saved)
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
	// BV-236 §5: the chosen button is filled #1B1A18, a pending row's buttons
	// are white; each button is 96x28, 6 px apart, 16 px from the row's right
	// edge and centred on the row; the tally counts both lists.
	if kv["CHOSENINK"] != "True" {
		t.Errorf("CHOSENINK = %q, want True (no #1B1A18 fill on a chosen button)", kv["CHOSENINK"])
	}
	if kv["PENDINGWHITE"] != "True" {
		t.Errorf("PENDINGWHITE = %q, want True (a pending row's button is not white)", kv["PENDINGWHITE"])
	}
	if kv["BTN"] != "96,28" || kv["BTNGAP"] != "6" || kv["BTNRIGHT"] != "16" || kv["BTNVCENTER"] != "True" {
		t.Errorf("buttons: BTN=%s BTNGAP=%s BTNRIGHT=%s BTNVCENTER=%s, want 96,28 6 16 True", kv["BTN"], kv["BTNGAP"], kv["BTNRIGHT"], kv["BTNVCENTER"])
	}
	// BV-314: with a key saved in git among the rows, the keys changed follow.
	if want := strings.ReplaceAll(cfg.TallyAOne+" · "+cfg.TallyDOne+" · "+cfg.TallyROne, " ", "_"); kv["TALLY"] != want {
		t.Errorf("TALLY = %q, want %q", kv["TALLY"], want)
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
	// The subtitle's first ink column lines up with the title's, for a «new» row
	// and a dated row (BV-184 point 3 follow-up).
	if kv["ALIGNSUB"] != "True" {
		t.Errorf("ALIGNSUB = %q, want True (the small line does not start in the title's ink column; ANEW=%s AREST=%s)", kv["ALIGNSUB"], kv["ANEW"], kv["AREST"])
	}
	// Chrome.
	if !strings.Contains(kv["BORDER"], "FixedSingle") || kv["MAX"] != "False" || kv["MIN"] != "False" || kv["TOP"] != "True" {
		t.Errorf("chrome: BORDER=%s MAX=%s MIN=%s TOP=%s", kv["BORDER"], kv["MAX"], kv["MIN"], kv["TOP"])
	}
	client := nums("CLIENT")
	if client[0] != 760 || client[1] != 560 {
		t.Errorf("client size = %v, want 760x560", client)
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
				// BV-314: the key saved in git, pending and with «Ya la he
				// cambiado» chosen; its group «Claves que has cambiado» is
				// at the bottom (-rest).
				{"-key", map[string]string{"ROTATED": "show"}},
				{"-key-ticked", map[string]string{"ROTATED": "tick"}},
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
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_ROTATED="+v.env["ROTATED"],
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

// TestBV236WindowConfig: the window draws W1, W2, the two buttons, the four
// groups and the tally from the catalog, in EN and ES, and each card starts
// with the button of its group chosen (BV-236 §5).
func TestBV236WindowConfig(t *testing.T) {
	want := map[string][]string{
		"en": {"What do you want to do with these findings?", "I accept it", "Dismiss", "Accepted by you", "1 accepted", "{n} dismissed", "None changed"},
		"es": {"¿Qué haces con estos avisos?", "Lo asumo", "Descartar", "Asumidos por ti", "1 asumido", "{n} descartados", "Ninguno cambiado"},
	}
	for lang, w := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		cfg := dismissWindowConfigFrom(cat, sampleDismissCards(cat))
		got := []string{cfg.Q, cfg.Accept, cfg.Dismiss, cfg.GroupAccept, cfg.TallyAOne, cfg.TallyDOther, cfg.TallyNone}
		for i := range w {
			if got[i] != w[i] {
				t.Errorf("%s: %q, want %q", lang, got[i], w[i])
			}
		}
		if cfg.Text != cat.Text("findings.text.accept", nil) || cfg.Tally != "{accepted} · {dismissed}" {
			t.Errorf("%s: W2 %q tally %q", lang, cfg.Text, cfg.Tally)
		}
		states := map[string]string{}
		for _, c := range cfg.Cards {
			states[c.ID] = c.State
		}
		if states["f_7"] != "accepted" || states["cred_6"] != "dismissed" || states["f_1"] != "" || states["f_3"] != "" || states["f_8"] != "rotated" {
			t.Errorf("%s: initial states %v", lang, states)
		}
	}
}

// With the findings window's self-test hook set, the driver logs the window's
// SHOWN line with its name and treats it as not saved.
func TestDismissDriverSelfTestShown(t *testing.T) {
	log := captureLog(t)
	t.Setenv(findingsSelfTestEnv, "1")
	if _, saved := driveDismissWindow(strings.NewReader("SHOWN\r\nCARDS=7\r\n")); saved {
		t.Fatal("a self-test window is not a save")
	}
	if !strings.Contains(log.String(), "window self-test: shown") || !strings.Contains(log.String(), "window=findings") {
		t.Fatalf("log has no self-test line for the findings window:\n%s", log)
	}
}
