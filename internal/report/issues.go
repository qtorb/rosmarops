package report

import (
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The report's first screen lists problems, not findings: the findings that
// one change fixes are one problem. The grouping happens here, while the
// report is drawn, from data report.v2 and graph.v2 already carry; nothing
// new is written to any JSON, and the findings, their codes and their count
// do not change (usage statistics and the company summary count findings).
//
// Two rules group findings; every other finding is a problem of its own:
//   - credentials: the credential findings (read and send, readable, in git)
//     whose credentials open the same service, by project when the
//     credential names one;
//   - another agent's settings: the findings of one agent that can change
//     other agents' configuration or instructions.

// credService is the service a credential authenticates to, and its
// project's path when the credential names it ("" otherwise).
type credService struct {
	service string
	project string
}

// credServicesOf reads, from the graph, the service of every credential:
// the credential node's service, and the project path of the external
// service its authenticates_to edge points to, when that path is a
// project's and not the bare service://<service>.
func credServicesOf(g *graph.Graph) map[string]credService {
	out := map[string]credService{}
	if g == nil {
		return out
	}
	path := map[string]string{}
	for _, n := range g.Nodes {
		switch n.Kind {
		case graph.KindCredential:
			cs := out[n.ID]
			cs.service = n.Service
			out[n.ID] = cs
		case graph.KindResource:
			if n.Type == graph.ResourceExternalService {
				path[n.ID] = n.Path
			}
		}
	}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeAuthenticatesTo {
			continue
		}
		if p := path[e.To]; p != "" && !strings.HasPrefix(p, "service://") {
			cs := out[e.From]
			cs.project = p
			out[e.From] = cs
		}
	}
	return out
}

// The kinds of problem.
const (
	issueCred   = "cred"   // a service's credentials, within reach of agents
	issueConfig = "config" // an agent that can change other agents' settings
	issueOne    = "one"    // a single finding, with its own text
	// issueConfigMany: two or more agents that can change other agents'
	// settings, one row with a part per agent (each an issueConfig).
	issueConfigMany = "config_many"
)

// issue is one problem of the first screen: the findings it joins, in the
// report's order.
type issue struct {
	kind     string
	key      string
	findings []rules.Finding

	service string  // issueCred: the service's id
	project bool    // issueCred: the credentials name one project
	agentID string  // issueConfig: the agent that can change the others
	parts   []issue // issueConfigMany: one issueConfig per agent, in the table's order
}

// red reports an urgent problem: any of its findings is red.
func (is issue) red() bool {
	for _, f := range is.findings {
		if f.Severity == rules.SeverityRed {
			return true
		}
	}
	return false
}

// ids are the problem's finding ids.
func (is issue) ids() map[string]bool {
	out := map[string]bool{}
	for _, f := range is.findings {
		out[f.ID] = true
	}
	return out
}

// readers are the distinct agents (by id) of the problem's reading findings,
// in order of first appearance; cred_in_git has no agent.
func (is issue) readers() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range is.findings {
		if f.Rule == rules.RuleCredInGit || len(f.Evidence.Nodes) == 0 {
			continue
		}
		if a := f.Evidence.Nodes[0]; !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// inGit are the problem's cred_in_git findings.
func (is issue) inGit() []rules.Finding {
	var out []rules.Finding
	for _, f := range is.findings {
		if f.Rule == rules.RuleCredInGit {
			out = append(out, f)
		}
	}
	return out
}

// targets are the distinct agents (by id) an issueConfig problem's agent can
// change, in order of first appearance.
func (is issue) targets() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range is.findings {
		if len(f.Evidence.Nodes) < 2 {
			continue
		}
		if t := f.Evidence.Nodes[1]; !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func isCredRule(r string) bool {
	return r == rules.RuleCredReadAndSend || r == rules.RuleCredReadable || r == rules.RuleCredInGit
}

func isForeignRule(r string) bool {
	return r == rules.RuleForeignAgentConfigWritable || r == rules.RuleForeignAgentInstructionsWritable
}

// credsOf are a credential finding's credential nodes: those of its
// evidence, and cred_in_git's own credential.
func credsOf(f rules.Finding) []string {
	var out []string
	if f.Rule == rules.RuleCredInGit && f.Params.CredID != "" {
		out = append(out, f.Params.CredID)
	}
	for _, n := range f.Evidence.Nodes {
		if strings.HasPrefix(n, "cred_") && !strings.Contains(strings.Join(out, " "), n) {
			out = append(out, n)
		}
	}
	return out
}

// issuesOf groups the findings (already in the report's order) into problems
// and sorts them: urgent first, then more findings first, then the report's
// order. A credential problem needs a known service and at least one agent
// that can read it; a service with several projects keeps the credentials
// that name none apart, one finding each, so projects never mix. A problem
// the signed texts cannot word (a key of an unknown service, a key no agent
// reads, a project not identified among several) stays one finding each,
// with its own text.
func issuesOf(fs []rules.Finding, svc map[string]credService) []issue {
	var out []issue
	at := map[string]int{}
	add := func(key string, mk func() issue, f rules.Finding) {
		if i, ok := at[key]; ok {
			out[i].findings = append(out[i].findings, f)
			return
		}
		at[key] = len(out)
		is := mk()
		is.key = key
		is.findings = []rules.Finding{f}
		out = append(out, is)
	}
	// Each service's projects, to tell whether a credential that names none
	// can join the only one.
	projects := map[string]map[string]bool{}
	for _, f := range fs {
		if !isCredRule(f.Rule) {
			continue
		}
		for _, c := range credsOf(f) {
			if s := svc[c]; s.project != "" {
				if projects[s.service] == nil {
					projects[s.service] = map[string]bool{}
				}
				projects[s.service][s.project] = true
			}
		}
	}
	for _, f := range fs {
		switch {
		case isCredRule(f.Rule):
			service, project := "", ""
			for _, c := range credsOf(f) {
				s := svc[c]
				if s.service == "" || s.service == graph.ServiceUnknown {
					continue
				}
				if service == "" || (project == "" && s.project != "") {
					service, project = s.service, s.project
				}
			}
			if service != "" && project == "" && len(projects[service]) == 1 {
				for p := range projects[service] {
					project = p
				}
			}
			if service == "" || (project == "" && len(projects[service]) > 1) {
				add("one:"+f.ID, func() issue { return issue{kind: issueOne} }, f)
				continue
			}
			key := "svc:" + service
			if project != "" {
				key = "proj:" + project
			}
			add(key, func() issue { return issue{kind: issueCred, service: service, project: project != ""} }, f)
		case isForeignRule(f.Rule) && len(f.Evidence.Nodes) > 0:
			a := f.Evidence.Nodes[0]
			add("cfg:"+a, func() issue { return issue{kind: issueConfig, agentID: a} }, f)
		default:
			add("one:"+f.ID, func() issue { return issue{kind: issueOne} }, f)
		}
	}
	// A credential problem no agent can read has no signed text: its findings
	// stand alone.
	var final []issue
	for _, is := range out {
		if is.kind == issueCred && len(is.readers()) == 0 {
			for _, f := range is.findings {
				final = append(final, issue{kind: issueOne, key: "one:" + f.ID, findings: []rules.Finding{f}})
			}
			continue
		}
		final = append(final, is)
	}
	sortIssues(final)
	return mergeConfigIssues(final)
}

// sortIssues orders the problems: urgent first, then the ones with more
// findings.
func sortIssues(iss []issue) {
	sort.SliceStable(iss, func(i, j int) bool {
		if iss[i].red() != iss[j].red() {
			return iss[i].red()
		}
		return len(iss[i].findings) > len(iss[j].findings)
	})
}

// mergeConfigIssues joins two or more problems of agents that can change
// other agents' settings into one row: its findings are theirs, its parts
// keep each agent's problem in the order they had, and the row takes its
// place among the others by the same order. One such problem stays as it is.
func mergeConfigIssues(iss []issue) []issue {
	var parts, rest []issue
	for _, is := range iss {
		if is.kind == issueConfig {
			parts = append(parts, is)
		} else {
			rest = append(rest, is)
		}
	}
	if len(parts) < 2 {
		return iss
	}
	many := issue{kind: issueConfigMany, key: "cfg_many", parts: parts}
	for _, p := range parts {
		many.findings = append(many.findings, p.findings...)
	}
	out := append(rest, many)
	sortIssues(out)
	return out
}

// issueGain is a problem's «if you fix this»: the shown score now and with
// every finding of the problem fixed at once (index.WithoutSet). The shown
// score is the computer's; when the computer has none, the agent's that the
// first screen shows (agentID is then that agent, "" for the computer). ok
// is false without a score or when the problem does not move it.
func issueGain(v V2, shown index.MachineScore, is issue) (now, then int, agentID string, ok bool) {
	if v.indexIn == nil || shown.Value == nil {
		return 0, 0, "", false
	}
	agents, m := index.WithoutSet(*v.indexIn, is.ids())
	if v.Index.Machine.Value == nil {
		a, has := agents[shown.AgentID]
		if !has || a <= *shown.Value {
			return 0, 0, "", false
		}
		return *shown.Value, a, shown.AgentID, true
	}
	if m == nil || *m <= *shown.Value {
		return 0, 0, "", false
	}
	return *shown.Value, *m, "", true
}
