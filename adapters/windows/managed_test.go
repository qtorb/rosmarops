package windows_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// fakeRegistry serves Settings values per hive; it records nothing and
// writes nothing.
type fakeRegistry map[string]string

func (r fakeRegistry) PolicySettings(_ context.Context, hive string) ([]byte, bool, error) {
	v, ok := r[hive]
	if v == "ERROR" {
		return nil, false, errors.New("access denied")
	}
	return []byte(v), ok, nil
}

const (
	hklmSettings = `registry:HKLM\SOFTWARE\Policies\ClaudeCode\Settings`
	hkcuSettings = `registry:HKCU\SOFTWARE\Policies\ClaudeCode\Settings`
)

// managedGraph runs the adapter with a managed directory and registry and
// returns the graph built from it.
func managedGraph(t *testing.T, e identityEnv, dir, legacy string, reg fakeRegistry) (graph.Graph, platform.ClaudeSettingsScan) {
	t.Helper()
	w := windows.NewWithManaged(dir, legacy, reg)
	settings, err := w.ListClaudeSettings(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := w.ListMCPServers(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := graph.FromScan(mcp, settings, testSalt, testTime)
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime)
	if _, err := graph.Encode(g); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return g, settings
}

func managedRulesOf(g graph.Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.AppliesIn == "managed" {
			out = append(out, n.Rule)
		}
	}
	sort.Strings(out)
	return out
}

func TestListClaudeSettings_ManagedPrecedenceThroughAdapter(t *testing.T) {
	e := newIdentityEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "managed-settings.json"), `{"permissions": {"allow": ["Bash(files)"]}}`)
	writeFile(t, filepath.Join(dir, "managed-settings.d", "20-b.json"), `{"permissions": {"allow": ["Bash(d20)"]}}`)
	writeFile(t, filepath.Join(dir, "managed-settings.d", "10-a.json"), `{"permissions": {"allow": ["Bash(d10)"]}}`)
	writeFile(t, filepath.Join(dir, "managed-settings.d", ".hidden.json"), `{"permissions": {"allow": ["Bash(hidden)"]}}`)
	writeFile(t, filepath.Join(dir, "managed-settings.d", "notes.txt"), `{"permissions": {"allow": ["Bash(txt)"]}}`)
	writeFile(t, filepath.Join(dir, "managed-mcp.json"), `{"mcpServers": {}}`)

	// No registry: the files are the policy, managed-settings.json first,
	// then .d in alphabetical order, hidden and non-.json files ignored.
	g, scan := managedGraph(t, e, dir, filepath.Join(t.TempDir(), "absent.json"), fakeRegistry{})
	if got := managedRulesOf(g); !reflect.DeepEqual(got, []string{"Bash(d10)", "Bash(d20)", "Bash(files)"}) {
		t.Errorf("files policy: got %v", got)
	}
	var order []string
	for _, src := range scan.Managed {
		order = append(order, src.Kind)
	}
	if want := []string{platform.ManagedRemote, platform.ManagedHKLM, platform.ManagedFiles, platform.ManagedHKCU}; !reflect.DeepEqual(order, want) {
		t.Errorf("source order: got %v, want %v", order, want)
	}
	if scan.ManagedMCP == nil || scan.ManagedMCP.Status != platform.MCPSourceRead {
		t.Errorf("managed-mcp.json: %+v", scan.ManagedMCP)
	}

	// HKLM with a policy key wins over the files (first-wins).
	g, _ = managedGraph(t, e, dir, "", fakeRegistry{windows.HiveHKLM: `{"permissions": {"allow": ["Bash(hklm)"]}}`})
	if got := managedRulesOf(g); !reflect.DeepEqual(got, []string{"Bash(hklm)"}) {
		t.Errorf("HKLM policy: got %v", got)
	}
	paths := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentConfig {
			paths[n.Path] = true
		}
	}
	for _, p := range []string{hklmSettings, filepath.Join(dir, "managed-settings.json"), filepath.Join(dir, "managed-mcp.json")} {
		if !paths[p] {
			t.Errorf("read managed source %s is not an agent_config", p)
		}
	}
}

func TestListClaudeSettings_HKCUOnlyAndUnreadableHKLM(t *testing.T) {
	e := newIdentityEnv(t)
	empty := t.TempDir()
	g, _ := managedGraph(t, e, empty, "", fakeRegistry{windows.HiveHKCU: `{"permissions": {"deny": ["Bash(rm *)"]}}`})
	if got := managedRulesOf(g); !reflect.DeepEqual(got, []string{"Bash(rm *)"}) {
		t.Errorf("HKCU alone: got %v", got)
	}

	// An HKLM value that cannot be read is a present admin document: HKCU
	// does not apply beneath it, and the failure is in coverage.
	g, _ = managedGraph(t, e, empty, "", fakeRegistry{windows.HiveHKLM: "ERROR", windows.HiveHKCU: `{"permissions": {"deny": ["Bash(rm *)"]}}`})
	if got := managedRulesOf(g); len(got) != 0 {
		t.Errorf("HKCU under unreadable HKLM: got %v", got)
	}
	var failed bool
	for _, sf := range g.Coverage.SourcesFailed {
		failed = failed || (sf.Path == hklmSettings && strings.HasPrefix(sf.Reason, graph.ReasonReadError+": "))
	}
	if !failed {
		t.Errorf("unreadable HKLM not in coverage: %+v", g.Coverage.SourcesFailed)
	}
}

func TestListClaudeSettings_LegacyPathNotOpened(t *testing.T) {
	e := newIdentityEnv(t)
	legacy := filepath.Join(t.TempDir(), "managed-settings.json")
	writeFile(t, legacy, `{"permissions": {"allow": ["Bash(legacy)"]}}`)
	g, _ := managedGraph(t, e, t.TempDir(), legacy, fakeRegistry{})
	if got := managedRulesOf(g); len(got) != 0 {
		t.Errorf("legacy file was read: %v", got)
	}
	var found bool
	for _, sf := range g.Coverage.SourcesFailed {
		found = found || (sf.Path == legacy && sf.Reason == graph.ReasonSourceUnsupported+": "+graph.LegacyManagedReason)
	}
	if !found {
		t.Errorf("legacy path not in coverage: %+v", g.Coverage.SourcesFailed)
	}
	for _, n := range g.Nodes {
		if n.Path == legacy {
			t.Errorf("legacy path produced a node: %+v", n)
		}
	}
}

func TestListClaudeSettings_HooksEnvAndApprovalsParsed(t *testing.T) {
	e := newIdentityEnv(t)
	writeFile(t, filepath.Join(e.home, ".claude", "settings.json"), `{
		"env": {"GITHUB_TOKEN": "ghp_ENVENVENVENVENVENVENVENVENVENVENVENV"},
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [
				{"type": "command", "command": "echo HOOK-MARKER-ONE"},
				{"type": "http", "url": "https://hooks.example.com"}
			]}],
			"Stop": [{"hooks": [{"type": "command", "command": "echo HOOK-MARKER-TWO"}]}]
		},
		"statusLine": {"type": "command", "command": "echo STATUS-MARKER"},
		"apiKeyHelper": "echo HELPER-MARKER",
		"enabledMcpjsonServers": ["db"],
		"disabledMcpjsonServers": ["evil"],
		"enableAllProjectMcpServers": false
	}`)
	scan, err := windows.New().ListClaudeSettings(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	user := scan.Files[0]
	wantExec := []string{"/hooks/PreToolUse/0/hooks/0/command", "/hooks/Stop/0/hooks/0/command", "/statusLine/command", "/apiKeyHelper"}
	if !reflect.DeepEqual(user.Executables, wantExec) {
		t.Errorf("executables: got %v, want %v", user.Executables, wantExec)
	}
	if !reflect.DeepEqual(user.EnabledMcpjsonServers, []string{"db"}) || !reflect.DeepEqual(user.DisabledMcpjsonServers, []string{"evil"}) ||
		user.EnableAllProjectMcpServers == nil || *user.EnableAllProjectMcpServers {
		t.Errorf("approvals: %+v %+v %v", user.EnabledMcpjsonServers, user.DisabledMcpjsonServers, user.EnableAllProjectMcpServers)
	}
	if len(user.Env) != 1 || user.Env[0].Key != "GITHUB_TOKEN" || !user.HasPolicyKey {
		t.Errorf("env/policy: %+v %v", user.Env, user.HasPolicyKey)
	}

	f, _, err := graph.FromScan(platform.MCPScan{}, scan, testSalt, testTime)
	if err != nil {
		t.Fatal(err)
	}
	out, err := graph.Encode(graph.Build(f, graph.Provenance{EngineVersion: "x", RulesVersion: "0", KnowledgeVersion: graph.KnowledgeVersion, HostID: "unset"}, testTime))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"HOOK-MARKER", "STATUS-MARKER", "HELPER-MARKER", "echo ", "ENVENVENV"} {
		if strings.Contains(string(out), marker) {
			t.Errorf("graph contains %q", marker)
		}
	}
	var execs int
	for _, n := range f.Nodes {
		if n.Type == graph.ResourceAgentExecutable {
			execs++
		}
	}
	if execs != 4 {
		t.Errorf("agent_executable: got %d, want 4", execs)
	}
}

func TestNew_NoManagedSourcesOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("managed sources exist on Windows")
	}
	newIdentityEnv(t)
	scan, err := windows.New().ListClaudeSettings(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Managed) != 0 || scan.ManagedMCP != nil || scan.LegacyManagedPath != "" {
		t.Errorf("managed sources off Windows: %+v", scan)
	}
}
