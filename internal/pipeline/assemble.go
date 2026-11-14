// Package pipeline assembles the v2 capability graph the way `rosmarops run`
// does.
//
// E3 (CTO-24 engineering standard): the graph assembly — the agent readers,
// Build, Infer, the Cross* steps, the Add* steps and Encode — lives in one
// exported function. The real run, the full-machine test and the factory-note
// test all call it, so no test builds the graph on a path of its own. BV-201
// and BV-202 were the same class of failure: a bug that `rosmarops run` hit on
// a real machine slipped past tests that assembled the graph by hand.
package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Input is every scan and context value the graph assembly reads. The zero
// value of any agent scan leaves that agent absent from the graph, so a
// single-agent build (a factory-note case) populates only its own fields.
type Input struct {
	// Agent readers, in the shapes the collectors produce.
	MCP          platform.MCPScan
	Settings     platform.ClaudeSettingsScan
	Cursor       platform.CursorScan
	Hermes       platform.HermesScan
	Codex        platform.CodexScan
	Grok         platform.GrokScan
	Copilot      platform.CopilotScan
	Antigravity  platform.AntigravityScan
	Gemini       platform.GeminiScan
	Cline        platform.ClineScan
	Instructions platform.InstructionScan
	Skills       platform.SkillScan
	CredFiles    platform.CredentialFileScan
	Extensions   []platform.VSCodeExtension
	Repos        graph.Repos

	// Context.
	Provenance graph.Provenance
	Salt       []byte
	RepoRoots  []string
	Home       string
	Now        time.Time

	// Crosses.
	Matcher    *intel.Matcher
	PkgObs     []graph.PackageObservation
	N8NFolder  string
	N8NKeyData bool
}

// Result is the assembled graph and its validated encoding.
type Result struct {
	Graph   graph.Graph
	Encoded []byte
	Stats   graph.PermissionStats
}

// Assemble builds the v2 graph from in and is the one assembly `rosmarops run`
// uses (E3). In order: the agent readers and merges, Build, the
// untrusted-root marks, Infer, the threat-intel / MCP-catalog / n8n crosses,
// the n8n key node, and Encode (schema and integrity validated). A graph that
// fails validation is never returned: Encode's error is returned instead.
func Assemble(in Input) (Result, error) {
	frag, stats, err := fragment(in)
	if err != nil {
		return Result{}, err
	}
	g, err := BuildGraph(frag, GraphOpts{
		Provenance: in.Provenance,
		RepoRoots:  in.RepoRoots,
		Home:       in.Home,
		Now:        in.Now,
		Codex:      in.Codex,
		Gemini:     in.Gemini,
		// BV-232 §4: the Claude Code extension for VS Code.
		ClaudeVSCodeExtension: ClaudeCodeExtensionPresent(in.Extensions),
		IntelServers:          in.MCP.Servers,
		IntelPkgs:             in.PkgObs,
		IntelMatcher:          in.Matcher,
	})
	if err != nil {
		return Result{}, err
	}
	// MCP catalog cross (BV-117/BV-121): mark mcp_server nodes of known servers
	// that act outside the machine. In-memory only (ExternalAction).
	catalog, err := knowledge.LoadMCPCatalog()
	if err != nil {
		return Result{}, fmt.Errorf("mcp catalog: %w", err)
	}
	graph.CrossMCPCatalog(&g, in.MCP.Servers, catalog, in.Now)
	// n8n cross (BV-175): mark mcp_server nodes that are n8n instances; when the
	// folder keeps its credential key beside the database, append the folder
	// resource node the n8n_key_with_data finding stands on.
	graph.CrossN8N(&g, in.MCP.Servers, in.Now)
	if in.N8NKeyData {
		graph.AddN8NKeyNode(&g, in.N8NFolder, in.Now)
	}
	out, err := graph.Encode(g)
	if err != nil {
		return Result{}, err
	}
	return Result{Graph: g, Encoded: out, Stats: stats}, nil
}

// fragment runs the agent readers and merges in the order `rosmarops run` uses.
func fragment(in Input) (graph.Fragment, graph.PermissionStats, error) {
	frag, stats, err := graph.FromScansWithGrok(in.MCP, in.Settings, in.Cursor, in.CredFiles, in.Repos, in.Hermes, in.Codex, in.Grok, in.Salt, in.Now)
	if err != nil {
		return graph.Fragment{}, graph.PermissionStats{}, err
	}
	// Every ai_client that comes in through its own reader is merged BEFORE
	// AddInstructions and AddSkills, because those attach their shared files
	// (AGENTS.md, a skills folder) only to ai_clients already in the fragment.
	// Merging after them was a latent bug: GitHub Copilot, Antigravity CLI,
	// Gemini CLI and Cline never got their AGENTS.md or skills on a real machine
	// (BV-203; surfaced by the single assembly, cf. BV-201/BV-202).
	//
	// GitHub Copilot's ai_client (BV-140, CTO-16 Copilot). The VS Code
	// extension, added below via AddExtensions, is the other presence signal.
	frag = graph.Merge(frag, graph.FromCopilot(in.Copilot, CopilotExtensionPresent(in.Extensions), in.Now))
	// Google Antigravity CLI (BV-140, CTO-16 Antigravity).
	frag = graph.Merge(frag, graph.FromAntigravityCLI(in.Antigravity, in.Now))
	// Google Gemini CLI (BV-193, CTO-22 Gemini).
	frag = graph.Merge(frag, graph.FromGeminiCLI(in.Gemini, in.Now))
	// The Cline VS Code extension (BV-193, CTO-22 Cline).
	frag = graph.Merge(frag, graph.FromCline(in.Cline, ClineExtensionPresent(in.Extensions), in.Now))
	// Shared instruction and skills files, attached to every ai_client now
	// present. Extensions add their own agent nodes and are order-independent.
	frag = graph.AddInstructions(frag, in.Instructions, in.Now)
	frag = graph.AddSkills(frag, in.Skills, in.Now)
	frag = graph.AddExtensions(frag, in.Extensions, filepath.Join(in.Home, ".vscode", "extensions"), in.Now)
	return frag, stats, nil
}

// GraphOpts are the finishing steps BuildGraph applies to a fragment: Build,
// the untrusted-root marks of Codex and Gemini, Infer, and the threat-intel
// cross. Assemble uses it, and so does any test that constructs a fragment by
// hand, so Build, Infer and Cross* run in one place and never on a path of
// their own (E3).
type GraphOpts struct {
	Provenance graph.Provenance
	RepoRoots  []string
	Home       string
	Now        time.Time

	// When the scan's agent is present, its untrusted repo roots are marked on
	// the built graph before inference. An absent scan is a no-op.
	Codex  platform.CodexScan
	Gemini platform.GeminiScan

	// ClaudeVSCodeExtension: the Claude Code extension for VS Code is
	// installed, so each project takes the less restrictive of its own mode
	// and the user's (BV-232 §4, graph.MarkClaudeVSCodeModes).
	ClaudeVSCodeExtension bool

	// SkipInfer returns the built-and-marked graph without inferring reaches and
	// writes_config_of edges. For callers that assert on the built graph alone.
	SkipInfer bool

	// Threat-intel cross (BV-100, F0a): applied when a matcher, servers or
	// package observations are given; a nil matcher makes CrossIntel a no-op.
	IntelServers []platform.MCPServer
	IntelPkgs    []graph.PackageObservation
	IntelMatcher *intel.Matcher
}

// BuildGraph builds frag into a graph, marks the untrusted roots, infers the
// permission edges (unless SkipInfer) and applies the threat-intel cross.
func BuildGraph(frag graph.Fragment, o GraphOpts) (graph.Graph, error) {
	g := graph.Build(frag, o.Provenance, o.Now)
	graph.MarkCodexUntrusted(&g, o.Codex)
	graph.MarkGeminiUntrusted(&g, o.Gemini, o.RepoRoots)
	graph.MarkClaudeVSCodeModes(&g, o.ClaudeVSCodeExtension)
	if !o.SkipInfer {
		var err error
		g, err = graph.Infer(g, o.RepoRoots, o.Home)
		if err != nil {
			return graph.Graph{}, err
		}
	}
	if o.IntelMatcher != nil || len(o.IntelServers) > 0 || len(o.IntelPkgs) > 0 {
		graph.CrossIntel(&g, o.IntelServers, o.IntelPkgs, o.IntelMatcher, o.Now)
	}
	return g, nil
}

// CopilotExtensionPresent reports the GitHub Copilot VS Code extension among
// the installed extensions (BV-140): a presence signal for the Copilot
// ai_client even when no chat.* configuration is set.
func CopilotExtensionPresent(exts []platform.VSCodeExtension) bool {
	for _, e := range exts {
		switch strings.ToLower(e.ID) {
		case "github.copilot", "github.copilot-chat":
			return true
		}
	}
	return false
}

// ClaudeCodeExtensionPresent reports the Claude Code extension for VS Code
// (anthropic.claude-code) among the installed extensions (BV-232 §4): its
// sessions do not read a project's starting mode.
func ClaudeCodeExtensionPresent(exts []platform.VSCodeExtension) bool {
	for _, e := range exts {
		if strings.EqualFold(e.ID, "anthropic.claude-code") {
			return true
		}
	}
	return false
}

// ClineExtensionPresent reports the Cline VS Code extension (BV-193) among the
// listed VS Code extensions, a presence signal for its ai_client.
func ClineExtensionPresent(exts []platform.VSCodeExtension) bool {
	for _, e := range exts {
		if strings.EqualFold(e.ID, "saoudrizwan.claude-dev") {
			return true
		}
	}
	return false
}
