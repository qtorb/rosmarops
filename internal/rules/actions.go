package rules

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// Action codes (§2, E5): the knowledge base's mitigations entry, in this
// order.
const (
	ActionAskEditRule       = "ask_edit_rule"
	ActionAskReadRule       = "ask_read_rule"
	ActionBlockReadsOutside = "block_reads_outside"
	ActionManualDefaultMode = "manual_default_mode"
	ActionRemoveFromDisk    = "remove_from_disk"

	// ActionSandboxReadBoundary is Cursor's readBoundary "workspace" in
	// ~/.cursor/sandbox.json (capacidades de Cursor, K2).
	ActionSandboxReadBoundary = "sandbox_read_boundary_workspace"
)

// AllActions lists every action code.
var AllActions = []string{ActionAskEditRule, ActionAskReadRule, ActionBlockReadsOutside, ActionManualDefaultMode, ActionRemoveFromDisk, ActionSandboxReadBoundary, ActionRotateCredential}

// Completeness of an action.
const (
	CompletenessFull       = "full"
	CompletenessPartial    = "partial"
	CompletenessRadiusOnly = "radius_only"
)

// AllCompleteness lists every completeness value.
var AllCompleteness = []string{CompletenessFull, CompletenessPartial, CompletenessRadiusOnly}

// Action is one exact change that cuts or narrows a finding. RosmarOps never
// applies it. File is the file to change; Key the settings key, or Rules the
// rules to add to Key's list; Value the literal JSON fragment to merge (the
// whole file when it does not exist: FileExists false). remove_from_disk
// has Locations instead of a file.
type Action struct {
	ID           string   `json:"id"`
	Code         string   `json:"code"`
	File         string   `json:"file,omitempty"`
	FileExists   bool     `json:"file_exists"`
	Key          string   `json:"key,omitempty"`
	Rules        []string `json:"rules,omitempty"`
	Value        string   `json:"value,omitempty"`
	Locations    []string `json:"locations,omitempty"`
	Completeness string   `json:"completeness"`
	Magnitude    int      `json:"magnitude"`
	Cuts         []string `json:"cuts"`

	points int // reduction points: full 2, partial or radius 1 (A1: radius 2 for any_project)
	files  int // files the person changes
}

// Today is the one thing to do today: a finding and its action, or the code
// today.nothing_urgent. Remains, for a partial or radius-only action, is
// what stays possible after the change.
type Today struct {
	Code      string   `json:"code,omitempty"`
	FindingID string   `json:"finding_id,omitempty"`
	ActionID  string   `json:"action_id,omitempty"`
	Remains   *Remains `json:"remains,omitempty"`
}

// Remains is what an action leaves: the radius left, and the finding's edges
// and egress capabilities it does not cut.
type Remains struct {
	Radius string   `json:"radius,omitempty"`
	Ways   []string `json:"ways"`
}

// actionsFor builds every action of the knowledge base that applies to f,
// from smallest to largest magnitude.
func actionsFor(in Input, a *agentView, f Finding, nodes map[string]graph.Node) []Action {
	cred := f.Rule == RuleCredReadAndSend || f.Rule == RuleCredReadable
	settings := userSettings(a.node.ConfigPath)
	exists := configExists(in.Graph, settings)
	out := []Action{}
	for _, code := range AllActions {
		m, ok := a.base.Mitigation(code)
		if !ok {
			continue
		}
		act := Action{Code: code, Magnitude: m.Magnitude, File: settings, FileExists: exists, files: 1}
		// A rule is never written for a path with pattern characters: its
		// edges are not cut by the action (BV-38).
		uncovered, uncoveredFile := map[string]bool{}, false
		switch code {
		case ActionAskEditRule:
			if cred {
				continue
			}
			act.Key = m.Setting
			for _, id := range f.Evidence.Nodes {
				r := nodes[id]
				if r.Kind != graph.KindResource {
					continue
				}
				file, ok := graph.FileOfResource(r.Path)
				if !ok {
					continue
				}
				if p, ok := rulePath(file); ok {
					act.Rules = append(act.Rules, m.RuleTool+"("+untrusted.PlainText(p)+")") // B7: a path read from disk
				} else {
					uncovered[id] = true
				}
			}
			if len(act.Rules) == 0 {
				continue
			}
		case ActionAskReadRule:
			if !cred {
				continue
			}
			act.Key = m.Setting
			for _, file := range credFilesOf(f) {
				if p, ok := rulePath(file); ok {
					act.Rules = append(act.Rules, m.RuleTool+"("+untrusted.PlainText(p)+")") // B7: a path read from disk
				} else {
					uncoveredFile = true
				}
			}
			if len(act.Rules) == 0 {
				continue
			}
		case ActionBlockReadsOutside:
			if !cred || f.Radius != RadiusAnyProject {
				continue
			}
			act.Key, act.Value = m.Setting, fragment(m.Setting, json.RawMessage(m.Value))
		case ActionSandboxReadBoundary:
			// Cursor's readBoundary workspace in ~/.cursor/sandbox.json
			// (decision 2026-10-25, Cursor, K2).
			if !cred || f.Radius != RadiusAnyProject {
				continue
			}
			act.File = inConfigDir(a.node.ConfigPath, m.File)
			act.FileExists = configExists(in.Graph, act.File)
			act.Key, act.Value = m.Setting, fragment(m.Setting, json.RawMessage(m.Value))
		case ActionManualDefaultMode:
			if a.modeCap == nil || a.modeCap.Basis == graph.BasisConfigured && a.modeCap.AppliesIn != "user" {
				continue // the mode is not set by the user (managed): not the person's change
			}
			act.Key, act.Value = m.Setting, fragment(m.Setting, json.RawMessage(m.Value))
		case ActionRemoveFromDisk:
			if !cred {
				continue
			}
			act.File, act.FileExists = "", false
			act.Locations = f.Evidence.Locations
			act.files = len(credFilesOf(f))
		}
		if len(act.Rules) > 0 {
			act.Rules = sortedUnique(act.Rules)
			act.Value = fragment(m.Setting, act.Rules)
		}
		covered := func(w way) bool {
			return !uncovered[w.target] && !(uncoveredFile && w.target != "")
		}
		if !applyCuts(&act, m, f, covered) {
			continue
		}
		act.ID = actionID(act)
		out = append(out, act)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Magnitude < out[j].Magnitude })
	return out
}

// applyCuts sets the action's cuts, completeness and points from the ways of
// the finding, and reports whether the action applies at all (it cuts or
// narrows some way of the finding).
//
// covered reports whether the action reaches a way at all: a way through a
// path the action writes no rule for is narrowed at most, never cut.
func applyCuts(act *Action, m knowledge.Mitigation, f Finding, covered func(way) bool) bool {
	in := func(code string, set []string) bool {
		for _, s := range set {
			if s == code || s == "any_reach" && !strings.HasPrefix(code, "egress_") {
				return true
			}
		}
		return false
	}
	touched, fullLeg := false, false
	act.Cuts = []string{}
	for _, leg := range f.legs {
		all := len(leg) > 0
		for _, w := range leg {
			switch {
			case in(w.code, m.Cuts) && covered(w):
				act.Cuts = append(act.Cuts, w.id)
				touched = true
			case in(w.code, m.Narrows):
				touched = true
				all = false
			default:
				all = false
			}
		}
		fullLeg = fullLeg || all
	}
	act.Cuts = sortedUnique(act.Cuts)
	if !touched {
		return false
	}
	switch m.Completeness {
	case "by_cuts":
		act.Completeness = CompletenessPartial
		if fullLeg {
			act.Completeness = CompletenessFull
		}
	default:
		act.Completeness = m.Completeness
	}
	act.points = 1
	if act.Completeness == CompletenessFull || act.Completeness == CompletenessRadiusOnly && f.Radius == RadiusAnyProject {
		act.points = 2 // A1: a radius action is complete for any_project
	}
	return true
}

// credFilesOf are the distinct files of a credential finding's locations.
func credFilesOf(f Finding) []string {
	return graph.CredentialFiles(graph.Node{Locations: f.Evidence.Locations})
}

// actionID is "a_" + sha16 of the action's code and target, so one change
// shared by several findings has one id.
func actionID(a Action) string {
	parts := append([]string{a.Code, a.File, a.Key, a.Value}, a.Locations...)
	return "a_" + graph.KeyHash(parts...)
}

// fragment is the literal JSON to merge into a settings file for key
// ("permissions.<name>", or a top-level key such as Cursor's readBoundary)
// set to value.
func fragment(key string, value any) string {
	doc := map[string]any{key: value}
	if name, ok := strings.CutPrefix(key, "permissions."); ok {
		doc = map[string]any{"permissions": map[string]any{name: value}}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ""
	}
	return string(out)
}

// userSettings is the user settings file of a Claude Code configuration
// folder.
func userSettings(configDir string) string {
	sep := "/"
	if strings.Contains(configDir, `\`) {
		sep = `\`
	}
	return strings.TrimRight(configDir, `/\`) + sep + "settings.json"
}

// inConfigDir resolves a mitigation's file "~/.cursor/<name>" in the agent's
// configuration folder (~/.cursor).
func inConfigDir(configDir, file string) string {
	name := file[strings.LastIndex(file, "/")+1:]
	return userSettings(configDir)[:len(userSettings(configDir))-len("settings.json")] + name
}

// configExists reports a settings file read into the graph (an agent_config
// resource with its path).
func configExists(g graph.Graph, file string) bool {
	want := graph.CanonicalPath(file)
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentConfig && graph.CanonicalPath(n.Path) == want {
			return true
		}
	}
	return false
}

// rulePath is the absolute pattern of a file in a Read or Edit rule: "//"
// and the path in POSIX form, as Claude Code matches it on Windows
// ("C:\Users\alice" is "/c/Users/alice"; permissions, "Read and Edit").
// ok is false for a path with a gitignore pattern character (*, ?, [ or
// ]): no rule is written for it (BV-38).
func rulePath(file string) (string, bool) {
	if strings.ContainsAny(file, "*?[]") {
		return "", false
	}
	p := strings.ReplaceAll(file, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return "/" + p, true
}

// Order of findings (§2.1): severity; impact class; confidence; radius;
// group size (larger first); id. cred_in_git (memoria §2.4): a high key
// committed in a repository with a remote may already have left the
// machine, so it goes first; without a remote, after cred_read_and_send.
var impactRank = map[string]int{
	RuleCredInGit + "/" + ClassHigh + "/remote": 0,
	RuleCredReadAndSend + "/" + ClassHigh:       1,
	RuleCredInGit + "/" + ClassHigh:             2,
	RuleForeignAgentConfigWritable:              3,
	RuleOwnConfigWritable:                       4,
	RuleCredReadable + "/" + ClassHigh:          5,
	RuleForeignAgentInstructionsWritable:        6,
	RuleOwnInstructionsWritable:                 7,
	// Not ranked in §2.1: model API keys and unclassified credentials
	// that can be sent out or are committed come last.
	RuleCredReadAndSend + "/" + ClassMedium:       8,
	RuleCredReadAndSend + "/" + ClassUnclassified: 8,
	RuleCredInGit + "/" + ClassMedium:             8,
	RuleCredInGit + "/" + ClassUnclassified:       8,
}

func rankOf(f Finding) int {
	if f.Rule == RuleCredInGit && f.Params.HasRemote != nil && *f.Params.HasRemote {
		if r, ok := impactRank[f.Rule+"/"+f.class+"/remote"]; ok {
			return r
		}
	}
	if r, ok := impactRank[f.Rule+"/"+f.class]; ok {
		return r
	}
	if r, ok := impactRank[f.Rule]; ok {
		return r
	}
	return len(impactRank)
}

func sortFindings(fs []Finding) {
	sev := map[string]int{SeverityRed: 0, SeverityYellow: 1}
	conf := map[string]int{ConfidenceHigh: 0, ConfidenceMedium: 1}
	rad := map[string]int{RadiusAnyProject: 0, RadiusSameProject: 1, "": 2}
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		switch {
		case sev[a.Severity] != sev[b.Severity]:
			return sev[a.Severity] < sev[b.Severity]
		case rankOf(a) != rankOf(b):
			return rankOf(a) < rankOf(b)
		case conf[a.Confidence] != conf[b.Confidence]:
			return conf[a.Confidence] < conf[b.Confidence]
		case rad[a.Radius] != rad[b.Radius]:
			return rad[a.Radius] < rad[b.Radius]
		case a.Group.Count != b.Group.Count:
			return a.Group.Count > b.Group.Count
		}
		return a.ID < b.ID
	})
}

// pickToday is the first finding of the ordered list and, among its
// actions, the largest reduction per magnitude (points / magnitude). Ties:
// the action that touches more findings of the list, then the one that
// changes a single file, then the larger reduction, then the action id.
func pickToday(fs []Finding) Today {
	if len(fs) == 0 {
		return Today{Code: TodayNothingUrgent}
	}
	f := fs[0]
	if len(f.Actions) == 0 {
		return Today{FindingID: f.ID}
	}
	best := bestAction(f, fs)
	t := Today{FindingID: f.ID, ActionID: best.ID}
	if best.Completeness != CompletenessFull {
		t.Remains = remains(f, best)
	}
	return t
}

// BestAction is a finding's main action: the one the algorithm of §2 picks
// among its actions, with fs the whole list of findings (for the ties). ok
// is false for a finding with no action.
func BestAction(f Finding, fs []Finding) (Action, bool) {
	if len(f.Actions) == 0 {
		return Action{}, false
	}
	return bestAction(f, fs), true
}

func bestAction(f Finding, fs []Finding) Action {
	touches := map[string]int{}
	for _, g := range fs {
		for _, a := range g.Actions {
			touches[a.ID]++
		}
	}
	best := f.Actions[0]
	for _, a := range f.Actions[1:] {
		if better(a, best, touches) {
			best = a
		}
	}
	return best
}

// better reports whether a beats b as today's action.
func better(a, b Action, touches map[string]int) bool {
	if l, r := a.points*b.Magnitude, b.points*a.Magnitude; l != r {
		return l > r
	}
	if touches[a.ID] != touches[b.ID] {
		return touches[a.ID] > touches[b.ID]
	}
	if (a.files == 1) != (b.files == 1) {
		return a.files == 1
	}
	if a.points != b.points {
		return a.points > b.points
	}
	return a.ID < b.ID
}

// remains is what stays possible after a partial or radius-only action: for
// a radius action, the radius left (same_project); and the ways it does not
// cut.
func remains(f Finding, a Action) *Remains {
	cut := map[string]bool{}
	for _, id := range a.Cuts {
		cut[id] = true
	}
	r := &Remains{Ways: []string{}}
	if a.Completeness == CompletenessRadiusOnly {
		r.Radius = RadiusSameProject
	}
	for _, leg := range f.legs {
		for _, w := range leg {
			if !cut[w.id] {
				r.Ways = append(r.Ways, w.id)
			}
		}
	}
	r.Ways = sortedUnique(r.Ways)
	return r
}
