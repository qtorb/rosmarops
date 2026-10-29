package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildCmd is the go build of the binary to bin, with ldflags (may be
// empty) and the extra build flags. On macOS it links fakes in place of
// /usr/bin/open and /usr/bin/osascript (BV-102): a black-box run must not
// open a browser or wait on a folder dialog in CI. The fake open succeeds;
// the fake osascript is a cancelled dialog (-128). Release builds never
// set these variables.
func buildCmd(t *testing.T, bin, ldflags string, extra ...string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "darwin" {
		dir := t.TempDir()
		open, osascript := filepath.Join(dir, "open"), filepath.Join(dir, "osascript")
		for p, body := range map[string]string{
			open:      "#!/bin/sh\nexit 0\n",
			osascript: "#!/bin/sh\necho 'execution error: User canceled. (-128)' >&2\nexit 1\n",
		} {
			if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		ldflags = strings.TrimSpace(ldflags + " -X github.com/qtorb/rosmarops/internal/notify.macOpen=" + open +
			" -X github.com/qtorb/rosmarops/internal/firstrun.macOsascript=" + osascript)
	}
	args := append([]string{"build"}, extra...)
	if ldflags != "" {
		args = append(args, "-ldflags="+ldflags)
	}
	build := exec.Command("go", append(args, "-o", bin, "./cmd/rosmarops")...)
	build.Dir = filepath.Join("..", "..")
	return build
}
