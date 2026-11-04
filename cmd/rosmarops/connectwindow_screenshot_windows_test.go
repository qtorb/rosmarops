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

// TestConnectWindowScreenshots (BV-178) renders the connect window in EN and ES
// at 100 % and 150 % to PNG files, for the advisor and the PR. It is gated on
// ROSMAROPS_CONNECT_WINDOW_SHOT_DIR (the output directory) so the normal test
// run never opens a GUI; the diagnostic-windows.yml connect-window job sets it
// and uploads the images. Each image is the real window captured off the
// desktop; 150 % is the window scaled by 1.5 (a 96-DPI runner cannot change its
// monitor DPI), the same factor AutoScaleMode Dpi applies on a 150 % monitor.
func TestConnectWindowScreenshots(t *testing.T) {
	dir := os.Getenv("ROSMAROPS_CONNECT_WINDOW_SHOT_DIR")
	if dir == "" {
		t.Skip("set ROSMAROPS_CONNECT_WINDOW_SHOT_DIR to capture the connect-window screenshots on a Windows runner")
	}
	ps1 := filepath.Join(t.TempDir(), "connectwindow.ps1")
	if err := os.WriteFile(ps1, []byte(connectWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		cfg := connectWindowConfigFrom(cat)
		cfg.Icon = "" // no exe icon in the test binary; use the rosmarops.ico fallback
		if ico, err := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico")); err == nil {
			cfg.IconFallback = ico
		}
		for _, s := range []struct{ scale, pct string }{{"1", "100"}, {"1.5", "150"}} {
			out := filepath.Join(dir, "connect-"+lang+"-"+s.pct+".png")
			cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
			cmd.Env = append(os.Environ(),
				"ROSMAROPS_CONNECT_WINDOW_SHOT="+out,
				"ROSMAROPS_CONNECT_WINDOW_SHOT_SCALE="+s.scale,
			)
			cmd.Stderr = os.Stderr
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if _, err := stdin.Write([]byte(connectWindowInput(cfg) + "\n")); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s %s%%: window exited with error: %v", lang, s.pct, err)
				}
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("%s %s%%: screenshot timed out", lang, s.pct)
			}
			_ = stdin.Close()
			fi, err := os.Stat(out)
			if err != nil || fi.Size() == 0 {
				t.Fatalf("%s %s%%: no screenshot at %s (err=%v)", lang, s.pct, out, err)
			}
			t.Logf("wrote %s (%d bytes)", out, fi.Size())
		}
	}
}
