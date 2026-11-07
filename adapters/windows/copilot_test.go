package windows

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// copilotEnv is a fixed environment for the reader tests.
func copilotEnv(vars map[string]string) agentdirs.Env {
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

func copilotLayer(t *testing.T, scan platform.CopilotScan, kind, root string) platform.CopilotLayer {
	t.Helper()
	for _, l := range scan.Layers {
		if l.Kind == kind && l.Root == root {
			return l
		}
	}
	t.Fatalf("no %s layer (root %q) in %+v", kind, root, scan.Layers)
	return platform.CopilotLayer{}
}

// TestReadCopilot covers the §7 fixtures: user global auto-approve, a project
// terminal catch-all, the registry policy (DWORD), an MCP server with a secret
// header (by fingerprint only) and the CLI credential file.
func TestReadCopilot(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	userDir := filepath.Join(home, "AppData", "Roaming", "Code", "User")
	repo := filepath.Join(tmp, "repo")

	// User settings.json with a comment (JSONC) and global auto-approve on.
	writeFileCodex(t, filepath.Join(userDir, "settings.json"), "{\n // yo\n \"chat.tools.global.autoApprove\": true\n}")
	// A profile settings.json with agent mode off.
	writeFileCodex(t, filepath.Join(userDir, "profiles", "p1", "settings.json"), `{"chat.agent.enabled": false}`)
	// Project .vscode/settings.json: terminal open with a catch-all regex.
	writeFileCodex(t, filepath.Join(repo, ".vscode", "settings.json"),
		`{"chat.tools.terminal.autoApprove": {"/.*/": true}, "chat.tools.terminal.ignoreDefaultAutoApproveRules": true}`)
	// User mcp.json (VS Code servers format) with a remote server + secret header.
	writeFileCodex(t, filepath.Join(userDir, "mcp.json"),
		`{"servers":{"api":{"url":"https://api.example.com/mcp/","headers":{"Authorization":"Bearer abcd1234efgh5678"}}}}`)
	// Copilot CLI credential file.
	writeFileCodex(t, filepath.Join(home, ".copilot", "config.json"), `{"github_token":"ghp_FAKEFAKEFAKE1234"}`)

	// Policy: HKLM ChatToolsAutoApprove = 0 (a DWORD) disables global approve.
	reg := codexFakeReg{HiveHKLM + "|" + copilotPolicyKey + "|ChatToolsAutoApprove": "0"}

	scan := ReadCopilot(context.Background(), userDir, home, copilotEnv(nil), []string{repo}, reg)

	if !scan.Present {
		t.Fatalf("Copilot should be present")
	}
	if u := copilotLayer(t, scan, platform.CopilotLayerUser, ""); u.GlobalAutoApprove == nil || !*u.GlobalAutoApprove {
		t.Errorf("user global.autoApprove: %+v", u)
	}
	if p := copilotLayer(t, scan, platform.CopilotLayerProfile, ""); p.AgentEnabled == nil || *p.AgentEnabled {
		t.Errorf("profile agent.enabled should be false: %+v", p)
	}
	proj := copilotLayer(t, scan, platform.CopilotLayerProject, repo)
	if proj.TerminalAutoApproveAll == nil || !*proj.TerminalAutoApproveAll {
		t.Errorf("project terminal catch-all should be true: %+v", proj)
	}
	if proj.TerminalIgnoreDefaults == nil || !*proj.TerminalIgnoreDefaults {
		t.Errorf("project ignore-default-rules should be true: %+v", proj)
	}
	man := copilotLayer(t, scan, platform.CopilotLayerManaged, "")
	if man.GlobalAutoApprove == nil || *man.GlobalAutoApprove {
		t.Errorf("policy ChatToolsAutoApprove=0 should give GlobalAutoApprove false: %+v", man)
	}

	// MCP: the remote server is read; its header is a secret, never in clear.
	var found bool
	for _, s := range scan.MCP {
		for _, srv := range s.Servers {
			if srv.ID == "api" {
				found = true
				if srv.URL != "https://api.example.com/mcp/" {
					t.Errorf("mcp url: %q", srv.URL)
				}
				if len(srv.Headers) != 1 || srv.Headers[0].Key != "Authorization" {
					t.Errorf("mcp header keys: %+v", srv.Headers)
				}
			}
		}
	}
	if !found {
		t.Errorf("the api MCP server was not read")
	}

	// Credentials are kept by fingerprint; the raw token never appears.
	if len(scan.Creds) == 0 {
		t.Errorf("the CLI credential was not read")
	}
	for _, c := range scan.Creds {
		if strings.Contains(c.Value.String(), "FAKEFAKE") {
			t.Errorf("a credential value leaked: %q", c.Value.String())
		}
	}
}

// TestReadCopilot_Absent: no Copilot config and no CLI folder → not present.
func TestReadCopilot_Absent(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	userDir := filepath.Join(home, "AppData", "Roaming", "Code", "User")
	scan := ReadCopilot(context.Background(), userDir, home, copilotEnv(nil), nil, nil)
	if scan.Present {
		t.Errorf("Copilot should not be present with no config: %+v", scan)
	}
}
