package graph

import (
	"fmt"
	"sort"
	"strings"
)

// probeName is the name of every synthetic target of Probe: no protected or
// ignored path of the knowledge bases matches it (a test checks it). It is
// never created on disk and never written to the graph, a report or a log.
const probeName = "rosmarops-probe"

// ProbeTarget is one synthetic target of Probe (decision 2026-10-25, cómo
// trabajan tus agentes, cálculo, annex 2, point 1).
type ProbeTarget struct {
	Path    string // synthetic path; never shown
	Outside bool   // outside every effective root
	Edit    string // the Edit allow capability whose pattern it follows, if any
}

// ProbeHit is what the inference finds for one target: the edges it would
// create, and the capabilities in force whose ask (configured or product
// default) or deny covers the target, for the cells with no way (annex 2,
// point 4).
type ProbeHit struct {
	Target        ProbeTarget
	Edges         []Edge
	AskBy, DenyBy []string
}

// AgentProbe is Probe's answer for one evaluated client: the read and write
// hits, and whether a root contains home, so that there is no target outside
// the roots and any reach counts as machine (annex 2, point 1).
type AgentProbe struct {
	Read, Write []ProbeHit
	NoOutside   bool
}

// Probe passes each evaluated client's inference (Claude Code and Cursor)
// over synthetic targets and returns the edges it would have created,
// without adding them to g (annex 2, point 1). The targets, per client: one
// inside each effective root (a synthetic root when there is none); one
// under home outside every effective root (additionalDirectories
// included), unless a root contains home; and, for Claude Code, one for each
// Edit allow in force, at a path its pattern covers. It is a pure function:
// nothing is read from or written to disk.
func Probe(g Graph, repoRoots []string, home string) (map[string]AgentProbe, error) {
	p, err := prepare(g)
	if err != nil {
		return nil, err
	}
	roots := repoRoots
	if len(roots) == 0 {
		// No configured root: a synthetic one, for the probe only.
		roots = []string{joinLike(home, probeName+"-root")}
	}
	out := map[string]AgentProbe{}
	for _, client := range g.Nodes {
		if client.Kind != KindAgent || client.Type != AgentAIClient {
			continue
		}
		switch client.Platform {
		case p.claude.Product:
			in := p.claudeInference(client, roots, home)
			targets, noOutside := probeTargets(in.roots, home)
			for _, c := range in.inForce {
				if t, ok := in.editTarget(c); ok {
					targets = append(targets, t)
				}
			}
			ap := AgentProbe{NoOutside: noOutside}
			for i, t := range targets {
				id := probeID("r", i)
				if t.Edit == "" {
					hit := ProbeHit{Target: t}
					for _, w := range in.readWays([]string{t.Path}) {
						hit.Edges = append(hit.Edges, in.edge(EdgeReaches, id, w.v, w.assumptions, w.rules))
					}
					hit.AskBy, hit.DenyBy = in.covered(t, CapReadFiles)
					ap.Read = append(ap.Read, hit)
				}
				hit := ProbeHit{Target: t}
				for _, w := range in.writeWays(t.Path) {
					hit.Edges = append(hit.Edges, in.edge(EdgeWritesConfigOf, probeID("w", i), w.v, w.assumptions, w.rules))
				}
				hit.AskBy, hit.DenyBy = in.covered(t, CapWriteFiles)
				ap.Write = append(ap.Write, hit)
			}
			out[client.ID] = ap
		case p.cursor.Product:
			in := p.cursorInference(client, roots)
			targets, noOutside := probeTargets(in.roots, home)
			ap := AgentProbe{NoOutside: noOutside}
			for i, t := range targets {
				rh := ProbeHit{Target: t}
				for _, w := range in.readWays([]string{t.Path}) {
					rh.Edges = append(rh.Edges, in.edge(EdgeReaches, probeID("r", i), w.cap, w.assumptions))
				}
				rh.AskBy = cursorCovered(in)
				wh := ProbeHit{Target: t}
				for _, w := range in.writeWays(t.Path, "") {
					wh.Edges = append(wh.Edges, in.edge(EdgeWritesConfigOf, probeID("w", i), w.cap, w.assumptions))
				}
				wh.AskBy = cursorCovered(in)
				ap.Read, ap.Write = append(ap.Read, rh), append(ap.Write, wh)
			}
			out[client.ID] = ap
		}
	}
	return out, nil
}

func probeID(kind string, i int) string { return fmt.Sprintf("probe_%s_%03d", kind, i) }

// probeTargets are the targets inside each root and the one outside them all,
// under home. noOutside: a root contains home, so nothing under home is
// outside the roots.
func probeTargets(roots []string, home string) ([]ProbeTarget, bool) {
	var out []ProbeTarget
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[normPath(r)] {
			continue
		}
		seen[normPath(r)] = true
		out = append(out, ProbeTarget{Path: joinLike(r, probeName)})
	}
	outside := joinLike(home, probeName+"-outside")
	for _, r := range roots {
		if within(r, home) || within(r, outside) {
			return out, true
		}
	}
	return append(out, ProbeTarget{Path: outside, Outside: true}), false
}

// joinLike joins name to dir with dir's separator.
func joinLike(dir, name string) string {
	sep := "/"
	if strings.Contains(dir, `\`) {
		sep = `\`
	}
	return strings.TrimRight(dir, `/\`) + sep + name
}

// editTarget is a concrete path the pattern of an Edit allow in force covers
// (annex 2, point 1), checked with the same matchRulePath the inference uses:
// "**" stands for no folder (one name at the end), "*" and "?" become
// letters of the probe name. No target when the pattern cannot be resolved
// to a path it covers.
func (in inference) editTarget(c Node) (ProbeTarget, bool) {
	if c.Type != CapWriteFiles || c.Approval != ApprovalAllow || c.Basis != BasisConfigured || c.TargetScope == scopeAll || c.TargetScope == "" {
		return ProbeTarget{}, false
	}
	s := in.scopeOf(c)
	pat := c.TargetScope
	var anchors []string
	switch {
	case strings.HasPrefix(pat, "//"):
		anchors, pat = []string{"/"}, pat[2:]
	case strings.HasPrefix(pat, "~/"):
		anchors, pat = []string{in.home}, pat[2:]
	case strings.HasPrefix(pat, "/"):
		pat = pat[1:]
		if root, ok := s.scopeRoot(); ok {
			anchors = []string{root}
		} else if s.appliesIn == "user" {
			anchors = []string{s.configDir}
		}
	default:
		pat = strings.TrimPrefix(pat, "./")
		if root, ok := s.scopeRoot(); ok {
			anchors = []string{root}
		} else {
			anchors = in.roots
		}
	}
	var segs []string
	for _, seg := range strings.Split(strings.TrimSuffix(pat, "/"), "/") {
		switch {
		case seg == "**":
			continue
		case strings.ContainsAny(seg, "["):
			return ProbeTarget{}, false
		}
		seg = strings.ReplaceAll(seg, "*", probeName)
		segs = append(segs, strings.ReplaceAll(seg, "?", "x"))
	}
	if strings.HasSuffix(pat, "/") || strings.HasSuffix(pat, "**") || len(segs) == 0 {
		segs = append(segs, probeName)
	}
	for _, a := range anchors {
		if a == "" {
			continue
		}
		file := a
		for _, seg := range segs {
			file = joinLike(file, seg)
		}
		if matchRulePath(c.TargetScope, c.Approval, s, file) {
			return ProbeTarget{Path: file, Outside: !in.withinRoots(file), Edit: c.ID}, true
		}
	}
	return ProbeTarget{}, false
}

// covered returns the capabilities in force whose ask, and whose deny, cover
// t for Claude Code's reads (typ read_files) or writes (write_files): a rule
// of that type whose target covers t (product defaults "*",
// working_directories and outside_working_directories by where t is;
// configured ones by their pattern), or, for the ask, the shell's ask
// without specifier. A Read deny also covers a write, as E(f) (annex 2,
// point 4).
func (in inference) covered(t ProbeTarget, typ string) (ask, deny []string) {
	for _, c := range in.inForce {
		match := func() bool {
			switch c.TargetScope {
			case scopeAll:
				return true
			case "working_directories":
				return !t.Outside
			case "outside_working_directories":
				return t.Outside
			}
			return c.Basis == BasisConfigured && matchRulePath(c.TargetScope, c.Approval, in.scopeOf(c), t.Path)
		}
		switch {
		case c.Type == typ && c.Approval == ApprovalAsk && match(),
			c.Type == CapExecuteShell && c.Approval == ApprovalAsk && c.TargetScope == scopeAll:
			ask = append(ask, c.ID)
		case c.Type == typ && c.Approval == ApprovalDeny && c.Basis == BasisConfigured && match(),
			typ == CapWriteFiles && c.Type == CapReadFiles && c.Approval == ApprovalDeny && c.Basis == BasisConfigured && isRule(c.Rule, "Read") && match():
			deny = append(deny, c.ID)
		}
	}
	return ask, deny
}

// cursorCovered returns the asks in force that cover a Cursor target: the
// terminal's ask without specifier (terminal_default, or configured) covers
// any file. No deny of Cursor's covers a probe name: its protected and
// ignored paths do not match it.
func cursorCovered(in cursorInference) []string {
	var ask []string
	for _, c := range in.inForce {
		if c.Type == CapExecuteShell && c.Approval == ApprovalAsk && c.TargetScope == scopeAll {
			ask = append(ask, c.ID)
		}
	}
	return ask
}

// ProbeNames are the probe's file names, for the test that no protected or
// ignored path of the knowledge bases matches them.
func ProbeNames() []string {
	names := []string{probeName, probeName + "-outside", probeName + "-root"}
	sort.Strings(names)
	return names
}
