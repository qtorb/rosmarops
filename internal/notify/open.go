package notify

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// openTimeout bounds macOS's open (decision 2026-10-29, macOS vista previa,
// point 1).
const openTimeout = 30 * time.Second

// macOpen is macOS's open, by its absolute path. A variable only so that
// the integration tests can link a fake (-ldflags -X); nothing changes it
// at run time.
var macOpen = "/usr/bin/open"

// runner runs name with args and no shell, and waits for it; tests replace
// it.
type runner func(ctx context.Context, name string, args ...string) error

// openWith opens the report at path with macOS's open: one argument, the
// report's absolute path, with no shell; macOS picks the browser. open
// exits non-zero when it could not, so its exit code is the outcome.
func openWith(run runner, path string) error {
	if !strings.HasPrefix(path, "/") { // a macOS absolute path
		return fmt.Errorf("notify: open report: not an absolute path: %q", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if err := run(ctx, macOpen, path); err != nil {
		return fmt.Errorf("notify: open report: %w", err)
	}
	return nil
}
