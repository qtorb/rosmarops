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

// sampleDismissCards are a few findings across the three groups, with the
// longest (Spanish) texts, to stress the window's layout.
func sampleDismissCards() []report.FindingCard {
	return []report.FindingCard{
		{ID: "f_1", Severity: "red", Title: "Una clave de Supabase que un agente puede leer y enviar fuera", WhatItMeans: "El agente puede leer esta clave y mandarla a un servicio de fuera.", WhenApplies: "Si esa clave es de prueba o no da acceso a nada real.", New: true, Agent: "Claude Code", Group: report.CardGroupNew},
		{ID: "f_2", Severity: "yellow", Title: "Otro agente puede escribir la configuración de este", WhatItMeans: "Otro agente puede cambiar cómo trabaja este.", WhenApplies: "Si usas a propósito este agente para configurar al otro.", New: true, Agent: "Cursor", Group: report.CardGroupNew},
		{ID: "f_3", Severity: "red", Title: "Una credencial dentro de git", WhatItMeans: "Hay una credencial guardada en el historial del repositorio.", WhenApplies: "Si es una clave de prueba, ya revocada.", Since: "25 oct", Agent: "Claude Code", Group: report.CardGroupRest},
		{ID: "cred_4", Severity: "yellow", Title: "Una clave que un agente puede leer", WhatItMeans: "El agente puede leer esta clave.", WhenApplies: "Si esa clave es de prueba.", Since: "1 nov", Agent: "Aider", Group: report.CardGroupDismissed},
	}
}

// TestDismissDriver proves the Go-side driver parses the window's answer:
// "save" then the ids, or "cancel". It uses pipes, so it needs no GUI and runs
// on the normal verify-windows job.
func TestDismissDriver(t *testing.T) {
	ids, saved := driveDismissWindow(strings.NewReader("save\nf_1 cred_4\n"))
	if !saved || strings.Join(ids, ",") != "f_1,cred_4" {
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
// checks its geometry and colours (BV-184 §4). Gated on
// ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN so the normal verify-windows job never
// launches a GUI; the diagnostic-windows.yml findings-window job sets it.
func TestDismissWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_FINDINGS_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}
	cat, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	cfg := dismissWindowConfigFrom(cat, sampleDismissCards())
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
	if kv["CLIENT"] == "" {
		t.Fatalf("no CLIENT emitted; collected: %v", kv)
	}
	client := nums("CLIENT")
	if client[0] != 640 || client[1] != 560 {
		t.Errorf("client size = %v, want 640x560", client)
	}
	if n := atoi(kv["ROWS"]); n != 4 {
		t.Errorf("rows = %d, want 4", n)
	}
	if !strings.Contains(kv["BORDER"], "FixedSingle") || kv["MAX"] != "False" || kv["MIN"] != "False" || kv["TOP"] != "True" {
		t.Errorf("chrome: BORDER=%s MAX=%s MIN=%s TOP=%s", kv["BORDER"], kv["MAX"], kv["MIN"], kv["TOP"])
	}
	argb := func(rgb uint32) int { return int(int32(0xFF000000 | rgb)) }
	if atoi(kv["FORMBACK"]) != argb(0xFFFFFF) {
		t.Errorf("form back = %s, want white", kv["FORMBACK"])
	}
	if atoi(kv["SAVEBACK"]) != argb(0x2346B8) {
		t.Errorf("save button back = %s, want cobalt", kv["SAVEBACK"])
	}
}

// TestDismissWindowScreenshots renders the window in EN and ES at 100 % and
// 150 %, closed and with an «i» open, for the advisor and the PR (BV-184 §4).
// Gated on ROSMAROPS_FINDINGS_WINDOW_SHOT_DIR.
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
		cfg := dismissWindowConfigFrom(cat, sampleDismissCards())
		cfg.Icon = ""
		cfg.IconFallback = ico
		for _, s := range []struct{ scale, pct string }{{"1", "100"}, {"1.5", "150"}} {
			for _, openI := range []string{"", "1"} {
				name := "findings-" + lang + "-" + s.pct
				if openI != "" {
					name += "-i"
				}
				out := filepath.Join(dir, name+".png")
				cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
				cmd.Env = append(os.Environ(),
					"ROSMAROPS_FINDINGS_WINDOW_SHOT="+out,
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_SCALE="+s.scale,
					"ROSMAROPS_FINDINGS_WINDOW_SHOT_OPEN_I="+openI,
				)
				cmd.Stderr = os.Stderr
				stdin, _ := cmd.StdinPipe()
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				if _, err := stdin.Write([]byte(dismissWindowInput(cfg) + "\n")); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("%s: window exited with error: %v", name, err)
					}
				case <-time.After(30 * time.Second):
					_ = cmd.Process.Kill()
					t.Fatalf("%s: screenshot timed out", name)
				}
				_ = stdin.Close()
				if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
					t.Fatalf("%s: no screenshot at %s (err=%v)", name, out, err)
				}
				t.Logf("wrote %s", out)
			}
		}
	}
}
