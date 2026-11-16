package windows

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// BV-249 §3.2: what the engine measures for Grok Build's P2 is what the reader
// reads from the block the report copies.
func TestGrokP2BlockIsWhatIsMeasured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(profiles.GuidedBlock(profiles.GuidedGrok, profiles.P2)), 0o600); err != nil {
		t.Fatal(err)
	}
	read := readGrokLayer(path, platform.GrokLayerUser, "", false)
	if read.Status != platform.MCPSourceRead {
		t.Fatalf("block not read: %+v", read)
	}
	applied, ok := profiles.GrokApply(platform.GrokScan{ConfigDir: dir}, profiles.P2)
	if !ok || len(applied.Layers) != 1 {
		t.Fatalf("not applied: %+v", applied)
	}
	got := applied.Layers[0]
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"permission_mode", got.PermissionMode, read.PermissionMode},
		{"sandbox", got.SandboxProfile, read.SandboxProfile},
		{"rules", got.Rules, read.Rules},
		{"deny", got.Deny, read.Deny},
		{"compat", got.Compat, read.Compat},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: applied %v, read %v", c.name, c.got, c.want)
		}
	}
}
