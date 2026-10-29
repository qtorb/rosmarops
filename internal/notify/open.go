package notify

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// openTimeout bounds macOS's open (decision 2026-10-29, macOS vista previa,
// point 1).
const openTimeout = 30 * time.Second

// MacOpen is macOS's open, by its absolute path.
const MacOpen = "/usr/bin/open"

// runner runs name with args and no shell, and waits for it; tests replace
// it.
type runner func(ctx context.Context, name string, args ...string) error

// openWith opens the report at path with macOS's open: one argument, the
// report's absolute path, with no shell; macOS picks the browser. open
// exits non-zero when it could not, so its exit code is the outcome.
func openWith(run runner, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("notify: open report: not an absolute path: %q", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if err := run(ctx, MacOpen, path); err != nil {
		return fmt.Errorf("notify: open report: %w", err)
	}
	return nil
}
