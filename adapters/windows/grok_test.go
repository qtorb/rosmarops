package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

func grokLayerByKindRoot(scan platform.GrokScan, kind, root string) (platform.GrokLayer, bool) {
	for _, l := range scan.Layers {
		if l.Kind == kind && l.Root == root {
			return l, true
		}
	}
	return platform.GrokLayer{}, false
}

// BV-140: ReadGrok reads the user config, the managed and requirements layers,
// trusted_folders.toml and each repo's .grok/config.toml (in force only when
// trusted), keeping credentials by fingerprint only (CTO-16 Grok §2–§4).
func TestReadGrok_Layers(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	trusted := filepath.Join(home, "repo-ok")
	untrusted := filepath.Join(home, "repo-no")

	writeFileCodex(t, filepath.Join(grok, "config.toml"), `[ui]
permission_mode = "ask"
yolo = false
[permission]
allow = ["Read(*)"]
deny = ["Bash(rm *)"]
[claude_compat]
imported = true
[compat.cursor]
mcps = false
hooks = false
[model.mine]
api_key = "xai-FAKEapikey0000WXYZ"
[telemetry]
events_api_key = "tel-FAKEtelemetry9999"
[mcp_servers.github]
command = "npx"
args = ["-y", "server-github"]
env = { GITHUB_PERSONAL_ACCESS_TOKEN = "ghp_FAKEFAKEFAKEFAKE1234" }
`)
	writeFileCodex(t, filepath.Join(grok, "auth.json"), `{"tokens":{"access_token":"grok-sess-FAKEtoken5678","refresh_token":"grok-refresh-FAKErt90"}}`)
	writeFileCodex(t, filepath.Join(grok, "trusted_folders.toml"), `[folders.'`+trusted+`']
trusted = true
decided_at = 1730000000
`)
	writeFileCodex(t, filepath.Join(trusted, ".grok", "config.toml"), `[permission]
allow = ["*"]
[mcp_servers.x]
command = "node"
args = ["x.js"]
`)
	writeFileCodex(t, filepath.Join(untrusted, ".grok", "config.toml"), `[permission]
allow = ["*"]
`)

	env := envOf(map[string]string{"GROK_HOME": grok})
	scan := ReadGrok(home, env, []string{trusted, untrusted})

	if !scan.Exists || scan.ConfigDir != grok {
		t.Fatalf("ConfigDir=%q Exists=%v", scan.ConfigDir, scan.Exists)
	}
	user, ok := grokLayerByKindRoot(scan, platform.GrokLayerUser, "")
	if !ok || user.Status != platform.MCPSourceRead {
		t.Fatalf("user layer: %+v", user)
	}
	if user.PermissionMode != "ask" || !user.HasPermissionMode || user.Yolo {
		t.Errorf("ui: %+v", user)
	}
	if len(user.Allow) != 1 || user.Allow[0] != "Read(*)" || len(user.Deny) != 1 || user.Deny[0] != "Bash(rm *)" {
		t.Errorf("permission rules: %+v / %+v", user.Allow, user.Deny)
	}
	if !user.ClaudeImported {
		t.Errorf("claude_compat imported not read")
	}
	mcps, mcpsSet := user.Compat["cursor.mcps"]
	hooks, hooksSet := user.Compat["cursor.hooks"]
	if !mcpsSet || !hooksSet || mcps || hooks {
		t.Errorf("compat cells cursor.mcps/hooks should be set to false: %+v", user.Compat)
	}
	// Credentials by fingerprint, never in clear.
	if len(user.APIKeys) != 2 {
		t.Errorf("api keys: %+v", user.APIKeys)
	}
	for _, k := range user.APIKeys {
		if strings.Contains(k.Value.String(), "FAKE") {
			t.Errorf("api key not redacted: %q", k.Value.String())
		}
	}
	if len(user.MCP) != 1 || user.MCP[0].ID != "github" || len(user.MCP[0].EnvKeys) != 1 {
		t.Errorf("mcp: %+v", user.MCP)
	}
	if len(user.SessionCreds) == 0 {
		t.Errorf("auth.json session creds not read")
	}
	for _, c := range user.SessionCreds {
		if strings.Contains(c.Value.String(), "FAKEtoken") || strings.Contains(c.Value.String(), "FAKErt") {
			t.Errorf("session cred not redacted: %q", c.Value.String())
		}
	}

	tl, ok := grokLayerByKindRoot(scan, platform.GrokLayerProject, trusted)
	if !ok || !tl.Trusted || len(tl.Allow) != 1 || tl.Allow[0] != "*" || len(tl.MCP) != 1 {
		t.Errorf("trusted project layer: %+v", tl)
	}
	ul, ok := grokLayerByKindRoot(scan, platform.GrokLayerProject, untrusted)
	if !ok || ul.Trusted {
		t.Errorf("untrusted project layer should not be trusted: %+v", ul)
	}
	// The project layer carries no [ui] even if present elsewhere.
	if tl.HasPermissionMode {
		t.Errorf("project layer must not read [ui]")
	}
}

// BV-140 (CTO-16 Grok §2): the folder-trust control off ([folder_trust]
// enabled = false) trusts every project without a trusted_folders entry.
func TestReadGrok_FolderTrustOff(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	repo := filepath.Join(home, "repo")
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "[folder_trust]\nenabled = false\n")
	writeFileCodex(t, filepath.Join(repo, ".grok", "config.toml"), "[permission]\nallow = [\"*\"]\n")

	scan := ReadGrok(home, envOf(map[string]string{"GROK_HOME": grok}), []string{repo})
	if !scan.FolderTrustOff {
		t.Fatalf("FolderTrustOff should be true")
	}
	pl, _ := grokLayerByKindRoot(scan, platform.GrokLayerProject, repo)
	if !pl.Trusted {
		t.Errorf("with folder-trust off the project should be trusted: %+v", pl)
	}
}

// A repo with no .grok/config.toml is trusted (nothing to load), even without a
// trusted_folders entry (CTO-16 Grok §2 exception).
func TestReadGrok_EmptyProjectTrusted(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	repo := filepath.Join(home, "repo")
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "[models]\ndefault = \"grok-4.5\"\n")

	scan := ReadGrok(home, envOf(map[string]string{"GROK_HOME": grok}), []string{repo})
	pl, ok := grokLayerByKindRoot(scan, platform.GrokLayerProject, repo)
	if !ok || pl.Status != platform.MCPSourceMissing || !pl.Trusted {
		t.Errorf("empty project should read missing and be trusted: %+v", pl)
	}
}

func TestReadGrok_InvalidTOML(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	// BV-193: arrays of tables now parse, so use genuinely invalid TOML — an
	// unterminated string — to exercise the parse-error path.
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "x = \"unterminated\n")
	scan := ReadGrok(home, envOf(map[string]string{"GROK_HOME": grok}), nil)
	user, _ := grokLayerByKindRoot(scan, platform.GrokLayerUser, "")
	if user.Status != platform.MCPSourceFailed || !strings.Contains(user.Reason, "parse_error") {
		t.Errorf("invalid TOML should fail with a parse reason: %+v", user)
	}
}

// BV-237: [permission] rules as CTO-50's P0 profile writes them (an array of
// inline tables over several lines) are read; an unknown tool is skipped.
func TestReadGrok_PermissionRules(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(`[ui]
permission_mode = "ask"

[permission]
rules = [
  { action = "deny", tool = "edit" },
  { action = "deny", tool = "bash" },
  { action = "allow", tool = "bash", pattern = "git status" },
  { action = "deny", tool = "websearch" },
]

[sandbox]
profile = "read-only"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	l := readGrokLayer(p, platform.GrokLayerUser, "", false)
	if l.Status != platform.MCPSourceRead {
		t.Fatalf("status %s: %s", l.Status, l.Reason)
	}
	want := []platform.GrokRule{{Action: "deny", Tool: "edit"}, {Action: "deny", Tool: "bash"}, {Action: "allow", Tool: "bash", Pattern: "git status"}}
	if len(l.Rules) != len(want) {
		t.Fatalf("rules %+v, want %+v", l.Rules, want)
	}
	for i := range want {
		if l.Rules[i] != want[i] {
			t.Errorf("rule %d: %+v, want %+v", i, l.Rules[i], want[i])
		}
	}
	if l.PermissionMode != "ask" || l.SandboxProfile != "read-only" {
		t.Errorf("mode %q profile %q", l.PermissionMode, l.SandboxProfile)
	}
}
