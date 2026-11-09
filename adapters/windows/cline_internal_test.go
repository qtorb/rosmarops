package windows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
)

// BV-203 point 8 (CTO-25 A3): the Cline extension version is read from the
// extension folder name and the highest wins. A string comparison picks 4.9.0
// over 4.10.0 (because "4.9" > "4.10" lexically); the comparison must be
// numeric per dotted component.

func TestClineVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool // a newer than b
	}{
		{"4.10.0", "4.9.0", true},  // the A3 case: 4.10 is newer than 4.9
		{"4.9.0", "4.10.0", false}, // and the other way round
		{"4.9.0", "4.9.0", false},  // equal is not newer
		{"5.0.0", "4.99.99", true}, // major wins
		{"4.10.1", "4.10.0", true}, // patch breaks the tie
		{"4.9", "4.9.0", false},    // trailing zero component equals
		{"4.10", "4.9.0", true},    // short vs long, numeric still wins
	}
	for _, c := range cases {
		if got := clineVersionNewer(c.a, c.b); got != c.want {
			t.Errorf("clineVersionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestClineExtensionVersionPicksNumericHighest drives the real reader over a
// fake extensions folder holding 4.9.0 and 4.10.0, and asserts 4.10.0 wins.
func TestClineExtensionVersionPicksNumericHighest(t *testing.T) {
	home := t.TempDir()
	exts := filepath.Join(home, ".vscode", "extensions")
	for _, v := range []string{"saoudrizwan.claude-dev-4.9.0", "saoudrizwan.claude-dev-4.10.0", "other.extension-1.0.0"} {
		if err := os.MkdirAll(filepath.Join(exts, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := clineExtensionVersion(agentdirs.Layout(0), home, nil); got != "4.10.0" {
		t.Errorf("clineExtensionVersion = %q, want 4.10.0", got)
	}
}
