package knowledge_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// cursorSources are the sources of the Cursor decision (2026-10-25, §8): every
// entry and mitigation cites one of them.
var cursorSources = map[string]bool{
	"https://cursor.com/docs/agent/security/run-modes.md":  true,
	"https://cursor.com/docs/agent/security.md":            true,
	"https://cursor.com/docs/reference/sandbox.md":         true,
	"https://cursor.com/docs/reference/permissions.md":     true,
	"https://cursor.com/docs/cli/reference/permissions.md": true,
	"https://cursor.com/docs/context/mcp":                  true,
	"https://cursor.com/docs/hooks":                        true,
	"https://cursor.com/docs/context/ignore-files":         true,
	"https://cursor.com/docs/context/rules":                true,
	// [R8] as annex 3 point 2 cites it.
	"https://cursor.com/docs/reference/ignore-file": true,
}

// K1, K2: the Cursor base decodes strictly, every entry and mitigation has
// a source of §8 and a verified note, the capabilities use the schema's
// enums, and Claude Code's launcher table is reused.
func TestCursor_LoadsStrictlyWithSources(t *testing.T) {
	b, err := knowledge.Cursor()
	if err != nil {
		t.Fatalf("Cursor: %v", err)
	}
	if b.Product != knowledge.CursorProduct || b.Version != knowledge.CursorVersion {
		t.Errorf("product/version %q/%q", b.Product, b.Version)
	}
	types, approvals := capabilityEnums(t)
	want := []string{"file_tools_read", "default_ignore_file_tools", "file_tools_write_workspace", "sandbox_protected_paths", "sandbox_defaults", "run_mode_builtin", "terminal_default", "mcp_default", "network_default"}
	var got []string
	for _, e := range b.Entries {
		got = append(got, e.ID)
		if !cursorSources[e.SourceURL] || e.Verified == "" {
			t.Errorf("entry %s: source %q", e.ID, e.SourceURL)
		}
		if !types[e.Type] || !approvals[e.Approval] {
			t.Errorf("entry %s: type/approval %s/%s", e.ID, e.Type, e.Approval)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries %v, want %v", got, want)
	}
	if e, _ := b.Entry("run_mode_builtin"); e.Type != "permission_mode" || e.TargetScope != "auto_review" || e.Approval != "allow" {
		t.Errorf("run mode %+v (K4: Auto-review)", e)
	}
	if e, _ := b.Entry("sandbox_defaults"); e.Type != "read_files" || e.TargetScope != "sandbox_read_boundary_system" {
		t.Errorf("sandbox defaults %+v (annex 2 point 4)", e)
	}
	if e, _ := b.Entry("default_ignore_file_tools"); e.IsCapability() || strings.Join(e.Paths, " ") != ".env* .git/" {
		t.Errorf("ignore list %+v (annex 3 point 2)", e)
	}
	if e, _ := b.Entry("sandbox_protected_paths"); e.IsCapability() || len(e.Paths) != 11 {
		t.Errorf("protected paths %+v", e)
	}
	var ms []string
	for _, m := range b.Mitigations {
		ms = append(ms, m.ID)
		if !cursorSources[m.SourceURL] || m.Verified == "" {
			t.Errorf("mitigation %s: source %q", m.ID, m.SourceURL)
		}
	}
	if !reflect.DeepEqual(ms, []string{"sandbox_read_boundary_workspace", "remove_from_disk"}) {
		t.Errorf("mitigations %v (K2: no sandbox_network_own_list)", ms)
	}
	m, _ := b.Mitigation("sandbox_read_boundary_workspace")
	if m.File != "~/.cursor/sandbox.json" || m.Setting != "readBoundary" || m.Value != `"workspace"` || m.Completeness != "radius_only" {
		t.Errorf("read boundary %+v", m)
	}
	cc, _ := knowledge.ClaudeCode()
	if !reflect.DeepEqual(b.CodeLaunchers, cc.CodeLaunchers) || len(b.CodeLaunchers) == 0 {
		t.Error("Cursor does not reuse the signed launcher table")
	}
}

// Annex 2 point 2: the closed command lists, lowercase.
func TestCursor_CommandLists(t *testing.T) {
	b, err := knowledge.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	names := func(cs []knowledge.CommandClass) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Program)
		}
		return strings.Join(out, " ")
	}
	if got := names(b.ReadCommands); got != "cat type more less head tail grep rg findstr awk sed get-content gc select-string sls" {
		t.Errorf("read commands %q", got)
	}
	if got := names(b.NetworkCommands); got != "curl wget invoke-webrequest iwr invoke-restmethod irm scp sftp ftp nc ncat ssh rsync" {
		t.Errorf("network commands %q", got)
	}
	all, err := knowledge.All()
	if err != nil || len(all) != 2 || all["cursor"].Product != "cursor" || all["claude-code"].Product != "claude-code" {
		t.Errorf("All: %v %v", all, err)
	}
}
