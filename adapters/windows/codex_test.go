package windows

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// envOf makes an agentdirs.Env from a map, for the reader tests.
func envOf(m map[string]string) agentdirs.Env {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// codexFakeReg serves one string value keyed "<hive>|<keyPath>|<valueName>".
type codexFakeReg map[string]string

func (codexFakeReg) PolicySettings(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (r codexFakeReg) StringValue(_ context.Context, hive, keyPath, valueName string) (string, bool, error) {
	v, ok := r[hive+"|"+keyPath+"|"+valueName]
	return v, ok, nil
}

func writeFileCodex(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func layerByKindRoot(scan platform.CodexScan, kind, root string) (platform.CodexLayer, bool) {
	for _, l := range scan.Layers {
		if l.Kind == kind && l.Root == root {
			return l, true
		}
	}
	return platform.CodexLayer{}, false
}

// BV-140: ReadCodex reads the TOML layers in order, gates project layers on the
// user config's trust, reads requirements, and keeps credentials by fingerprint
// only (CTO-16 §2–§4, §6).
func TestReadCodex_Layers(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	trusted := filepath.Join(home, "repo-ok")
	untrusted := filepath.Join(home, "repo-no")

	writeFileCodex(t, filepath.Join(codex, "config.toml"), `approval_policy = "on-request"
approvals_reviewer = "auto_review"
sandbox_mode = "workspace-write"
[sandbox_workspace_write]
network_access = false
[windows]
sandbox = "mxc"
[projects.'`+trusted+`']
trust_level = "trusted"
[mcp_servers.gh]
command = "npx"
args = ["-y", "server-github"]
env = { GITHUB_PERSONAL_ACCESS_TOKEN = "ghp_FAKEFAKEFAKEFAKE1234" }
[model_providers.mine]
experimental_bearer_token = "sk-FAKEbearerTOKEN9876"
`)
	writeFileCodex(t, filepath.Join(codex, "work.config.toml"), `sandbox_mode = "read-only"`+"\n")
	writeFileCodex(t, filepath.Join(trusted, ".codex", "config.toml"), `approval_policy = "never"`+"\n")
	writeFileCodex(t, filepath.Join(untrusted, ".codex", "config.toml"), `approval_policy = "never"`+"\n")
	// A rules file beside the trusted project config.
	writeFileCodex(t, filepath.Join(trusted, ".codex", "rules", "x.rules"), "allow *\n")
	req := filepath.Join(home, "pd", "OpenAI", "Codex", "requirements.toml")
	writeFileCodex(t, req, `sandbox_mode = "read-only"`+"\n")

	env := envOf(map[string]string{"CODEX_HOME": codex})
	scan := ReadCodex(home, env, []string{trusted, untrusted}, req)

	if !scan.Exists || scan.ConfigDir != codex {
		t.Fatalf("ConfigDir=%q Exists=%v", scan.ConfigDir, scan.Exists)
	}
	user, ok := layerByKindRoot(scan, platform.CodexLayerUser, "")
	if !ok || user.Status != platform.MCPSourceRead {
		t.Fatalf("user layer: %+v", user)
	}
	if user.ApprovalPolicy != "on-request" || !user.HasApprovalPolicy || user.ApprovalsReviewer != "auto_review" ||
		user.SandboxMode != "workspace-write" || user.NetworkAccess == nil || *user.NetworkAccess ||
		user.WindowsSandbox != "mxc" || !user.HasWindowsSandbox {
		t.Errorf("user fields: %+v", user)
	}
	if len(user.MCP) != 1 || user.MCP[0].ID != "gh" || user.MCP[0].Command != "npx" || len(user.MCP[0].EnvKeys) != 1 ||
		user.MCP[0].EnvKeys[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Errorf("user MCP: %+v", user.MCP)
	}
	// The token never appears in clear; only the redacted preview.
	if len(user.MCP[0].Env) == 1 {
		if got := user.MCP[0].Env[0].Value.String(); strings.Contains(got, "FAKEFAKE") || !strings.Contains(got, " ... ") {
			t.Errorf("MCP env not redacted: %q", got)
		}
	}
	if len(user.BearerTokens) != 1 || user.BearerTokens[0].Key != "mine" ||
		strings.Contains(user.BearerTokens[0].Value.String(), "bearerTOKEN") {
		t.Errorf("bearer token not redacted: %+v", user.BearerTokens)
	}

	prof, ok := layerByKindRoot(scan, platform.CodexLayerProfile, "")
	if !ok || prof.Status != platform.MCPSourceRead || prof.SandboxMode != "read-only" {
		t.Errorf("profile layer: %+v", prof)
	}
	tl, ok := layerByKindRoot(scan, platform.CodexLayerProject, trusted)
	if !ok || !tl.Trusted || tl.ApprovalPolicy != "never" || !tl.HasRulesFiles {
		t.Errorf("trusted project layer: %+v", tl)
	}
	ul, ok := layerByKindRoot(scan, platform.CodexLayerProject, untrusted)
	if !ok || ul.Trusted {
		t.Errorf("untrusted project layer should not be trusted: %+v", ul)
	}
	rl, ok := layerByKindRoot(scan, platform.CodexLayerRequirements, "")
	if !ok || rl.Status != platform.MCPSourceRead || rl.SandboxMode != "read-only" {
		t.Errorf("requirements layer: %+v", rl)
	}
}

func TestReadCodex_InvalidTOML(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	writeFileCodex(t, filepath.Join(codex, "config.toml"), "[[array_of_tables]]\nx = 1\n") // refused by the subset
	scan := ReadCodex(home, envOf(map[string]string{"CODEX_HOME": codex}), nil, "")
	user, _ := layerByKindRoot(scan, platform.CodexLayerUser, "")
	if user.Status != platform.MCPSourceFailed || !strings.Contains(user.Reason, "parse_error") {
		t.Errorf("invalid TOML should fail with a parse reason: %+v", user)
	}
}

func TestReadCodex_OversizeNotOpened(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	big := strings.Repeat("# filler comment line\n", 110000) // > 2 MB
	writeFileCodex(t, filepath.Join(codex, "config.toml"), big)
	scan := ReadCodex(home, envOf(map[string]string{"CODEX_HOME": codex}), nil, "")
	user, _ := layerByKindRoot(scan, platform.CodexLayerUser, "")
	if user.Status != platform.MCPSourceFailed || !strings.Contains(user.Reason, "size_limit_exceeded") {
		t.Errorf("oversize config should fail with a size reason: %+v", user)
	}
}

// resolveCodexRequirements prefers the HKLM RequirementsPath value and falls
// back to %ProgramData%\OpenAI\Codex\requirements.toml.
func TestResolveCodexRequirements(t *testing.T) {
	env := envOf(map[string]string{"ProgramData": `C:\PD`})
	if got := resolveCodexRequirements(context.Background(), env, nil); got != filepath.Join(`C:\PD`, "OpenAI", "Codex", "requirements.toml") {
		t.Errorf("ProgramData fallback: %q", got)
	}
	reg := codexFakeReg{HiveHKLM + "|" + codexPolicyKey + "|RequirementsPath": `D:\policy\req.toml`}
	if got := resolveCodexRequirements(context.Background(), env, reg); got != `D:\policy\req.toml` {
		t.Errorf("registry override: %q", got)
	}
}
