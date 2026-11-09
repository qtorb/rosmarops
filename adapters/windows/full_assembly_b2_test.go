package windows_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
)

func b2mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func b2write(t *testing.T, p, body string) {
	t.Helper()
	b2mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// CTO-30 B2: real readers -> pipeline.Assemble; each of the four ai_clients the
// point-1/B1 fix touches (Copilot, Cline, Antigravity CLI, Gemini CLI) must end
// with its skill-derived agent_instructions. Fails without B1 (Copilot's skill
// never attaches), passes with B1, on all three systems (pure readers + env).
func TestFullAssemblyFourClientsHaveSkillInstructions(t *testing.T) {
	e := newIdentityEnv(t)
	home := e.home
	vscodeUser := filepath.Join(e.appData, "Code", "User")

	// Copilot: present via a user settings.json and its globalStorage folder;
	// a private skill under ~/.copilot/skills.
	b2write(t, filepath.Join(vscodeUser, "settings.json"), `{"chat.agent.enabled": true}`)
	b2mkdir(t, filepath.Join(vscodeUser, "globalStorage", "github.copilot-chat"))
	b2write(t, filepath.Join(home, ".copilot", "skills", "x", "SKILL.md"), "# copilot skill\n")

	// Cline: present via its data folder.
	b2mkdir(t, filepath.Join(home, ".cline", "data", "settings"))

	// Antigravity CLI (scored) and the app (detected).
	b2write(t, filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), `{}`)
	b2mkdir(t, filepath.Join(home, ".gemini", "antigravity"))

	// Gemini CLI: present via its settings.json.
	b2write(t, filepath.Join(home, ".gemini", "settings.json"), `{}`)

	// A repo with AGENTS.md, GEMINI.md and a shared .agents/skills skill.
	repo := e.repoA
	b2write(t, filepath.Join(repo, "AGENTS.md"), "# shared\n")
	b2write(t, filepath.Join(repo, "GEMINI.md"), "# gemini\n")
	b2write(t, filepath.Join(repo, ".agents", "skills", "deploy", "SKILL.md"), "# deploy\n")

	w := windows.New()
	ctx := context.Background()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mcp, err := w.ListMCPServers(ctx, []string{repo})
	must(err)
	settings, err := w.ListClaudeSettings(ctx, []string{repo})
	must(err)
	copilot, err := w.ListCopilot(ctx, []string{repo})
	must(err)
	antigravity, err := w.ListAntigravity(ctx, []string{repo})
	must(err)
	gemini, err := w.ListGemini(ctx, []string{repo})
	must(err)
	cline, err := w.ListCline(ctx, []string{repo})
	must(err)
	instructions, err := w.ListAgentInstructions(ctx, []string{repo})
	must(err)
	skillScan, err := w.ListSkills(ctx, []string{repo})
	must(err)

	res, err := pipeline.Assemble(pipeline.Input{
		MCP: mcp, Settings: settings, Copilot: copilot, Antigravity: antigravity,
		Gemini: gemini, Cline: cline, Instructions: instructions, Skills: skillScan,
		Provenance: graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID([]byte("0123456789abcdef0123456789abcdef"))},
		Salt:       []byte("0123456789abcdef0123456789abcdef"), RepoRoots: []string{repo}, Home: home,
	})
	must(err)
	g := res.Graph

	// Each product's ai_client id (point 6: one scored client per product).
	clientOf := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			clientOf[n.Platform] = n.ID
		}
	}
	// Every SKILL.md-derived agent_instructions resource, by the client it
	// configures. A SKILL.md attaches only when the skill scan's (product,
	// ConfigDir) matches the client's — exactly what B1 corrects for Copilot.
	skillInstr := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentInstructions && strings.HasSuffix(filepath.ToSlash(n.Path), "/SKILL.md") {
			skillInstr[n.Configures] = true
		}
	}

	// The four ai_clients the point-1/B1 fix governs must each be present and
	// carry their skill's agent_instructions. Without B1, Copilot's skills
	// (~/.copilot/skills and the shared .agents/skills) resolve to clientID
	// (github-copilot, ~/.copilot), which no client has, so they never attach
	// and Copilot alone is missing here.
	for _, product := range []string{"github-copilot", "cline", "antigravity-cli", "gemini-cli"} {
		id, ok := clientOf[product]
		if !ok {
			t.Errorf("%s has no scored ai_client in the assembled graph", product)
			continue
		}
		if !skillInstr[id] {
			t.Errorf("%s ai_client (%s) has no SKILL.md agent_instructions — its skills did not attach", product, id)
		}
	}
}
