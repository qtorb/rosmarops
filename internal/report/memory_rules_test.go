package report_test

import (
	"encoding/json"
	"testing"

	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// BV-107b: every rule is either in the rule enum of the findings memory's
// current schema or in report.MemoryUntracked, never both. A rule added
// without deciding whether the memory tracks it fails here, not in a user's
// run (BV-107 wrote a rule the memory schema rejects, and the run stopped
// before saving its state).
func TestMemoryCoversEveryRule(t *testing.T) {
	raw, ok := map[int][]byte{3: schemas.FindingsMemoryV3Schema}[memory.SchemaVersion]
	if !ok {
		t.Fatalf("findings memory schema v%d: not mapped in this test", memory.SchemaVersion)
	}
	var schema struct {
		Definitions struct {
			Entry struct {
				Properties struct {
					Rule struct {
						Enum []string `json:"enum"`
					} `json:"rule"`
				} `json:"properties"`
			} `json:"entry"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	enum := map[string]bool{}
	for _, r := range schema.Definitions.Entry.Properties.Rule.Enum {
		enum[r] = true
	}
	if len(enum) == 0 {
		t.Fatal("no rule enum in the findings memory schema")
	}
	all := map[string]bool{}
	for _, r := range rules.AllRules {
		all[r] = true
		switch {
		case enum[r] && report.MemoryUntracked[r]:
			t.Errorf("%s is in the memory schema and in MemoryUntracked", r)
		case !enum[r] && !report.MemoryUntracked[r]:
			t.Errorf("%s is neither in the memory schema's rule enum nor in MemoryUntracked: decide whether the memory tracks it", r)
		}
	}
	for r := range report.MemoryUntracked {
		if !all[r] {
			t.Errorf("MemoryUntracked has %s, which is not in rules.AllRules", r)
		}
	}
}
