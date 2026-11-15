package report_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-236 §5: an accepted finding («Lo asumo») shows in its own group of the
// window and the console, after «the rest» and before «dismissed», never among
// the pending ones.
func TestBV236_FindingCardsAccepted(t *testing.T) {
	v, err := report.ReadV2("../schemaval/testdata/valid", []string{"/home/dev/src/webapp"}, 0)
	if err != nil {
		t.Fatalf("ReadV2: %v", err)
	}
	if len(v.Findings) == 0 {
		t.Fatal("fixture has no findings")
	}
	v.Findings[0].Accepted = &rules.Accepted{Date: "2026-11-14"}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	cards := report.FindingCards(v, cat, "/h")
	order := map[string]int{report.CardGroupNew: 0, report.CardGroupRest: 1, report.CardGroupAccepted: 2, report.CardGroupDismissed: 3}
	last := -1
	found := false
	for _, c := range cards {
		if order[c.Group] < last {
			t.Errorf("card %s (%s) out of group order", c.ID, c.Group)
		}
		last = order[c.Group]
		if c.ID == v.Findings[0].ID {
			found = true
			if c.Group != report.CardGroupAccepted {
				t.Errorf("accepted finding in group %q", c.Group)
			}
		}
	}
	if !found {
		t.Error("the accepted finding is not shown")
	}
}
