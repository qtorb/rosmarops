// Package memory is the findings memory between runs (decision 2026-10-24,
// memoria, M1–M4): each v2 finding gets a verdict against the last run that
// was checked, with how long it has been open. The memory lives in
// findings-memory.json in the reports directory and holds no paths,
// services, fingerprints, host_id, values or text: findings, agents and
// credentials are only salted references.
package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// FileName is the memory in the reports directory (M2).
const FileName = "findings-memory.json"

// Retention (§1.2): a finding no longer observed is kept for KeepRuns runs
// or KeepDays days, whichever comes first; never more than MaxEntries.
const (
	KeepRuns   = 26
	KeepDays   = 180
	MaxEntries = 1000
)

// Verdicts (M1).
const (
	VerdictNew              = "new"
	VerdictStillObserved    = "still_observed"
	VerdictObservedAgain    = "observed_again"
	VerdictNoLongerObserved = "no_longer_observed"
	VerdictNotComparable    = "not_comparable"
	VerdictNotChecked       = "not_checked"
)

// AllVerdicts lists the verdicts.
var AllVerdicts = []string{VerdictNew, VerdictStillObserved, VerdictObservedAgain, VerdictNoLongerObserved, VerdictNotComparable, VerdictNotChecked}

// Reasons a run is not comparable with the last checked one (M4).
const (
	ReasonRulesChanged = "rules_changed"
	ReasonScopeChanged = "scope_changed"
	ReasonSaltChanged  = "salt_changed" // A2: the local salt is not the memory's
)

// saltRefLabel is the fixed label of salt_ref (A2), not host_id's.
const saltRefLabel = "rosmarops/findings-memory/salt-ref/v1"

// SaltRef is the salt's reference in the memory: HMAC-SHA256 of the label
// with the salt, 16 hex.
func SaltRef(salt []byte) string { return hash.Keyed(salt, []byte(saltRefLabel))[:16] }

// SchemaVersion is the version written (decision 2026-10-26, agentes y MCP
// nuevos, A2); version 1 is still read.
const SchemaVersion = 2

// File mirrors schemas/v2/findings-memory.v2.schema.json (and v1, read
// without SaltRef and AgentRefs).
type File struct {
	SchemaVersion int      `json:"schema_version"`
	SaltRef       string   `json:"salt_ref,omitempty"`
	Runs          int      `json:"runs"`
	LastChecked   *Checked `json:"last_checked"`
	Entries       []Entry  `json:"entries"`
}

// Checked is the last run whose coverage was complete: the base of the next
// comparison (§1.5).
type Checked struct {
	Run              int      `json:"run"`
	Date             string   `json:"date"`
	RulesVersion     string   `json:"rules_version"`
	KnowledgeVersion string   `json:"knowledge_version"`
	Agents           []string `json:"agents"`
	Repos            int      `json:"repos"`
	// AgentRefs are the salted references of the agents seen in that run,
	// by comparison key (A1). nil: no base (a v1 memory).
	AgentRefs *[]string `json:"agent_refs,omitempty"`
}

// Entry is one finding's memory.
type Entry struct {
	// Mid is HMAC-SHA256 of the finding id with the local salt, 16 hex.
	Mid         string `json:"mid"`
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	FirstSeen   string `json:"first_seen"`
	OpenSince   string `json:"open_since"`
	LastSeen    string `json:"last_seen"`
	LastVerdict string `json:"last_verdict"`
	Reappeared  int    `json:"reappeared"`
	// LastReport is the base name of the last report the finding was in.
	LastReport string `json:"last_report"`

	// LastSeenRun is the run counter when it was last seen (retention by
	// runs); Open says it is in the current streak.
	LastSeenRun int  `json:"last_seen_run"`
	Open        bool `json:"open"`

	// Agent and Creds are salted references to the finding's agent and
	// credentials, for "moved or merged" (§1.3).
	Agent string   `json:"agent"`
	Creds []string `json:"creds"`
}

// Finding is one current finding, by its raw ids (references are salted
// here, never stored raw).
type Finding struct {
	ID, Rule, Severity, Agent string
	Creds                     []string
}

// Run is this run.
type Run struct {
	Date, Report                   string // YYYY-MM-DD; the report's base name
	RulesVersion, KnowledgeVersion string
	Agents                         []string // evaluated agents
	Repos                          int
	// Complete: the graph was built and no agent configuration source
	// failed (A1).
	Complete bool
	// Seen are the agents of this run, by comparison key (A1).
	Seen []SeenAgent
}

// SeenAgent is one agent of the run: its comparison key (A1), whether it is
// an MCP server, and its name for the report.
type SeenAgent struct {
	Key  string
	MCP  bool
	Name string
}

// Root is the run's memory in report.v2.json.
type Root struct {
	Counts           map[string]int `json:"counts"`
	NoLongerObserved []Gone         `json:"no_longer_observed"`
	Moved            []Moved        `json:"moved"`
	Reason           string         `json:"reason,omitempty"`
	Reset            bool           `json:"reset"`

	// NewAgents and NewMCP are the agents and MCP servers new since the
	// last checked run, by name, sorted (A1); none without a base.
	// SaltChanged: the local salt is not the memory's (A2).
	NewAgents   []string `json:"-"`
	NewMCP      []string `json:"-"`
	SaltChanged bool     `json:"-"`
}

// Gone is a finding no longer observed: no detail, only what the memory
// holds and the report it was last in.
type Gone struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	OpenSince  string `json:"open_since"`
	LastReport string `json:"last_report"`
}

// Moved is a pair no_longer_observed + new of one rule and one agent with a
// credential in common, shown as one line (memory.moved).
type Moved struct {
	Rule       string `json:"rule"`
	FindingID  string `json:"finding_id"`
	OpenSince  string `json:"open_since"`
	LastReport string `json:"last_report"`
}

// ref is the salted reference of an id: HMAC-SHA256 with the local salt,
// 16 hex.
func ref(salt []byte, id string) string { return hash.Keyed(salt, []byte(id))[:16] }

// Update computes this run's verdicts against prev and returns the new
// memory, the verdict of each current finding (by finding id) and the run's
// root object (M1, M4).
func Update(prev File, run Run, cur []Finding, salt []byte) (File, map[string]rules.FindingMemory, Root) {
	saltRef := SaltRef(salt)
	next := File{SchemaVersion: SchemaVersion, SaltRef: saltRef, Runs: prev.Runs + 1, LastChecked: prev.LastChecked}
	byMid := map[string]Entry{}
	for _, e := range prev.Entries {
		byMid[e.Mid] = e
	}
	root := Root{Counts: map[string]int{}, NoLongerObserved: []Gone{}, Moved: []Moved{}}
	for _, v := range AllVerdicts {
		root.Counts[v] = 0
	}
	// A2: another salt, nothing compares (a v1 memory has no salt_ref).
	root.SaltChanged = prev.SaltRef != "" && prev.SaltRef != saltRef
	if b := prev.LastChecked; b != nil {
		switch {
		case b.RulesVersion != run.RulesVersion || b.KnowledgeVersion != run.KnowledgeVersion:
			root.Reason = ReasonRulesChanged
		case !sameSet(b.Agents, run.Agents) || float64(run.Repos) < 0.9*float64(b.Repos):
			root.Reason = ReasonScopeChanged
		}
	}
	if root.SaltChanged {
		root.Reason = ReasonSaltChanged // A8: shown as the other reasons
	}
	notComparable := root.Reason != ""

	// A1: the agents and MCP servers not in the last checked run's.
	if b := prev.LastChecked; b != nil && b.AgentRefs != nil && !root.SaltChanged {
		base := map[string]bool{}
		for _, r := range *b.AgentRefs {
			base[r] = true
		}
		for _, a := range run.Seen {
			if base[ref(salt, a.Key)] {
				continue
			}
			if a.MCP {
				root.NewMCP = append(root.NewMCP, a.Name)
			} else {
				root.NewAgents = append(root.NewAgents, a.Name)
			}
		}
		root.NewAgents, root.NewMCP = uniqueSorted(root.NewAgents), uniqueSorted(root.NewMCP)
	}

	verdicts := map[string]rules.FindingMemory{}
	seen := map[string]bool{}
	type newOne struct {
		f   Finding
		mid string
	}
	var news []newOne
	for _, f := range cur {
		mid := ref(salt, f.ID)
		seen[mid] = true
		e, known := byMid[mid]
		var verdict string
		switch {
		case known && e.Open:
			verdict = VerdictStillObserved
		case known:
			verdict = VerdictObservedAgain
			e.Reappeared++
			e.OpenSince = run.Date
		default:
			verdict = VerdictNew
			if notComparable {
				verdict = VerdictNotComparable
			}
			e = Entry{Mid: mid, FirstSeen: run.Date, OpenSince: run.Date}
			if verdict == VerdictNew {
				news = append(news, newOne{f, mid})
			}
		}
		e.Rule, e.Severity, e.LastSeen, e.LastVerdict = f.Rule, f.Severity, run.Date, verdict
		e.LastReport, e.LastSeenRun, e.Open = run.Report, next.Runs, true
		e.Agent, e.Creds = ref(salt, f.Agent), refs(salt, f.Creds)
		byMid[mid] = e
		root.Counts[verdict]++
		verdicts[f.ID] = rules.FindingMemory{Verdict: verdict, OpenSince: e.OpenSince, FirstSeen: e.FirstSeen, Reappeared: e.Reappeared}
	}

	// The open findings this run did not see.
	var gone []Entry
	for mid, e := range byMid {
		if seen[mid] || !e.Open {
			continue
		}
		switch {
		case !run.Complete:
			// Not read is not clean: the finding stays open (§1.5).
			e.LastVerdict = VerdictNotChecked
		case notComparable:
			e.LastVerdict, e.Open = VerdictNotComparable, false
		default:
			e.LastVerdict, e.Open = VerdictNoLongerObserved, false
			gone = append(gone, e)
		}
		root.Counts[e.LastVerdict]++
		byMid[mid] = e
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Mid < gone[j].Mid })
	sort.Slice(news, func(i, j int) bool { return news[i].mid < news[j].mid })

	// Moved or merged (§1.3): a gone and a new of one rule, one agent and a
	// credential in common are one line.
	used := map[string]bool{}
	for _, n := range news {
		for _, g := range gone {
			if used[g.Mid] || g.Rule != n.f.Rule || g.Agent != ref(salt, n.f.Agent) || !overlap(g.Creds, refs(salt, n.f.Creds)) {
				continue
			}
			used[g.Mid] = true
			root.Moved = append(root.Moved, Moved{Rule: g.Rule, FindingID: n.f.ID, OpenSince: g.OpenSince, LastReport: g.LastReport})
			break
		}
	}
	for _, g := range gone {
		if !used[g.Mid] {
			root.NoLongerObserved = append(root.NoLongerObserved, Gone{Rule: g.Rule, Severity: g.Severity, OpenSince: g.OpenSince, LastReport: g.LastReport})
		}
	}

	if run.Complete {
		agentRefs := make([]string, 0, len(run.Seen))
		for _, a := range run.Seen {
			agentRefs = append(agentRefs, ref(salt, a.Key))
		}
		if agentRefs = uniqueSorted(agentRefs); agentRefs == nil {
			agentRefs = []string{} // a base with no agent
		}
		next.LastChecked = &Checked{
			Run: next.Runs, Date: run.Date, RulesVersion: run.RulesVersion, KnowledgeVersion: run.KnowledgeVersion,
			Agents: sortedCopy(run.Agents), Repos: run.Repos, AgentRefs: &agentRefs,
		}
	}
	for _, e := range byMid {
		next.Entries = append(next.Entries, e)
	}
	next.Entries = retain(next.Entries, next.Runs, run.Date)
	return next, verdicts, root
}

// retain applies §1.2: open entries stay; closed ones go after KeepRuns runs
// or KeepDays days; at most MaxEntries, the oldest closed first.
func retain(es []Entry, runs int, today string) []Entry {
	now, _ := time.Parse("2006-01-02", today)
	var keep []Entry
	for _, e := range es {
		if !e.Open {
			last, err := time.Parse("2006-01-02", e.LastSeen)
			if runs-e.LastSeenRun >= KeepRuns || err == nil && now.Sub(last) >= KeepDays*24*time.Hour {
				continue
			}
		}
		keep = append(keep, e)
	}
	sort.Slice(keep, func(i, j int) bool {
		a, b := keep[i], keep[j]
		if a.Open != b.Open {
			return !a.Open // closed first: they go first
		}
		if a.LastSeenRun != b.LastSeenRun {
			return a.LastSeenRun < b.LastSeenRun
		}
		return a.Mid < b.Mid
	})
	if len(keep) > MaxEntries {
		keep = keep[len(keep)-MaxEntries:]
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].Mid < keep[j].Mid })
	if keep == nil {
		keep = []Entry{}
	}
	return keep
}

func refs(salt []byte, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, ref(salt, id))
	}
	sort.Strings(out)
	return out
}

func overlap(a, b []string) bool {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if set[x] {
			return true
		}
	}
	return false
}

func uniqueSorted(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func sortedCopy(ss []string) []string {
	out := append([]string{}, ss...)
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	a, b = sortedCopy(a), sortedCopy(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validate validates raw against the schema of its schema_version: 1 or 2
// (A2, the same pattern as the state). Any other version does not validate.
func validate(raw []byte) error {
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return fmt.Errorf("parse findings memory: %w", err)
	}
	schema := schemas.FindingsMemoryV2Schema
	if head.SchemaVersion == 1 {
		schema = schemas.FindingsMemorySchema
	}
	s, err := schemaval.Compile(schema)
	if err != nil {
		return fmt.Errorf("compile findings memory schema: %w", err)
	}
	r, err := s.Validate(raw)
	if err != nil {
		return fmt.Errorf("validate findings memory: %w", err)
	}
	if !r.Valid() {
		msg := "findings memory fails schema validation:"
		for _, e := range r.Errors() {
			msg += "\n  " + e.String()
		}
		return errors.New(msg)
	}
	return nil
}

// Read reads dir/findings-memory.json, validated. A missing file is an empty
// memory. A file that does not validate is renamed with the suffix .invalid
// in the same directory (a free name, never over another file) and the
// memory starts again: reset is true, for the report to say so (as I9).
func Read(dir string) (f File, reset bool, err error) {
	path := filepath.Join(dir, FileName)
	f = File{SchemaVersion: SchemaVersion, Entries: []Entry{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("read findings memory: %w", err)
	}
	var parsed File
	if verr := validate(raw); verr == nil {
		if jerr := json.Unmarshal(raw, &parsed); jerr == nil {
			return parsed, false, nil
		}
	}
	target := path + ".invalid"
	for n := 1; ; n++ {
		if _, statErr := os.Lstat(target); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		target = path + "." + strconv.Itoa(n) + ".invalid"
	}
	if err := os.Rename(path, target); err != nil {
		return f, false, fmt.Errorf("set aside invalid findings memory: %w", err)
	}
	return f, true, nil
}

// Write validates f and writes dir/findings-memory.json, always as v2.
func Write(dir string, f File) error {
	f.SchemaVersion = SchemaVersion
	if f.Entries == nil {
		f.Entries = []Entry{}
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal findings memory: %w", err)
	}
	if err := validate(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create reports dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, FileName), append(raw, '\n'), 0o600)
}
