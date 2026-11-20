package windows_test

import (
	"os"
	"path/filepath"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

func putFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func noEnv(string) (string, bool) { return "", false }

// TestReadPaperclip_Configs reads the two deployment modes, both binds, sign-up
// open and closed, and the two keys that hold secrets (only whether they are
// set).
func TestReadPaperclip_Configs(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".paperclip", "instances")
	putFile(t, filepath.Join(base, "default", "config.json"), `{"server":{"deploymentMode":"local_trusted","bind":"loopback","host":"127.0.0.1","port":3100}}`)
	putFile(t, filepath.Join(base, "team", "config.json"), `{"server":{"deploymentMode":"authenticated","bind":"lan","host":"0.0.0.0"},"auth":{"disableSignUp":true},"llm":{"apiKey":"sk-x"},"database":{"connectionString":""},"telemetry":{"enabled":false}}`)
	scan := windows.ReadPaperclip(agentdirs.LinuxLayout, home, noEnv, nil)
	if len(scan.Instances) != 2 {
		t.Fatalf("instances %+v", scan.Instances)
	}
	byID := map[string]platform.PaperclipInstance{}
	for _, in := range scan.Instances {
		byID[in.ID] = in
	}
	d, team := byID["default"], byID["team"]
	if d.DeploymentMode != "local_trusted" || d.Bind != "loopback" || d.Port != 3100 || d.SignUpDisabled != nil || d.LLMAPIKeySet {
		t.Errorf("default %+v", d)
	}
	if team.DeploymentMode != "authenticated" || team.Bind != "lan" || team.SignUpDisabled == nil || !*team.SignUpDisabled {
		t.Errorf("team %+v", team)
	}
	if !team.LLMAPIKeySet || team.ConnectionStringSet {
		t.Errorf("team keys: apiKey set %v, connectionString set %v (an empty string is not set)", team.LLMAPIKeySet, team.ConnectionStringSet)
	}
	if team.Telemetry == nil || *team.Telemetry {
		t.Errorf("team telemetry %v", team.Telemetry)
	}
	if len(scan.Sources) != 2 {
		t.Errorf("sources %+v", scan.Sources)
	}
}

// TestReadPaperclip_Versions reads cli/install.json and the versions in npm's
// cache.
func TestReadPaperclip_Versions(t *testing.T) {
	for _, v := range []string{"2026.415.0", "2026.1117.0"} {
		home := t.TempDir()
		putFile(t, filepath.Join(home, ".paperclip", "instances", "default", "config.json"), `{}`)
		putFile(t, filepath.Join(home, ".paperclip", "cli", "install.json"), `{"version":"`+v+`","source":"install.sh"}`)
		putFile(t, filepath.Join(home, ".npm", "_npx", "a1b2", "node_modules", "paperclipai", "package.json"), `{"name":"paperclipai","version":"2026.410.0"}`)
		scan := windows.ReadPaperclip(agentdirs.LinuxLayout, home, noEnv, nil)
		if scan.Installed != v {
			t.Errorf("installed %q, want %q", scan.Installed, v)
		}
		if len(scan.NPMCache) != 1 || scan.NPMCache[0].Version != "2026.410.0" {
			t.Errorf("npm cache %+v", scan.NPMCache)
		}
	}
}

// TestReadPaperclip_HomeAndService: PAPERCLIP_HOME wins over ~/.paperclip, and
// a systemd unit's PAPERCLIP_* variables are kept (only those).
func TestReadPaperclip_HomeAndService(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "pc")
	putFile(t, filepath.Join(custom, "instances", "default", "config.json"), `{}`)
	putFile(t, filepath.Join(home, ".config", "systemd", "user", "paperclip.service"),
		"[Service]\nEnvironment=PAPERCLIP_BIND=lan PAPERCLIP_AUTH_DISABLE_SIGN_UP=true OTHER=x\nExecStart=/usr/bin/paperclipai run\n")
	env := func(k string) (string, bool) {
		if k == "PAPERCLIP_HOME" {
			return custom, true
		}
		return "", false
	}
	scan := windows.ReadPaperclip(agentdirs.LinuxLayout, home, env, nil)
	if scan.Base != custom || len(scan.Instances) != 1 {
		t.Fatalf("base %q instances %d", scan.Base, len(scan.Instances))
	}
	if scan.Service == nil || scan.Service.Env["PAPERCLIP_BIND"] != "lan" || scan.Service.Env["PAPERCLIP_AUTH_DISABLE_SIGN_UP"] != "true" {
		t.Fatalf("service %+v", scan.Service)
	}
	if _, ok := scan.Service.Env["OTHER"]; ok {
		t.Error("a variable that is not Paperclip's was kept")
	}
}

// TestReadPaperclip_Manifests counts the agents of a .paperclip.yaml that run
// an arbitrary command: type process, and no type (imported as process).
func TestReadPaperclip_Manifests(t *testing.T) {
	home := t.TempDir()
	putFile(t, filepath.Join(home, ".paperclip", "instances", "default", "config.json"), `{}`)
	repo := filepath.Join(t.TempDir(), "acme")
	putFile(t, filepath.Join(repo, ".paperclip.yaml"), "schema: paperclip/v1\nagents:\n  builder:\n    adapter:\n      type: process\n  helper:\n    runtime:\n      heartbeat: 3600\n  coder:\n    adapter:\n      type: claude-local\n")
	scan := windows.ReadPaperclip(agentdirs.LinuxLayout, home, noEnv, []string{repo})
	if len(scan.Manifests) != 1 {
		t.Fatalf("manifests %+v", scan.Manifests)
	}
	if m := scan.Manifests[0]; m.ProcessAgents != 1 || m.UntypedAgents != 1 {
		t.Errorf("manifest %+v, want 1 process and 1 untyped", m)
	}
}

// TestReadPaperclip_Absent: no Paperclip home, nothing read (a manifest alone
// is inert).
func TestReadPaperclip_Absent(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "acme")
	putFile(t, filepath.Join(repo, ".paperclip.yaml"), "schema: paperclip/v1\n")
	if scan := windows.ReadPaperclip(agentdirs.LinuxLayout, home, noEnv, []string{repo}); scan.Base != "" || len(scan.Instances) != 0 || len(scan.Manifests) != 0 {
		t.Errorf("scan %+v", scan)
	}
}
