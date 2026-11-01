package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// V2FileName is the v2 report written to the reports directory. Like the
// graph it reads, its name is fixed: each run replaces it.
const V2FileName = "report.v2.json"

// V2 mirrors schemas/v2/report.v2.schema.json (decision 2026-10-24, informe
// legible, §6, E9). It carries no prose: the Markdown takes its texts from
// the message catalog.
type V2 struct {
	SchemaVersion int             `json:"schema_version"`
	GeneratedAt   string          `json:"generated_at"`
	Provenance    V2Provenance    `json:"provenance"`
	Status        string          `json:"status"`
	Today         rules.Today     `json:"today"`
	Index         index.Index     `json:"index"`
	Memory        memory.Root     `json:"memory"`
	Findings      []rules.Finding `json:"findings"`
	Posture       []rules.Posture `json:"posture"`
	// AgentsOverview is «how your agents work» (decision 2026-10-25, cómo
	// trabajan tus agentes, cálculo, T10): one element per detected agent.
	AgentsOverview []rules.OverviewAgent `json:"agents_overview"`
	// NotificationUnavailable is why a scheduled review's notification did
	// not show (decision 2026-10-26, integridad y revisión semanal, J7);
	// absent otherwise.
	NotificationUnavailable string     `json:"notification_unavailable,omitempty"`
	Coverage                V2Coverage `json:"coverage"`
	Legacy                  V2Legacy   `json:"legacy"`
	// Silenced is the test data the user silenced (M10–M11), always
	// shown; Signals the red technical-report categories (adjustment B).
	Silenced rules.Silenced `json:"silenced"`
	Signals  []Signal       `json:"signals"`
	// TestData are the keys treated as test data by default (decision
	// 2026-10-26, ruido del informe, point 1): shown, not in the JSON.
	TestData rules.TestData `json:"-"`
	// NewAgents are the non-MCP agents new since the last checked run, by name
	// (v3 §13.3; A1). NewMCP are the new MCP servers and MCPChanged the servers
	// that now point somewhere else (BV-91, D3/D4), each with its agent and
	// destination.
	NewAgents      []string         `json:"-"`
	NewMCP         []MCPServerLine  `json:"-"`
	MCPChanged     []MCPServerLine  `json:"-"`
	repos          int              // configured repositories
	nextMemory     memory.File      // findings-memory.json after this run
	memoryPrev     bool             // an earlier findings-memory.json was read
	memoryLost     bool             // B8: prior runs, but no memory to compare with
	historyMissing bool             // B8: prior runs, but no index-history.json
	hidden         []HiddenEntry    // B9: what the configuration hid, by entry
	base           string           // the run's report base name
	series         []int            // the evaluated agent's comparable scores, oldest first
	seriesBy       map[string][]int // every evaluated agent\'s, by name (v3 §10.4)
	// vscodeFirstRun is the first run of the extensions collector (A5).
	vscodeFirstRun *VSCodeFirstRun
	// agentNames are the graph's agent names by node id, for the target
	// agents of agents_overview (v3 §11.4).
	agentNames map[string]string
	// radiusProj are, per agent id and radius_only action id, the score
	// after that action (I6; v3 §15.3); repoRoots the configured roots, for
	// {projects} (v3 §15.2).
	radiusProj map[string]map[string]int
	repoRoots  []string
	// integrityNotChecked is V2Input's (v3 §17.5).
	integrityNotChecked bool
	exePath             string
	weekly              scheduledtasks.Weekly
	// OS is the operating system of the run, from the v1 report's platform
	// ("windows", "macos", "linux"): the macOS preview's texts, paths and
	// sections (decision 2026-10-29, macOS vista previa).
	OS string `json:"-"`
	// NotChecked are the checks that do not apply on this system yet, by
	// code (NotCheckedCodes), in that order: shown, not in the JSON
	// (decision 2026-10-29, macOS vista previa, point 3).
	NotChecked []string `json:"-"`
	// SpaceConnected is true when this computer is connected to the online
	// space (BV-137): the «Your space» section is shown only then. Set by the
	// command before the HTML is rendered; not in the JSON.
	SpaceConnected bool `json:"-"`
	// SpaceSentDate is the local date of the most recent successful send to
	// the space, or empty when nothing has been sent yet (pending). Drives the
	// section's sent/pending line. Not in the JSON.
	SpaceSentDate string `json:"-"`
}

// NotCheckedCodes are the codes of NotChecked, in the order they are shown;
// each has its html.na.<code> text.
var NotCheckedCodes = []string{"scheduledtasks", "avexclusions", "shellprofile", "python_cross", "weekly", "display_language"}

// macOS reports a run on macOS.
func (v V2) macOS() bool { return v.OS == "macos" }

// homePrefix is how the agent prompt names the user's folder: "~" on
// macOS, "%USERPROFILE%" elsewhere (decision 2026-10-29, point 6).
func (v V2) homePrefix() string {
	if v.macOS() {
		return "~"
	}
	return "%USERPROFILE%"
}

// osKey is key, or its macOS text (key + ".macos") on macOS (decision
// 2026-10-29, point 5).
func (v V2) osKey(key string) string {
	if v.macOS() {
		return key + ".macos"
	}
	return key
}

// notChecked keeps the known codes of in, once each, in NotCheckedCodes'
// order; only on macOS, whose texts they have (html.na.title names it).
func notChecked(os string, in []string) []string {
	if os != "macos" {
		return nil
	}
	got := map[string]bool{}
	for _, c := range in {
		got[c] = true
	}
	var out []string
	for _, c := range NotCheckedCodes {
		if got[c] {
			out = append(out, c)
		}
	}
	return out
}

// NextMemory is findings-memory.json after this run, to write.
func (v V2) NextMemory() memory.File { return v.nextMemory }

// VSCodeFirstRun is a first run of the VS Code extensions collector (A5):
// Recorded extensions taken as the starting point, Recent of them reported
// because they were installed in the last 7 days.
type VSCodeFirstRun struct {
	Recorded, Recent int
}

// V2Provenance is the graph's provenance, the SHA-256 of the graph.v2.json
// evaluated, and the rules version (graph and hash null when no graph was
// built).
type V2Provenance struct {
	Graph        *graph.Provenance `json:"graph"`
	GraphSHA256  *string           `json:"graph_sha256"`
	RulesVersion string            `json:"rules_version"`
	// IntelVersion and IntelPublishedAt are the embedded threat-data snapshot
	// used for the cross (BV-100); empty when no snapshot was loaded.
	IntelVersion     string `json:"intel_version,omitempty"`
	IntelPublishedAt string `json:"intel_published_at,omitempty"`
}

// V2Coverage summarises the graph's coverage and the run's scan errors.
type V2Coverage struct {
	GraphBuilt        bool                     `json:"graph_built"`
	SourcesRead       int                      `json:"sources_read"`
	SourcesFailed     []graph.SourceFailed     `json:"sources_failed"`
	AgentsUnsupported []graph.AgentUnsupported `json:"agents_unsupported"`
	ScanErrors        []string                 `json:"scan_errors"`
}

// V2Legacy is the v1 findings, grouped (§3, E7).
type V2Legacy struct {
	Status string        `json:"status"`
	Counts LegacyCounts  `json:"counts"`
	Groups []LegacyGroup `json:"groups"`
}

// LegacyCounts are the v1 findings by severity.
type LegacyCounts struct {
	Red    int `json:"red"`
	Yellow int `json:"yellow"`
	Info   int `json:"info"`
}

// LegacyGroup is one row of v1 findings: same category, pattern or detail,
// and repository family. Severity is the highest of its members.
type LegacyGroup struct {
	Category   string   `json:"category"`
	Detail     string   `json:"detail"`
	Family     string   `json:"family"`
	Severity   string   `json:"severity"`
	Count      int      `json:"count"`
	Repos      []string `json:"repos"`
	FindingIDs []string `json:"finding_ids"`
	AlsoInV2   bool     `json:"also_in_v2,omitempty"`

	// members are what identifies each finding of the group, for the
	// Markdown (BV-39); report.v2.json keeps finding_ids and repos.
	members []string
}

// V2Input is what the v2 report is built from.
type V2Input struct {
	// Graph is the graph written this run, nil when none could be built;
	// GraphSHA256 the SHA-256 of its file.
	Graph       *graph.Graph
	GraphSHA256 string

	Base knowledge.Base
	// Bases are every knowledge base by product (decision 2026-10-25,
	// Cursor); empty means Base alone.
	Bases     map[string]knowledge.Base
	RepoRoots []string

	// Families are the repository families of the run (BV-33), each a list
	// of roots; a root in none is its own family.
	Families [][]string

	V1          Report
	GeneratedAt time.Time

	// Previous is the last entry of index-history.json (nil on a first run
	// or after a reset); HistoryReset says the history was set aside.
	Previous     *index.Entry
	HistoryReset bool
	// History is every entry of index-history.json, for the evolution line
	// of the HTML (informe HTML v2, §5).
	History []index.Entry

	// Salt is the local salt (the memory's references); Memory the last
	// findings-memory.json, MemoryReset true when it was set aside.
	Salt        []byte
	Memory      memory.File
	MemoryReset bool

	// AllowHits are the warnings the configuration's allow_list hid in the
	// v1 scanners (decision 2026-10-27, blindaje, B9); the rotated
	// credentials' come from the rules layer.
	AllowHits []config.AllowHit

	// PriorRuns: RosmarOps ran before on this computer, by the state
	// (last_run) or by a report in the reports directory (report.html or
	// report-*.md). With no memory, or no history, to compare with, the
	// report says they were lost, never «first run» (decision 2026-10-27,
	// blindaje, B8).
	PriorRuns bool

	// Rotated and Fixtures are allow_list.rotated_credentials and
	// test_fixtures; ConfigFile the configuration file as the report shows
	// it (decision 2026-10-24, memoria, M9–M10).
	Rotated    []config.AllowEntryRotated
	Fixtures   []config.AllowEntryTestFixture
	ConfigFile string

	// VSCodeFirstRun is set on the first run of the VS Code extensions
	// collector (decision 2026-10-26, agentes y MCP nuevos, A5).
	VSCodeFirstRun *VSCodeFirstRun

	// ClaudeSettings is the Claude Code settings scan the graph was built
	// from (the model provider row, T12); Home the user's home folder (the
	// probe of agents_overview, annex 2). Both optional.
	ClaudeSettings *platform.ClaudeSettingsScan
	Home           string

	// Hermes is the Hermes Agent scan the graph was built from (BV-111): the
	// overview reads each agent's terminal.backend from it. Optional.
	Hermes platform.HermesScan

	// IntegrityNotChecked: the state could not be read, so this run checked
	// no integrity (decision 2026-10-26, integridad y revisión semanal, J1;
	// informe HTML v3 §17.5).
	IntegrityNotChecked bool

	// ExePath is the absolute path of the rosmarops.exe that made the report
	// and Weekly what the scheduled tasks say about its weekly review (J4,
	// J5; v3 §17.3). NotificationUnavailable is the reason a scheduled
	// review's notification will not show (J7), empty otherwise.
	ExePath                 string
	Weekly                  scheduledtasks.Weekly
	NotificationUnavailable string

	// IntelVersion and IntelPublishedAt are the embedded threat-data snapshot
	// used for the cross (BV-100, F0a); empty when none was loaded. They stamp
	// the provenance and the report's foot line.
	IntelVersion     string
	IntelPublishedAt string

	// NotChecked are the checks that returned platform.ErrNotApplicable on
	// this system, by code (NotCheckedCodes); unknown codes are dropped
	// and the rest shown in NotCheckedCodes' order, on macOS only.
	NotChecked []string
}

// bases are the knowledge bases by product: Bases, or Base alone.
func (in V2Input) bases() map[string]knowledge.Base {
	if len(in.Bases) > 0 {
		return in.Bases
	}
	return map[string]knowledge.Base{in.Base.Product: in.Base}
}

// inference are the products with a knowledge base: an agent of one of
// them is evaluated (decision 2026-10-25, Cursor).
func inference(bases map[string]knowledge.Base) map[string]bool {
	out := map[string]bool{}
	for p := range bases {
		out[p] = true
	}
	return out
}

// BuildV2 builds the v2 report: the rules over the graph, the v1 findings
// grouped, and the status, the highest of v1 and v2 (fail-secure: no graph
// is at least yellow). It is a pure function.
func BuildV2(in V2Input) (V2, rules.Result) {
	fams := map[string]string{}
	for _, fam := range in.Families {
		for _, root := range fam {
			fams[graph.CanonicalPath(root)] = fam[0]
		}
	}
	v := V2{
		SchemaVersion: 2,
		GeneratedAt:   in.GeneratedAt.UTC().Format(time.RFC3339),
		Provenance:    V2Provenance{RulesVersion: rules.Version, IntelVersion: in.IntelVersion, IntelPublishedAt: in.IntelPublishedAt},
		Findings:      []rules.Finding{},
		Posture:       []rules.Posture{},
		repos:         len(in.RepoRoots),
		repoRoots:     in.RepoRoots,

		integrityNotChecked: in.IntegrityNotChecked,
		exePath:             in.ExePath,
		weekly:              in.Weekly,
		OS:                  in.V1.Platform.OS,
		NotChecked:          notChecked(in.V1.Platform.OS, in.NotChecked),

		NotificationUnavailable: in.NotificationUnavailable,
	}
	v2Status := finding.StatusYellow // no graph: fail-secure
	var res rules.Result
	if in.Graph != nil {
		g := *in.Graph
		prov, sha := g.Provenance, in.GraphSHA256
		v.Provenance.Graph, v.Provenance.GraphSHA256 = &prov, &sha
		res = rules.Evaluate(rules.Input{
			Graph: g, Base: in.Base, Bases: in.bases(), RepoRoots: in.RepoRoots, Families: fams,
			Committed: committedOf(in.V1.Findings), Rotated: in.Rotated, Fixtures: in.Fixtures,
			ConfigFile: in.ConfigFile, Now: in.GeneratedAt, ClaudeSettings: in.ClaudeSettings, Home: in.Home, Hermes: in.Hermes,
		})
		v.Silenced, v.TestData = res.Silenced, res.TestData
		v.agentNames = map[string]string{}
		for _, n := range g.Nodes {
			if n.Kind == graph.KindAgent {
				v.agentNames[n.ID] = n.Name
			}
		}
		v.Findings, v.Today, v.Posture = res.Findings, res.Today, res.Posture
		if v.Findings == nil {
			v.Findings = []rules.Finding{}
		}
		v.Coverage = V2Coverage{
			GraphBuilt:        true,
			SourcesRead:       len(g.Coverage.SourcesRead),
			SourcesFailed:     g.Coverage.SourcesFailed,
			AgentsUnsupported: g.Coverage.AgentsUnsupported,
		}
		v2Status = reduceV2(v.Findings, g.Coverage.SourcesFailed)
	} else {
		v.Today = rules.Today{Code: rules.TodayNothingUrgent}
	}
	if v.Coverage.SourcesFailed == nil {
		v.Coverage.SourcesFailed = []graph.SourceFailed{}
	}
	if v.Coverage.AgentsUnsupported == nil {
		v.Coverage.AgentsUnsupported = []graph.AgentUnsupported{}
	}
	v.Coverage.ScanErrors = scanErrorScopes(in.V1.Errors)
	v.hidden = hiddenEntries(append(append([]config.AllowHit{}, in.AllowHits...), res.Silenced.Rotated...))
	v.Index, v.radiusProj = buildIndex(in, res, v)
	v.historyMissing = in.PriorRuns && !in.HistoryReset && len(in.History) == 0 // B8
	v.AgentsOverview = agentsOverview(res.AgentsOverview, v.Index)
	// The technical report keeps the v1 categories of §2.1 that do not
	// pass to v2 and are not retired (M5): only they count in its status.
	tech := technicalFindings(in.V1.Findings)
	counts := LegacyCounts{}
	for _, f := range tech {
		switch f.Severity {
		case finding.SeverityRed:
			counts.Red++
		case finding.SeverityYellow:
			counts.Yellow++
		case finding.SeverityInfo:
			counts.Info++
		}
	}
	legacyStatus := string(finding.Reducer(tech, len(in.V1.Errors)))
	v.Legacy = V2Legacy{
		Status: legacyStatus,
		Counts: counts,
		Groups: groupLegacy(tech, fams, reportedFiles(v.Findings)),
	}
	v.Signals = signals(in.V1.Findings)
	v.vscodeFirstRun = in.VSCodeFirstRun
	if v.Silenced.Entries == nil {
		v.Silenced.Entries = []rules.SilencedEntry{}
	}
	setPostureCount(v.Posture, rules.PostureEnvFilesTracked, envFilesTracked(in.V1.Findings, committedOf(in.V1.Findings)))
	v.Status = string(maxStatus(finding.Status(legacyStatus), v2Status))
	// The memory names the run's report, which needs the status.
	applyMemory(&v, in)
	v.base = FilenameV2(in.V1, v)
	// The evolution follows the step 1 score: the evaluated agent with the
	// lowest score, ties by name (informe HTML v3 §3 point 6).
	v.series = []int{}
	var low *index.AgentIndex
	for i := range v.Index.Agents {
		a := &v.Index.Agents[i]
		if a.Value != nil && (low == nil || *a.Value < *low.Value || *a.Value == *low.Value && a.Agent < low.Agent) {
			low = a
		}
	}
	if low != nil {
		v.series = index.Series(in.History, index.EntryOf(v.Index, ""), low.Agent)
	}
	v.seriesBy = map[string][]int{}
	for _, a := range v.Index.Agents {
		v.seriesBy[a.Agent] = index.Series(in.History, index.EntryOf(v.Index, ""), a.Agent)
	}
	return v, res
}

// agentsOverview is the rules' overview with every evaluated agent whose
// score is unavailable for coverage turned into not_evaluated coverage
// (decision 2026-10-25, cómo trabajan tus agentes, cálculo, §5). No graph:
// none.
func agentsOverview(ov []rules.OverviewAgent, idx index.Index) []rules.OverviewAgent {
	out := make([]rules.OverviewAgent, 0, len(ov))
	for _, a := range ov {
		for _, ai := range idx.Agents {
			if a.Evaluated && ai.AgentID == a.AgentID && ai.Unavailable == index.ReasonCoverage {
				r := rules.NotEvaluatedCoverage
				a.Evaluated, a.NotEvaluatedReason, a.Rows = false, &r, rules.NotEvaluatedRows()
			}
		}
		out = append(out, a)
	}
	return out
}

// statusRank orders statuses; anything unknown counts as yellow
// (fail-secure).
// applyMemory sets each finding's verdict and the run's memory against the
// last checked run (memoria, M1–M4). The run is complete when the graph was
// built and no agent configuration source failed (decision 2026-10-26,
// agentes y MCP nuevos, A1): the base of the findings' verdicts and of the
// new agents and MCP servers.
func applyMemory(v *V2, in V2Input) {
	run := memory.Run{
		Date: in.GeneratedAt.Local().Format("2006-01-02"), Report: FilenameV2(in.V1, *v),
		RulesVersion: rules.Version, KnowledgeVersion: knowledge.CompositeOf(in.bases()),
		Agents: v.Index.Scope.Evaluated, Repos: len(in.RepoRoots),
		Complete: in.Graph != nil && len(v.Coverage.SourcesFailed) == 0,
	}
	run.MemoryLost = in.PriorRuns && in.Memory.Runs == 0
	if in.Graph != nil {
		run.Seen = seenAgents(*in.Graph)
	}
	cur := make([]memory.Finding, 0, len(v.Findings))
	for _, f := range v.Findings {
		// MemoryUntracked rules are not tracked in the findings memory (F0a,
		// BV-107b); they carry a not_comparable verdict instead (see the
		// verdict loop).
		if MemoryUntracked[f.Rule] {
			continue
		}
		mf := memory.Finding{ID: f.ID, Rule: f.Rule, Severity: f.Severity}
		machine := rules.IsMachineFinding(f) // no agent: the credential is the key
		for i, n := range f.Evidence.Nodes {
			switch {
			case i == 0 && !machine:
				mf.Agent = n
			case strings.HasPrefix(n, "cred_"):
				mf.Creds = append(mf.Creds, n)
			}
		}
		cur = append(cur, mf)
	}
	next, verdicts, root := memory.Update(in.Memory, run, cur, in.Salt)
	root.Reset = in.MemoryReset
	for i := range v.Findings {
		if m, ok := verdicts[v.Findings[i].ID]; ok {
			v.Findings[i].Memory = &m
			continue
		}
		// A MemoryUntracked rule (F0a, BV-107b): a not_comparable verdict
		// satisfies report.v2's required memory object and renders no memory
		// badge, while nothing is persisted to the memory file.
		v.Findings[i].Memory = &rules.FindingMemory{
			Verdict: memory.VerdictNotComparable, OpenSince: run.Date,
			FirstSeen: run.Date, Reappeared: 0,
		}
	}
	v.Memory, v.nextMemory, v.memoryPrev = root, next, in.Memory.Runs > 0
	v.memoryLost = run.MemoryLost
	v.NewAgents = root.NewAgents // A1: from the memory, not from v1
	if in.Graph != nil {
		// BV-91 (D4): the new and changed MCP lines pair the memory's refs with
		// each server's agent and destination from the graph.
		v.NewMCP = mcpServerLines(*in.Graph, root.NewMCP, in.Home)
		v.MCPChanged = mcpServerLines(*in.Graph, root.MCPChanged, in.Home)
	}
}

// MemoryUntracked are the rules the findings memory does not track: their
// findings are not written to findings-memory.json and get a not_comparable
// verdict each run. The memory schema's rule enum is not theirs, and adding
// them would be a memory-schema change.
//   - package_known_malicious (decision F0a, BV-100): always red, re-derived
//     each run from the embedded threat-data snapshot.
//   - project_hook_runs_repo_script (BV-107b): BV-107 added the rule without
//     a memory decision, and writing it failed the memory's schema
//     validation, so the run stopped before saving its state.
//   - mcp_external_action (BV-117/BV-121): a stable configuration finding
//     re-derived each run; tracking it would need a memory-schema change,
//     which BV-121 does not sign.
//
// Every rule is either in the memory schema's enum or here
// (TestMemoryCoversEveryRule).
var MemoryUntracked = map[string]bool{
	rules.RulePackageKnownMalicious:     true,
	rules.RuleProjectHookRunsRepoScript: true,
	rules.RuleMCPExternalAction:         true,
}

// buildIndex computes the Agent Posture Score of the run (I1–I9).
func buildIndex(in V2Input, res rules.Result, v V2) (index.Index, map[string]map[string]int) {
	ii := index.Input{
		GraphBuilt: in.Graph != nil, Inference: inference(in.bases()),
		Findings: v.Findings, Today: v.Today, MediumReadable: map[string]int{},
		SourcesFailed: v.Coverage.SourcesFailed, Repos: len(in.RepoRoots),
		RulesVersion: rules.Version, KnowledgeVersion: knowledge.CompositeOf(in.bases()), Previous: in.Previous,
	}
	if in.Graph != nil {
		for _, n := range in.Graph.Nodes {
			switch {
			case n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient:
				ii.Agents = append(ii.Agents, index.Agent{ID: n.ID, Name: n.Name, Product: n.Platform, ConfigDir: n.ConfigPath})
			case n.Kind == graph.KindAgent && n.Type == graph.AgentExtension:
				// An agent extension is present, never evaluated (A3): the
				// machine score is then not available (I5).
				ii.Agents = append(ii.Agents, index.Agent{ID: n.ID, Name: n.Name, Product: n.Platform})
			}
		}
	}
	for agent, creds := range res.MediumReadable {
		ii.MediumReadable[agent] = len(creds)
	}
	idx := index.Compute(ii)
	if in.Graph == nil {
		// No graph: no agent is known; the score is unavailable, not "not
		// applicable" (fail-secure, I5).
		idx.Machine = index.Score{Unavailable: index.ReasonCoverage}
	}
	idx.HistoryReset = in.HistoryReset
	return idx, index.ActionProjections(ii)
}

func statusRank(s finding.Status) int {
	switch s {
	case finding.StatusRed:
		return 2
	case finding.StatusGreen:
		return 0
	}
	return 1
}

// HiddenEntry is one allow_list entry that hid warnings in this run (B9):
// its list, its identifier and note as written, and how many it hid.
type HiddenEntry struct {
	List, ID, Note string
	Count          int
}

// hiddenEntries groups hits by entry, sorted by list, id and note.
func hiddenEntries(hits []config.AllowHit) []HiddenEntry {
	n := map[config.AllowHit]int{}
	for _, h := range hits {
		n[h]++
	}
	out := make([]HiddenEntry, 0, len(n))
	for h, c := range n {
		out = append(out, HiddenEntry{List: h.List, ID: h.ID, Note: h.Note, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.List != b.List {
			return a.List < b.List
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Note < b.Note
	})
	return out
}

// HiddenCount is the number of warnings the configuration hid (B9).
func (v V2) HiddenCount() int {
	n := 0
	for _, e := range v.hidden {
		n += e.Count
	}
	return n
}

// reduceV2 is the v2 graph's status: the most severe finding's, and at
// least yellow when any source of the graph could not be read (decision
// 2026-10-27, blindaje, B6; invariant 4). Every source in sources_failed is
// an agent's: a configuration or instructions file, or a credential file
// an agent can reach; what was not read is never reported as clean.
func reduceV2(fs []rules.Finding, failed []graph.SourceFailed) finding.Status {
	s := finding.StatusGreen
	if len(failed) > 0 {
		s = finding.StatusYellow
	}
	for _, f := range fs {
		s = maxStatus(s, finding.Status(f.Severity))
	}
	return s
}

func maxStatus(a, b finding.Status) finding.Status {
	return []finding.Status{finding.StatusGreen, finding.StatusYellow, finding.StatusRed}[max(statusRank(a), statusRank(b))]
}

func scanErrorScopes(errs []finding.ScanError) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range errs {
		if !seen[e.Scope] {
			seen[e.Scope] = true
			out = append(out, e.Scope)
		}
	}
	sort.Strings(out)
	return out
}

// reportedFiles are the files (canonical) of the credentials v2 findings
// report: each location's part before ":<line>" or "#".
func reportedFiles(fs []rules.Finding) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		if f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable && f.Rule != rules.RuleCredInGit {
			continue
		}
		for _, file := range graph.CredentialFiles(graph.Node{Locations: f.Evidence.Locations}) {
			out[graph.CanonicalPath(file)] = true
		}
	}
	return out
}

// legacyFile is the file of a v1 finding: its evidence file, under its
// repository when the file is relative.
func legacyFile(f finding.Finding) string {
	file := f.Evidence.File
	if file == "" || f.Evidence.RepoPath == "" || strings.HasPrefix(file, "/") || strings.Contains(file, ":") {
		return file
	}
	return strings.TrimRight(f.Evidence.RepoPath, `/\`) + "/" + file
}

// legacyMember is what identifies a v1 finding, read from its evidence
// (BV-39): never a preview or a value.
//   - repository secrets: repository + file; history: repository + the
//     commit's first 12 characters;
//   - vscode_extension_new: the extension id;
//   - mcp_*: server (client);
//   - scheduled_task_*: Extra task_name;
//   - the rest, the most specific field: package.json and the script name
//     (lifecycle), the file (workflows, hooks), core.hooksPath (its
//     value), the repository (scan_timeout), kind and value (Defender
//     exclusions), the profile paths (shell profile), manager and
//     package@version (global packages), npmrc_path for a registry (the
//     registry URL can carry credentials), else the file or repository.
func legacyMember(f finding.Finding) string {
	e := f.Evidence
	in := func(file string) string {
		if file == "" {
			return e.RepoPath
		}
		return legacyFile(finding.Finding{Evidence: finding.Evidence{RepoPath: e.RepoPath, File: file}})
	}
	switch {
	case f.Category == "secret_tracked" || f.Category == "env_tracked":
		return in(e.File)
	case f.Category == "secret_in_history":
		commit := e.CommitSHA
		if len(commit) > 12 {
			commit = commit[:12]
		}
		return strings.TrimSpace(e.RepoPath + " @ " + commit)
	case f.Category == "vscode_extension_new":
		return e.ExtensionID
	case strings.HasPrefix(f.Category, "mcp_"):
		if e.MCPServer == "" {
			return ""
		}
		return e.MCPServer + " (" + e.MCPClient + ")"
	case strings.HasPrefix(f.Category, "scheduled_task_"):
		return extra(e, "task_name")
	case f.Category == "lifecycle_script_with_network":
		return in(e.File) + " " + extra(e, "script_name")
	case f.Category == "core_hookspath_redirected":
		return e.RepoPath + " core.hooksPath=" + extra(e, "core_hooks_path")
	case f.Category == "av_exclusion_new":
		return extra(e, "exclusion_kind") + " " + extra(e, "exclusion_value")
	case f.Category == "shell_profile_modified":
		var paths []string
		for _, k := range extraKeys(e) {
			if strings.HasPrefix(k, "profile_") && strings.HasSuffix(k, "_path") {
				paths = append(paths, extra(e, k))
			}
		}
		return strings.Join(paths, ", ")
	case f.Category == "global_package_new":
		return extra(e, "package_manager") + " " + extra(e, "package_name") + "@" + extra(e, "package_version")
	case f.Category == "config_key_case_conflict": // B5: the file and the key
		return in(e.File) + "#" + extra(e, "key")
	case f.Category == "npm_registry_non_standard":
		return extra(e, "npmrc_path")
	}
	return in(e.File)
}

// extra reads a string from a finding's Extra map.
func extra(e finding.Evidence, key string) string {
	switch m := e.Extra.(type) {
	case map[string]string:
		return m[key]
	case map[string]any:
		if s, ok := m[key].(string); ok {
			return s
		}
	}
	return ""
}

// extraKeys are the keys of a finding's Extra map, sorted.
func extraKeys(e finding.Evidence) []string {
	var keys []string
	switch m := e.Extra.(type) {
	case map[string]string:
		for k := range m {
			keys = append(keys, k)
		}
	case map[string]any:
		for k := range m {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(ss []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// groupLegacy groups v1 findings by (category, pattern or detail, family).
// A finding with no detail (outside the repository checks) is a group of
// its own; one outside every repository has family "". A group with a
// finding in a file of a credential v2 reports is also_in_v2 (dedup by
// file, BV-38).
func groupLegacy(fs []finding.Finding, fams map[string]string, v2Files map[string]bool) []LegacyGroup {
	type key struct{ category, detail, family string }
	groups := map[key]*LegacyGroup{}
	repos := map[key]map[string]bool{}
	for _, f := range fs {
		detail := f.GroupDetail
		if detail == "" {
			// A finding outside the repository checks: its readable
			// identifier, not its hashed id (BV-39).
			if detail = legacyMember(f); detail == "" {
				detail = f.ID
			}
		}
		family := ""
		if f.Evidence.RepoPath != "" {
			family = f.Evidence.RepoPath
			if fam, ok := fams[graph.CanonicalPath(f.Evidence.RepoPath)]; ok {
				family = fam
			}
		}
		k := key{f.Category, detail, family}
		g, ok := groups[k]
		if !ok {
			g = &LegacyGroup{Category: f.Category, Detail: detail, Family: family, Severity: string(f.Severity)}
			groups[k] = g
			repos[k] = map[string]bool{}
		}
		g.Count++
		g.FindingIDs = append(g.FindingIDs, f.ID)
		if file := legacyFile(f); file != "" && v2Files[graph.CanonicalPath(file)] {
			g.AlsoInV2 = true
		}
		if severityOrder(f.Severity) < severityOrder(finding.Severity(g.Severity)) {
			g.Severity = string(f.Severity)
		}
		if f.Evidence.RepoPath != "" {
			repos[k][f.Evidence.RepoPath] = true
		}
		if m := legacyMember(f); m != "" {
			g.members = append(g.members, m)
		}
	}
	out := make([]LegacyGroup, 0, len(groups))
	for k, g := range groups {
		g.Repos = []string{}
		for r := range repos[k] {
			g.Repos = append(g.Repos, r)
		}
		sort.Strings(g.Repos)
		sort.Strings(g.FindingIDs)
		g.members = uniqueSorted(g.members)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if sa, sb := severityOrder(finding.Severity(a.Severity)), severityOrder(finding.Severity(b.Severity)); sa != sb {
			return sa < sb
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Detail != b.Detail {
			return a.Detail < b.Detail
		}
		return a.Family < b.Family
	})
	return out
}

// RenderV2JSON serialises v and validates it against the embedded
// report.v2 schema: an invalid report is an error, never written.
func RenderV2JSON(v V2) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report v2: %w", err)
	}
	schema, err := schemaval.Compile(schemas.ReportV2Schema)
	if err != nil {
		return nil, fmt.Errorf("compile report v2 schema: %w", err)
	}
	result, err := schema.Validate(data)
	if err != nil {
		return nil, fmt.Errorf("validate report v2: %w", err)
	}
	if !result.Valid() {
		msgs := ""
		for _, e := range result.Errors() {
			msgs += "\n  " + e.String()
		}
		return nil, fmt.Errorf("report v2 fails schema validation:%s", msgs)
	}
	return append(data, '\n'), nil
}

// V2Counts are the red and yellow findings of v2.
func V2Counts(v V2) (red, yellow int) {
	for _, f := range v.Findings {
		switch f.Severity {
		case rules.SeverityRed:
			red++
		case rules.SeverityYellow:
			yellow++
		}
	}
	return red, yellow
}

// FilenameV2 is Filename with the run's status (the highest of v1 and v2,
// fail-secure) and the red and yellow counts of v2 only (BV-39).
func FilenameV2(r Report, v V2) string {
	r.Header.Status = v.Status
	r.Header.Counts.Red, r.Header.Counts.Yellow = V2Counts(v)
	return Filename(r)
}

// ConsoleSummary is the run's two console lines, from the catalog
// (BV-39): the status and the v2 and legacy counts, then the report path.
func ConsoleSummary(v V2, cat catalog.Catalog, path string) string {
	red, yellow := RowCounts(v) // BV-55: rows, as the report shows them
	summary := cat.Text("console.summary", map[string]string{
		"status": cat.Text("report.status."+v.Status, nil),
		"red":    strconv.Itoa(red), "yellow": strconv.Itoa(yellow),
		"legacy_red": strconv.Itoa(v.Legacy.Counts.Red), "legacy_yellow": strconv.Itoa(v.Legacy.Counts.Yellow),
	})
	return summary + "\n" + cat.Text("console.report", map[string]string{"path": path}) + "\n"
}

// WriteV2 writes the run's report files to dir: the Markdown (the v2
// report, with v1 in its legacy section), the v1 JSON sidecar as before,
// and report.v2.json. The v2 JSON is validated before anything is written.
// The Markdown is the technical report: always English, whatever --lang
// (BV-45).
func WriteV2(r Report, v V2, loc *time.Location, dir string) (mdPath, jsonPath, v2Path string, err error) {
	cat, err := catalog.Load(catalog.DefaultLang)
	if err != nil {
		return "", "", "", fmt.Errorf("message catalog: %w", err)
	}
	v2JSON, err := RenderV2JSON(v)
	if err != nil {
		return "", "", "", err
	}
	v1JSON, err := RenderJSON(r)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", fmt.Errorf("create report dir %s: %w", dir, err)
	}
	base := FilenameV2(r, v)
	mdPath = filepath.Join(dir, base+".md")
	jsonPath = filepath.Join(dir, base+".json")
	v2Path = filepath.Join(dir, V2FileName)
	if err := os.WriteFile(mdPath, []byte(RenderV2Markdown(v, cat, loc)), 0o600); err != nil {
		return "", "", "", fmt.Errorf("write markdown report: %w", err)
	}
	if err := os.WriteFile(jsonPath, v1JSON, 0o600); err != nil {
		return "", "", "", fmt.Errorf("write json sidecar: %w", err)
	}
	if err := os.WriteFile(v2Path, v2JSON, 0o600); err != nil {
		return "", "", "", fmt.Errorf("write report v2: %w", err)
	}
	return mdPath, jsonPath, v2Path, nil
}
