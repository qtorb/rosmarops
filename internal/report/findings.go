package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
)

// FindingCard is one finding as the «No me afecta» window and the console
// list show it (BV-184): the same title and «what it means» the report gives,
// plus whether it is new, the date it has been open, and the agent. It holds
// no secret and no graph detail — only what the person reads to decide.
type FindingCard struct {
	ID          string // the finding id (f_… or cred_…), the dismissal key
	Severity    string // rules.SeverityRed or rules.SeverityYellow
	Title       string // the finding's title, as the report shows it
	WhatItMeans string // the report's «what it means» text for this finding
	WhenApplies string // findings.when.<rule> (or .default)
	New         bool   // memory verdict «new»: not in the last checked run
	Since       string // the localized «open since» date; empty when New
	Agent       string // the agent of the finding; empty for a machine finding
	// Group is "new", "rest" or "dismissed" — the window's three sections, in
	// that order; red before yellow and then the report's order inside each.
	Group string
}

// Card group names, in the window's and the console list's order.
const (
	CardGroupNew       = "new"
	CardGroupRest      = "rest"
	CardGroupDismissed = "dismissed"
)

// FindingCards renders, from a built v2 report, the cards the dismiss window
// and the console list show: the active findings split into «new» and «the
// rest», then the dismissed findings that appeared this run. Within each
// group red comes before yellow and then the report's own order, exactly as
// the window lays them out. It renders the title and «what it means» with the
// same htmlCtx the report uses, so they match the report word for word.
func FindingCards(v V2, cat catalog.Catalog, home string) []FindingCard {
	c := htmlCtx{v: v, cat: cat, home: home, score: map[string]*index.AgentIndex{}, rows: credRows(v)}
	card := func(f rules.Finding, group string) FindingCard {
		whenKey := "findings.when." + f.Rule
		if !cat.Has(whenKey) {
			whenKey = "findings.when.default"
		}
		fc := FindingCard{
			ID: f.ID, Severity: f.Severity, Group: group,
			Title: c.titleText(f), WhatItMeans: c.impactText(f),
			WhenApplies: cat.Text(whenKey, nil), Agent: f.Params.Agent,
		}
		if f.Memory != nil {
			if f.Memory.Verdict == memory.VerdictNew {
				fc.New = true
			} else if d, err := time.Parse("2006-01-02", f.Memory.OpenSince); err == nil {
				fc.Since = cat.DayMonthLabel(d)
			}
		}
		return fc
	}
	// Within a group: red before yellow, keeping the report's order (v.Findings
	// and v.Dismissed are already sorted by the engine).
	group := func(src []rules.Finding, pick func(rules.Finding) bool, name string) []FindingCard {
		var reds, yellows []FindingCard
		for _, f := range src {
			if pick != nil && !pick(f) {
				continue
			}
			cc := card(f, name)
			if f.Severity == rules.SeverityRed {
				reds = append(reds, cc)
			} else {
				yellows = append(yellows, cc)
			}
		}
		return append(reds, yellows...)
	}
	isNew := func(f rules.Finding) bool { return f.Memory != nil && f.Memory.Verdict == memory.VerdictNew }
	out := group(v.Findings, isNew, CardGroupNew)
	out = append(out, group(v.Findings, func(f rules.Finding) bool { return !isNew(f) }, CardGroupRest)...)
	out = append(out, group(v.Dismissed, nil, CardGroupDismissed)...)
	return out
}

// ReadV2 reads <dataDir>/report.v2.json back into a V2 for `rosmarops
// findings` (BV-184): the exported fields come from the JSON; repoRoots (the
// configured repositories) and the minute limit are supplied by the caller,
// since the JSON carries no prose and no configuration. It is enough to render
// the finding cards; it does not rebuild the whole report.
func ReadV2(dataDir string, repoRoots []string, limitMinutes int) (V2, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, V2FileName))
	if err != nil {
		return V2{}, fmt.Errorf("read %s: %w", V2FileName, err)
	}
	var v V2
	if err := json.Unmarshal(raw, &v); err != nil {
		return V2{}, fmt.Errorf("parse %s: %w", V2FileName, err)
	}
	v.repoRoots = repoRoots
	v.repos = len(repoRoots)
	v.limitMinutes = limitMinutes
	v.memoryPrev = true // the cards read f.Memory directly, present in the JSON
	return v, nil
}
