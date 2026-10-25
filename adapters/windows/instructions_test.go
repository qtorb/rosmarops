package windows_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// instructionsGraph builds and encodes the graph of the MCP, settings and
// instruction scans for repoRoots.
func instructionsGraph(t *testing.T, w *windows.WindowsPlatform, repoRoots []string) (graph.Graph, []byte) {
	t.Helper()
	ctx := context.Background()
	mcp, err := w.ListMCPServers(ctx, repoRoots)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := w.ListClaudeSettings(ctx, repoRoots)
	if err != nil {
		t.Fatal(err)
	}
	ins, err := w.ListAgentInstructions(ctx, repoRoots)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := graph.FromScan(mcp, settings, testSalt, testTime)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, ins, testTime)
	g := graph.Build(f, graph.Provenance{EngineVersion: "x", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime)
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return g, out
}

// instructionResources maps each agent_instructions path (canonical) to the
// platform of the agent it configures.
func instructionResources(g graph.Graph) map[string]string {
	agents := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent {
			agents[n.ID] = n.Platform
		}
	}
	out := map[string]string{}
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentInstructions {
			out[canon(n.Path)] = agents[n.Configures]
		}
	}
	return out
}

const fakeToken = "ghp_INSTRUCTIONSINSTRUCTIONSINSTRUCTIO"

func TestListAgentInstructions_ExistingFilesAreResources(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, false) // Claude Code and Cursor clients exist
	managed := t.TempDir()
	cc := filepath.Join(e.home, ".claude")
	files := map[string]string{
		filepath.Join(cc, "CLAUDE.md"):                                     "claude-code",
		filepath.Join(cc, "rules", "style.md"):                             "claude-code",
		filepath.Join(managed, "CLAUDE.md"):                                "claude-code",
		filepath.Join(e.repoA, "CLAUDE.md"):                                "claude-code",
		filepath.Join(e.repoA, ".claude", "CLAUDE.md"):                     "claude-code",
		filepath.Join(e.repoA, "CLAUDE.local.md"):                          "claude-code",
		filepath.Join(e.repoA, ".claude", "rules", "testing.md"):           "claude-code",
		filepath.Join(e.repoA, ".claude", "rules", "frontend", "react.MD"): "claude-code",
		filepath.Join(e.repoA, ".cursorrules"):                             "cursor",
		filepath.Join(e.repoA, ".cursor", "rules", "x.mdc"):                "cursor",
		filepath.Join(e.repoA, ".cursor", "rules", "api", "RULE.md"):       "cursor",
	}
	for p := range files {
		writeFile(t, p, "# instructions with "+fakeToken+"\n")
	}
	// Not instruction files, or excluded: never resources.
	for _, p := range []string{
		filepath.Join(e.repoA, ".claude", "rules", "notes.txt"),
		filepath.Join(e.repoA, ".claude", "rules", "node_modules", "dep.md"),
		filepath.Join(e.repoA, ".cursor", "rules", ".git", "x.mdc"),
		filepath.Join(e.repoA, "sub", "CLAUDE.md"),
	} {
		writeFile(t, p, "x")
	}
	if err := os.MkdirAll(filepath.Join(e.repoB, "CLAUDE.md"), 0o700); err != nil { // a folder, not a file
		t.Fatal(err)
	}

	w := windows.NewWithManaged(managed, "", fakeRegistry{})
	g, out := instructionsGraph(t, w, []string{e.repoA, e.repoB})

	want := map[string]string{}
	for p, product := range files {
		want[canon(p)] = product
	}
	if got := instructionResources(g); !reflect.DeepEqual(got, want) {
		var gk, wk []string
		for k := range got {
			gk = append(gk, k)
		}
		for k := range want {
			wk = append(wk, k)
		}
		sort.Strings(gk)
		sort.Strings(wk)
		t.Errorf("agent_instructions:\n got %v\nwant %v", gk, wk)
	}
	// Content is never read: the token in every file is not in the graph.
	if strings.Contains(string(out), fakeToken) || strings.Contains(string(out), "INSTRUCTIONS") {
		t.Error("instruction file content in the graph")
	}
}

func TestListAgentInstructions_NoFilesNoResources(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, false)
	g, _ := instructionsGraph(t, windows.NewWithManaged(t.TempDir(), "", fakeRegistry{}), []string{e.repoA, e.repoB})
	if got := instructionResources(g); len(got) != 0 {
		t.Errorf("resources without files: %v", got)
	}
}

// Without a Cursor client (no ~/.cursor/mcp.json), Cursor's rule files are
// not resources; Claude Code's still are.
func TestListAgentInstructions_CursorAbsent(t *testing.T) {
	e := newIdentityEnv(t)
	writeFile(t, filepath.Join(e.home, ".claude.json"), `{"mcpServers": {}}`)
	writeFile(t, filepath.Join(e.repoA, ".cursorrules"), "x")
	writeFile(t, filepath.Join(e.repoA, ".cursor", "rules", "x.mdc"), "x")
	writeFile(t, filepath.Join(e.repoA, "CLAUDE.md"), "x")

	g, _ := instructionsGraph(t, windows.New(), []string{e.repoA})
	want := map[string]string{canon(filepath.Join(e.repoA, "CLAUDE.md")): "claude-code"}
	if got := instructionResources(g); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestListAgentInstructions_OversizeToCoverage(t *testing.T) {
	e := newIdentityEnv(t)
	writeFile(t, filepath.Join(e.home, ".claude.json"), `{"mcpServers": {}}`)
	big := filepath.Join(e.repoA, "CLAUDE.md")
	writeFile(t, big, strings.Repeat("x", windows.MaxConfigBytes+1))

	ins, err := windows.New().ListAgentInstructions(context.Background(), []string{e.repoA})
	if err != nil {
		t.Fatal(err)
	}
	if len(ins.Files) != 0 || len(ins.Failed) != 1 || ins.Failed[0].Path != big ||
		!strings.HasPrefix(ins.Failed[0].Reason, graph.ReasonSizeLimitExceeded+": ") {
		t.Fatalf("scan: %+v", ins)
	}
	g, _ := instructionsGraph(t, windows.New(), []string{e.repoA})
	var found bool
	for _, sf := range g.Coverage.SourcesFailed {
		found = found || sf.Path == big
	}
	if !found || len(instructionResources(g)) != 0 {
		t.Errorf("coverage %+v, resources %v", g.Coverage.SourcesFailed, instructionResources(g))
	}
}

// A symbolic link is listed without being followed.
func TestListAgentInstructions_SymlinkNotFollowed(t *testing.T) {
	e := newIdentityEnv(t)
	link := filepath.Join(e.repoA, "CLAUDE.md")
	if err := os.MkdirAll(e.repoA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing-target.md"), link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ins, err := windows.New().ListAgentInstructions(context.Background(), []string{e.repoA})
	if err != nil {
		t.Fatal(err)
	}
	want := []platform.InstructionFile{{Product: "claude-code", ConfigDir: filepath.Join(e.home, ".claude"), Path: link}}
	if !reflect.DeepEqual(ins.Files, want) || len(ins.Failed) != 0 {
		t.Errorf("scan: %+v", ins)
	}
}

// agentsResources lists, for every agent_instructions resource of path, the
// platform of the agent it configures, and checks the ids are distinct.
func agentsResources(t *testing.T, g graph.Graph, path string) []string {
	t.Helper()
	agents := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent {
			agents[n.ID] = n.Platform
		}
	}
	var out []string
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentInstructions && n.Path == path {
			out = append(out, agents[n.Configures])
			if ids[n.ID] {
				t.Errorf("duplicate resource id %s", n.ID)
			}
			ids[n.ID] = true
		}
	}
	sort.Strings(out)
	return out
}

// BV-29 D4: <repo>/AGENTS.md is one resource per agent that loads it; BV-48:
// in subfolders, Cursor's only.
func TestListAgentInstructions_AgentsMDPerAgent(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, false) // Claude Code and Cursor
	agentsA := filepath.Join(e.repoA, "AGENTS.md")
	agentsB := filepath.Join(e.repoB, "AGENTS.md")
	writeFile(t, agentsA, "Use "+fakeToken+"\n")
	writeFile(t, agentsB, "Use "+fakeToken+"\n")
	writeFile(t, filepath.Join(e.repoB, "CLAUDE.local.md"), "x")  // Claude Code reads CLAUDE files instead
	writeFile(t, filepath.Join(e.repoA, "sub", "AGENTS.md"), "x") // Cursor only (K10)

	g, out := instructionsGraph(t, windows.New(), []string{e.repoA, e.repoB})
	if got := agentsResources(t, g, agentsA); !reflect.DeepEqual(got, []string{"claude-code", "cursor"}) {
		t.Errorf("repo A AGENTS.md: %v", got)
	}
	if got := agentsResources(t, g, agentsB); !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Errorf("repo B AGENTS.md (with CLAUDE.local.md): %v", got)
	}
	// Cursor loads AGENTS.md in subfolders too (decision 2026-10-25,
	// Cursor, K10); Claude Code only the root one.
	if got := agentsResources(t, g, filepath.Join(e.repoA, "sub", "AGENTS.md")); !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Errorf("nested AGENTS.md: %v", got)
	}
	if strings.Contains(string(out), fakeToken) {
		t.Error("AGENTS.md content in the graph")
	}
}

// An agent that does not exist gets no AGENTS.md resource.
func TestListAgentInstructions_AgentsMDAbsentAgent(t *testing.T) {
	e := newIdentityEnv(t)
	agents := filepath.Join(e.repoA, "AGENTS.md")
	writeFile(t, agents, "x")

	writeFile(t, filepath.Join(e.home, ".claude.json"), `{"mcpServers": {}}`) // Claude Code only
	g, _ := instructionsGraph(t, windows.New(), []string{e.repoA})
	if got := agentsResources(t, g, agents); !reflect.DeepEqual(got, []string{"claude-code"}) {
		t.Errorf("without Cursor: %v", got)
	}

	if err := os.Remove(filepath.Join(e.home, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.home, ".cursor", "mcp.json"), `{"mcpServers": {}}`) // Cursor only
	g, _ = instructionsGraph(t, windows.New(), []string{e.repoA})
	if got := agentsResources(t, g, agents); !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Errorf("without Claude Code: %v", got)
	}
}
