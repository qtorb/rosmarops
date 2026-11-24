//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// TestUsageWindowScreenshots (BV-286) renders the usage statistics window in
// ES and EN, the question and the thank-you, at 100 % and 150 %, to PNG files
// for the PR. It is gated on ROSMAROPS_USAGE_WINDOW_SHOT_DIR so the normal test
// run never opens a GUI; the diagnostic-windows.yml usage-window job sets it
// and uploads the images. Each image is the real window captured off the
// desktop, as the connect window's.
func TestUsageWindowScreenshots(t *testing.T) {
	dir := os.Getenv("ROSMAROPS_USAGE_WINDOW_SHOT_DIR")
	if dir == "" {
		t.Skip("set ROSMAROPS_USAGE_WINDOW_SHOT_DIR to capture the usage-window screenshots on a Windows runner")
	}
	ps1 := filepath.Join(t.TempDir(), "usagewindow.ps1")
	if err := os.WriteFile(ps1, []byte(usageWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"es", "en"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		cfg := usageWindowConfigFrom(cat)
		cfg.Icon = ""
		if ico, err := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico")); err == nil {
			cfg.IconFallback = ico
		}
		for _, state := range []string{"question", "thanks"} {
			for _, s := range []struct{ scale, pct string }{{"1", "100"}, {"1.5", "150"}} {
				out := filepath.Join(dir, "usage-"+lang+"-"+state+"-"+s.pct+".png")
				cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
				cmd.Env = append(os.Environ(),
					"ROSMAROPS_USAGE_WINDOW_SHOT="+out,
					"ROSMAROPS_USAGE_WINDOW_SHOT_SCALE="+s.scale,
					"ROSMAROPS_USAGE_WINDOW_SHOT_STATE="+state,
				)
				cmd.Stderr = os.Stderr
				stdin, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				if _, err := stdin.Write([]byte(usageWindowInput(cfg) + "\n")); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("%s %s %s%%: window exited with error: %v", lang, state, s.pct, err)
					}
				case <-time.After(30 * time.Second):
					_ = cmd.Process.Kill()
					t.Fatalf("%s %s %s%%: screenshot timed out", lang, state, s.pct)
				}
				_ = stdin.Close()
				fi, err := os.Stat(out)
				if err != nil || fi.Size() == 0 {
					t.Fatalf("%s %s %s%%: no screenshot at %s (err=%v)", lang, state, s.pct, out, err)
				}
				t.Logf("wrote %s (%d bytes)", out, fi.Size())
			}
		}
	}
}
