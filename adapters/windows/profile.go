package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// profilePathsResponse mirrors the JSON produced by the PowerShell command
// that resolves all four $PROFILE scope paths.
type profilePathsResponse struct {
	CurrentUserCurrentHost string `json:"CurrentUserCurrentHost"`
	CurrentUserAllHosts    string `json:"CurrentUserAllHosts"`
	AllUsersCurrentHost    string `json:"AllUsersCurrentHost"`
	AllUsersAllHosts       string `json:"AllUsersAllHosts"`
}

// ListShellProfiles implements platform.Platform.
//
// Queries PowerShell for the four canonical $PROFILE scope paths, then reads
// and hashes each file directly in Go. Files that do not exist on disk are
// represented with an empty SHA256 — this is normal for most scopes.
//
// The -NoProfile flag on the PowerShell call is critical: it prevents recursive
// execution of the very profile files we are auditing.
func (w *WindowsPlatform) ListShellProfiles(ctx context.Context) ([]platform.ShellProfile, error) {
	const psCmd = `[PSCustomObject]@{` +
		`CurrentUserCurrentHost = $PROFILE.CurrentUserCurrentHost;` +
		`CurrentUserAllHosts    = $PROFILE.CurrentUserAllHosts;` +
		`AllUsersCurrentHost    = $PROFILE.AllUsersCurrentHost;` +
		`AllUsersAllHosts       = $PROFILE.AllUsersAllHosts` +
		`} | ConvertTo-Json -Compress`

	raw, err := runPS(ctx, psCmd)
	if err != nil {
		return nil, fmt.Errorf("profile: query profile paths: %w", err)
	}
	if raw == nil {
		// PowerShell unavailable (non-Windows).
		return nil, nil
	}

	var paths profilePathsResponse
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil, fmt.Errorf("profile: parse profile paths JSON: %w", err)
	}

	scopes := []struct {
		name string
		path string
	}{
		{"CurrentUserCurrentHost", paths.CurrentUserCurrentHost},
		{"CurrentUserAllHosts", paths.CurrentUserAllHosts},
		{"AllUsersCurrentHost", paths.AllUsersCurrentHost},
		{"AllUsersAllHosts", paths.AllUsersAllHosts},
	}

	profiles := make([]platform.ShellProfile, 0, len(scopes))
	for _, s := range scopes {
		p := platform.ShellProfile{
			Scope: s.name,
			Path:  s.path,
		}
		if s.path != "" {
			sha, err := hashProfileFile(s.path)
			if err != nil {
				slog.Debug("profile: cannot hash scope file", "scope", s.name, "path", s.path, "err", err)
			}
			p.SHA256 = sha
		}
		profiles = append(profiles, p)
	}

	return profiles, nil
}

// hashProfileFile returns the SHA-256 of the file at path.
// Returns empty string if the file does not exist (not an error — the user
// simply has not created a profile for that scope).
// Returns empty string on other I/O errors, logging a debug message.
func hashProfileFile(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}
	sha, err := hash.File(path)
	if err != nil {
		return "", fmt.Errorf("hash profile file %s: %w", path, err)
	}
	return sha, nil
}
