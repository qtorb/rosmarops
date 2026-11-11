package exposure

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/agents"
)

// TestComplexityRegistryMissing (CTO-31 M1): an entry not marked Complete, or
// one with no sources, gives complexity no data (registry_missing), never a
// level — Cursor declares no source and must not come out as level 0. Today no
// entry is Complete, so every product has no complexity data.
func TestComplexityRegistryMissing(t *testing.T) {
	for _, e := range agents.All() {
		if e.Complete {
			t.Errorf("%s: registry entry marked Complete before the full registry (CTO-31 §a)", e.Product)
		}
		ax := complexity(e.Product)
		if ax.Level != nil || ax.NoData != NoDataRegistryMissing {
			t.Errorf("%s: complexity = level %v, no_data %q; want no level and registry_missing", e.Product, ax.Level, ax.NoData)
		}
	}
	if e, ok := agents.EntryOf("cursor"); !ok || len(e.Sources) != 0 {
		t.Fatalf("cursor is the no-sources case: entry %+v", e)
	}
	if ax := complexity("no-such-product"); ax.Level != nil || ax.NoData != NoDataRegistryMissing {
		t.Errorf("unknown product: complexity = %+v, want registry_missing", ax)
	}
}
