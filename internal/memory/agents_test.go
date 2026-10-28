package memory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// seen is a run's agents: Claude Code and the MCP servers named.
func seen(mcps ...string) []SeenAgent {
	out := []SeenAgent{{Key: "agent_client_cc", Name: "Claude Code"}}
	for _, m := range mcps {
		out = append(out, SeenAgent{Key: "mcp\x00claude-code\x00" + m + "\x00user", MCP: true, Name: m})
	}
	return out
}

func runSeen(day string, s []SeenAgent) Run {
	r := run(day)
	r.Seen = s
	return r
}

// Decision 2026-10-26 (agentes y MCP nuevos, A1): a first run has no base
// and no «new» line; a server added in run 2 is one line, gone in run 3.
func TestNewAgents_AgainstLastChecked(t *testing.T) {
	m, _, root := Update(File{}, runSeen("2026-10-01", seen("fs")), nil, salt)
	if root.NewAgents != nil || root.NewMCP != nil {
		t.Fatalf("run 1: %v %v", root.NewAgents, root.NewMCP)
	}
	if m.LastChecked == nil || m.LastChecked.AgentRefs == nil || len(*m.LastChecked.AgentRefs) != 2 {
		t.Fatalf("run 1 base: %+v", m.LastChecked)
	}
	for _, r := range *m.LastChecked.AgentRefs {
		if strings.Contains(r, "fs") || strings.Contains(r, "Claude") || len(r) != 16 {
			t.Errorf("agent ref %q is not a salted reference", r)
		}
	}
	m, _, root = Update(m, runSeen("2026-10-02", seen("fs", "browser")), nil, salt)
	wantNew := []MCPRef{{Name: "browser", Key: "mcp\x00claude-code\x00browser\x00user"}}
	if root.NewAgents != nil || !reflect.DeepEqual(root.NewMCP, wantNew) {
		t.Fatalf("run 2: %v %v", root.NewAgents, root.NewMCP)
	}
	_, _, root = Update(m, runSeen("2026-10-03", seen("fs", "browser")), nil, salt)
	if root.NewAgents != nil || root.NewMCP != nil {
		t.Fatalf("run 3: %v %v", root.NewAgents, root.NewMCP)
	}
}

// A2: a v1 findings-memory.json reads without error and gives no «new»
// (it has no agent_refs); it is written back at the current version.
func TestNewAgents_V1MemoryHasNoBase(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"schema_version":1,"runs":2,"last_checked":{"run":2,"date":"2026-10-25","rules_version":"1","knowledge_version":"k","agents":["Claude Code"],"repos":10},"entries":[]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, reset, err := Read(dir)
	if err != nil || reset || prev.SchemaVersion != 1 || prev.LastChecked == nil || prev.LastChecked.AgentRefs != nil {
		t.Fatalf("read v1: %+v %v %v", prev, reset, err)
	}
	// An incomplete run keeps the v1 base: still no base for new agents.
	inc := runSeen("2026-10-26", seen("fs"))
	inc.Complete = false
	next, _, root := Update(prev, inc, nil, salt)
	if root.NewAgents != nil || root.NewMCP != nil || root.SaltChanged {
		t.Fatalf("v1: %v %v %v", root.NewAgents, root.NewMCP, root.SaltChanged)
	}
	if err := Write(dir, next); err != nil {
		t.Fatal(err)
	}
	back, _, err := Read(dir)
	if err != nil || back.SchemaVersion != SchemaVersion || back.SaltRef != SaltRef(salt) || back.LastChecked.AgentRefs != nil {
		t.Fatalf("written: %+v %v", back, err)
	}
	if _, _, root = Update(back, runSeen("2026-10-27", seen("fs", "browser")), nil, salt); root.NewMCP != nil {
		t.Errorf("a carried-over v1 base gives new lines: %v", root.NewMCP)
	}
}

// A1, «complete»: a run with a failed source is not last_checked, and the
// next one does not take that source's servers for new.
func TestNewAgents_IncompleteRunIsNotTheBase(t *testing.T) {
	m, _, _ := Update(File{}, runSeen("2026-10-01", seen("fs", "browser")), nil, salt)
	failed := runSeen("2026-10-02", seen("fs")) // browser's file could not be read
	failed.Complete = false
	m, _, _ = Update(m, failed, nil, salt)
	if m.LastChecked.Run != 1 {
		t.Fatalf("an incomplete run became the base: %+v", m.LastChecked)
	}
	if _, _, root := Update(m, runSeen("2026-10-03", seen("fs", "browser")), nil, salt); root.NewMCP != nil {
		t.Errorf("browser is new after a failed read: %v", root.NewMCP)
	}
}

// A2: another salt makes no «new» line and the findings not comparable,
// with the reason salt_changed (A8, BV-60).
func TestNewAgents_SaltChanged(t *testing.T) {
	m, _, _ := Update(File{}, runSeen("2026-10-01", seen("fs")), []Finding{fnd("f_a"), fnd("f_gone")}, salt)
	other := []byte("fedcba9876543210fedcba9876543210")
	_, v, root := Update(m, runSeen("2026-10-02", seen("fs", "browser")), []Finding{fnd("f_a")}, other)
	if !root.SaltChanged || root.Reason != ReasonSaltChanged || root.NewMCP != nil || root.NewAgents != nil {
		t.Fatalf("salt changed: %+v", root)
	}
	if v["f_a"].Verdict != VerdictNotComparable || root.Counts[VerdictNotComparable] != 3 || root.Counts[VerdictNew] != 0 || len(root.NoLongerObserved) != 0 {
		t.Errorf("findings: %+v %+v", v, root.Counts)
	}
}

// A2: an agent_refs with a name or a path in clear does not validate.
func TestSchemaV2_AgentRefsRefsOnly(t *testing.T) {
	doc := func(refs string) []byte {
		return []byte(`{"schema_version":2,"salt_ref":"0123456789abcdef","runs":1,"last_checked":{"run":1,"date":"2026-10-26","rules_version":"1","knowledge_version":"k","agents":["Claude Code"],"repos":1,"agent_refs":[` + refs + `]},"entries":[]}`)
	}
	if err := validate(doc(`"0123456789abcdef"`)); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for _, bad := range []string{`"Claude Code"`, `"C:/Users/x/.claude.json"`, `"0123456789ABCDEF"`} {
		if validate(doc(bad)) == nil {
			t.Errorf("agent_refs %s validates", bad)
		}
	}
}
