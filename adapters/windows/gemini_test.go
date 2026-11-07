package windows

import (
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-194 (CTO-22 Gemini §3, §5, §6): ReadGemini reads the extensions, the
// admin/user policy tiers, the settings.json hooks and the .env loading rule.
func TestReadGemini_BV194(t *testing.T) {
	home := t.TempDir()
	gem := filepath.Join(home, ".gemini")
	repo := filepath.Join(home, "repo")

	// An extension: gemini-extension.json with an MCP server and hooks, a
	// skills/ directory and a policies/*.toml.
	ext := filepath.Join(gem, "extensions", "demo")
	writeFileCodex(t, filepath.Join(ext, "gemini-extension.json"), `{
	  "name": "demo", "version": "1.2.3",
	  "mcpServers": {"srv": {"command": "node", "args": ["s.js"]}},
	  "contextFileName": "DEMO.md"
	}`)
	writeFileCodex(t, filepath.Join(ext, "skills", "x", "SKILL.md"), "x")
	writeFileCodex(t, filepath.Join(ext, "policies", "p.toml"), "[[rules]]\ntoolName = \"run_shell_command\"\ndecision = \"allow\"\npriority = 1\n")

	// A user policy tier.
	writeFileCodex(t, filepath.Join(gem, "policies", "u.toml"), "[[rules]]\ntoolName = [\"run_shell_command\"]\ndecision = \"allow\"\npriority = 2\ncommandPrefix = \"git\"\n")

	// A project settings.json with a hook whose command runs a repo script.
	writeFileCodex(t, filepath.Join(repo, ".gemini", "settings.json"), `{"hooks": {"BeforeTool": [{"hooks": [{"type": "command", "command": "node .gemini/hook.js"}]}]}}`)
	writeFileCodex(t, filepath.Join(repo, ".gemini", "hook.js"), "console.log(1)")

	// The .env chain of the (untrusted) repo root: .gemini/.env wins.
	writeFileCodex(t, filepath.Join(repo, ".gemini", ".env"), "OPENAI_API_KEY=sk-x\nGEMINI_API_KEY=g-x\n")
	writeFileCodex(t, filepath.Join(repo, ".env"), "FOO=bar\n")

	scan := ReadGemini(home, envOf(map[string]string{}), []string{repo}, filepath.Join(home, "none", "settings.json"), filepath.Join(home, "none", "system-defaults.json"))

	// Extension.
	if len(scan.Extensions) != 1 {
		t.Fatalf("extensions: got %d, want 1", len(scan.Extensions))
	}
	e := scan.Extensions[0]
	if e.Name != "demo" || e.Version != "1.2.3" || !e.HasSkills || len(e.MCP) != 1 || len(e.Policies) != 1 {
		t.Errorf("extension: %+v", e)
	}
	if len(e.ContextFileNames) != 1 || e.ContextFileNames[0] != "DEMO.md" {
		t.Errorf("extension context: %v", e.ContextFileNames)
	}

	// User policy tier.
	var userPol *platform.GeminiPolicyLayer
	for i := range scan.Policies {
		if scan.Policies[i].Kind == platform.GeminiPolicyUser {
			userPol = &scan.Policies[i]
		}
	}
	if userPol == nil || len(userPol.Rules) != 1 || userPol.Rules[0].Decision != "allow" {
		t.Errorf("user policies: %+v", scan.Policies)
	} else if len(userPol.Rules[0].CommandPrefix) != 1 || userPol.Rules[0].CommandPrefix[0] != "git" {
		t.Errorf("user policy prefix: %v", userPol.Rules[0].CommandPrefix)
	}

	// Project settings hook resolves the repo script.
	proj, ok := geminiLayerByKind(scan.Layers, platform.GeminiLayerProject)
	if !ok || len(proj.Hooks) != 1 || proj.Hooks[0].ScriptPath == "" {
		t.Errorf("project hook: %+v", proj.Hooks)
	}

	// The .env rule: in the untrusted repo, the winning .gemini/.env loads only
	// the allowed variables (OPENAI line denied, GEMINI line not), and the
	// non-winning .env is fully denied.
	denied := map[string]bool{}
	for _, d := range scan.EnvDenied {
		denied[d] = true
	}
	winner := filepath.Join(repo, ".gemini", ".env")
	if !denied[winner+":1"] { // OPENAI_API_KEY: not one of the four
		t.Errorf("OPENAI line should be denied; EnvDenied=%v", scan.EnvDenied)
	}
	if denied[winner+":2"] { // GEMINI_API_KEY: allowed even untrusted
		t.Errorf("GEMINI_API_KEY line should not be denied")
	}
	if !denied[filepath.Join(repo, ".env")+":1"] { // non-winning .env: fully denied
		t.Errorf("non-winning .env should be denied; EnvDenied=%v", scan.EnvDenied)
	}
}
