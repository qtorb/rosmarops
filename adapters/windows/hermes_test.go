package windows_test

import (
	"path/filepath"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

func hermesEnv(dir string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		if k == "HERMES_HOME" {
			return dir, true
		}
		return "", false
	}
}

// N2a (BV-97): <HERMES_HOME>\config.yaml and a profile's, read with the YAML
// subset reader: approvals.mode as Hermes reads it, the protected
// instruction files, and the MCP servers (local and remote) with their env
// and headers as credentials.
func TestReadHermes(t *testing.T) {
	home := t.TempDir()
	hermes := filepath.Join(home, "hermes")
	writeFile(t, filepath.Join(hermes, "config.yaml"), `# Hermes
approvals:
  mode: off
security:
  protected_instruction_files: true
mcp_servers:
  local:
    command: node
    args: ["server.js", --port, "8080"]
    env:
      API_KEY: "sk-fake"
  remote:
    url: https://user:pw@mcp.example.com/sse?token=x
    transport: sse
    headers:
      Authorization: "Bearer abc"
    command: ignored
`)
	writeFile(t, filepath.Join(hermes, "profiles", "work", "config.yaml"), "approvals:\n  mode: Smart\nmcp_servers: {}\n")
	writeFile(t, filepath.Join(hermes, "profiles", "play", "config.yaml"), "approvals:\n  mode: yolo\n")
	// Anchors/aliases under keys RosmarOps does not read are skipped, not a
	// failure (BV-101, decision 1); with no approvals section the mode defaults.
	writeFile(t, filepath.Join(hermes, "profiles", "bad", "config.yaml"), "base: &b\n  x: 1\nother: *b\n")

	scan := windows.ReadHermes(home, hermesEnv(hermes))
	modes := map[string]string{}
	for _, c := range scan.Configs {
		modes[filepath.Base(filepath.Dir(c.Path))] = c.ApprovalMode + "/" + string(c.Status)
	}
	want := map[string]string{
		"hermes": "off/" + string(platform.MCPSourceRead),
		"work":   "/" + string(platform.MCPSourceFailed), // {} under mcp_servers (a read key): parse_error
		"play":   "manual/" + string(platform.MCPSourceRead),
		"bad":    "smart/" + string(platform.MCPSourceRead), // anchors under non-read keys: skipped
	}
	for k, w := range want {
		if modes[k] != w {
			t.Errorf("%s: %q, want %q", k, modes[k], w)
		}
	}
	for _, c := range scan.Configs {
		if strings.HasSuffix(filepath.Dir(c.Path), "hermes") && (c.ProtectedInstructionFiles == nil || !*c.ProtectedInstructionFiles) {
			t.Errorf("protected_instruction_files %v", c.ProtectedInstructionFiles)
		}
	}
	if len(scan.Sources) != 4 {
		t.Errorf("sources %+v", scan.Sources)
	}
	for _, s := range scan.Sources {
		if s.Platform != "hermes" || !s.LoadsMCP || s.Status == platform.MCPSourceFailed && !strings.HasPrefix(s.Reason, "parse_error") {
			t.Errorf("source %+v", s)
		}
	}
	if len(scan.Servers) != 2 {
		t.Fatalf("servers %+v", scan.Servers)
	}
	local, remote := scan.Servers[0], scan.Servers[1]
	if local.Name != "local" || local.Transport != "stdio" || local.Command != "node" || strings.Join(local.Args, " ") != "server.js --port 8080" ||
		len(local.Env) != 1 || local.Env[0].Value.Reveal() != "sk-fake" || local.Pointer != "/mcp_servers/local" || local.ConfigDir != hermes {
		t.Errorf("local %+v", local)
	}
	if remote.Name != "remote" || remote.Transport != "sse" || remote.URL != "https://user:pw@mcp.example.com/sse?token=x" || remote.Command != "" ||
		len(remote.Headers) != 1 || remote.Headers[0].Key != "Authorization" {
		t.Errorf("remote %+v", remote)
	}
}

// approvals.mode as Hermes reads it (tools/approval_context.py:266-280).
func TestReadHermesApprovalModes(t *testing.T) {
	for doc, want := range map[string]string{
		"model:\n  default: x\n":         "smart", // absent → smart (BV-101 decision 2)
		"approvals:\n  mode: off\n":      "off",   // YAML 1.1 false
		"approvals:\n  mode: No\n":       "off",
		"approvals:\n  mode: \"off\"\n":  "off", // the string
		"approvals:\n  mode: on\n":       "manual",
		"approvals:\n  mode: SMART\n":    "smart",
		"approvals:\n  mode: auto\n":     "manual", // unknown
		"approvals:\n  mode:\n":          "manual",
		"approvals:\n  timeout: 300\n":   "smart", // mode absent → smart (BV-101 decision 2)
		"approvals:\n  mode: manual\n":   "manual",
		"approvals:\n  mode: 'smart'\n":  "smart",
		"approvals:\n  mode: [off]\n":    "manual",
		"approvals:\n  mode: \"yes\"\n":  "manual",
		"approvals:\n  mode: false\n":    "off",
		"approvals:\n  mode: true\n":     "manual",
		"approvals:\n  mode: Off  # x\n": "off",
	} {
		home := t.TempDir()
		hermes := filepath.Join(home, "hermes")
		writeFile(t, filepath.Join(hermes, "config.yaml"), doc)
		scan := windows.ReadHermes(home, hermesEnv(hermes))
		if len(scan.Configs) != 1 || scan.Configs[0].ApprovalMode != want {
			t.Errorf("%q: %+v, want %s", doc, scan.Configs, want)
		}
	}
}

// No config.yaml: nothing read, no source, no failure.
func TestReadHermesAbsent(t *testing.T) {
	home := t.TempDir()
	if scan := windows.ReadHermes(home, hermesEnv(filepath.Join(home, "hermes"))); len(scan.Configs)+len(scan.Sources)+len(scan.Servers) != 0 {
		t.Errorf("scan %+v", scan)
	}
}
