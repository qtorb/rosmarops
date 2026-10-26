package windows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
)

// extensionRecord mirrors a single entry in
// %USERPROFILE%\.vscode\extensions\extensions.json.
// Only the fields RosmarOps needs are decoded; unknown fields are ignored.
type extensionRecord struct {
	Identifier struct {
		ID string `json:"id"`
	} `json:"identifier"`
	Version  string `json:"version"`
	Metadata struct {
		// InstalledTimestamp is the install date in milliseconds since
		// the epoch; VS Code writes it in the metadata of each entry, which
		// is optional (A5).
		InstalledTimestamp int64 `json:"installedTimestamp"`
	} `json:"metadata"`
}

// ListVSCodeExtensions implements platform.Platform.
//
// Strategy:
//  1. Read %USERPROFILE%\.vscode\extensions\extensions.json — the authoritative
//     registry of all installed extensions (enabled and disabled).
//  2. Run `code --list-extensions` with a 10-second timeout to identify which
//     extensions are currently enabled. If the CLI is unavailable or exits
//     non-zero, all extensions are treated as Enabled=true (conservative —
//     safer to over-report than to miss an entry).
//  3. Return a VSCodeExtension for each record with Enabled set accordingly.
func (w *WindowsPlatform) ListVSCodeExtensions(ctx context.Context) ([]platform.VSCodeExtension, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	jsonPath := filepath.Join(home, ".vscode", "extensions", "extensions.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			// VS Code not installed or no extensions — not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("read extensions.json %s: %w", jsonPath, err)
	}

	var records []extensionRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("parse extensions.json: %w", err)
	}

	// nil = CLI unavailable → treat all as enabled.
	enabledSet := tryListEnabledIDs(ctx)

	exts := make([]platform.VSCodeExtension, 0, len(records))
	for _, r := range records {
		id := strings.ToLower(r.Identifier.ID)
		if id == "" {
			continue
		}
		enabled := true
		if enabledSet != nil {
			_, enabled = enabledSet[id]
		}
		ext := platform.VSCodeExtension{
			ID:      id,
			Version: r.Version,
			Enabled: enabled,
		}
		if ms := r.Metadata.InstalledTimestamp; ms > 0 {
			ext.InstalledAt = time.UnixMilli(ms).UTC()
		}
		exts = append(exts, ext)
	}

	return exts, nil
}

// tryListEnabledIDs runs `code --list-extensions` with a 10-second timeout
// and returns a set of lowercase enabled extension IDs.
// Returns nil if the code CLI is not on PATH or exits non-zero.
func tryListEnabledIDs(ctx context.Context) map[string]struct{} {
	ctx10, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx10, "code", "--list-extensions")
	cmd.WaitDelay = subprocessWaitDelay // code.cmd starts a child that holds stdout
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		// CLI unavailable — caller uses all-enabled fallback.
		return nil
	}

	set := make(map[string]struct{})
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip version suffix in case --show-versions was somehow implied.
		if idx := strings.Index(line, "@"); idx > 0 {
			line = line[:idx]
		}
		set[strings.ToLower(line)] = struct{}{}
	}

	return set
}
