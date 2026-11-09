package graph

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// TestClineConfiguredCapabilitiesHaveRule (BV-202 C5, CTO-28): the 5 nodes that
// failed the schema on Albert's machine were Cline's auto-approval capabilities
// — basis=configured with no rule. Whether put in force by autoApprovalSettings
// or by the legacy yolo toggle, each configured capability now carries a rule
// naming the literal globalState.json setting (no secret) and a Source pointer,
// so the encoded graph validates. Without the fix Encode fails the node oneOf.
func TestClineConfiguredCapabilitiesHaveRule(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	salt := []byte("0123456789abcdef0123456789abcdef")
	dir := "/h/.cline/data"
	prov := Provenance{EngineVersion: "t", RulesVersion: RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: HostID(salt)}

	cases := []struct {
		name     string
		scan     platform.ClineScan
		wantRule map[string]string // capability type -> exact rule
	}{
		{
			name: "autoApprovalSettings",
			scan: platform.ClineScan{
				ConfigDir: dir, Present: true, GlobalStateStatus: platform.MCPSourceRead,
				HasAutoApproval: true, ReadFiles: true, EditFiles: false,
				ExecuteSafeCommands: true, UseBrowser: false, UseMcp: true,
			},
			wantRule: map[string]string{
				CapReadFiles:     "autoApprovalSettings.actions.readFiles: true",
				CapWriteFiles:    "autoApprovalSettings.actions.editFiles: false",
				CapExecuteShell:  "autoApprovalSettings.actions.executeSafeCommands: true",
				CapNetwork:       "autoApprovalSettings.actions.useBrowser: false",
				CapInvokeMCPTool: "autoApprovalSettings.actions.useMcp: true",
			},
		},
		{
			name: "yolo",
			scan: platform.ClineScan{
				ConfigDir: dir, Present: true, GlobalStateStatus: platform.MCPSourceRead,
				Yolo: true, YoloKey: "yoloModeToggled",
			},
			wantRule: map[string]string{
				CapReadFiles:     "yoloModeToggled: true",
				CapWriteFiles:    "yoloModeToggled: true",
				CapExecuteShell:  "yoloModeToggled: true",
				CapNetwork:       "yoloModeToggled: true",
				CapInvokeMCPTool: "yoloModeToggled: true",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := Build(FromCline(tc.scan, true, now), prov, now)
			out, err := Encode(g)
			if err != nil {
				t.Fatalf("configured Cline graph fails schema validation: %v", err)
			}
			got := map[string]Node{}
			for _, n := range g.Nodes {
				if n.Kind == KindCapability {
					if n.Basis != BasisConfigured {
						t.Errorf("capability %s basis=%q, want configured", n.Type, n.Basis)
					}
					if n.Rule == "" {
						t.Errorf("capability %s has no rule (the 0.10.1 bug)", n.Type)
					}
					got[n.Type] = n
				}
			}
			for typ, want := range tc.wantRule {
				n, ok := got[typ]
				if !ok {
					t.Errorf("no %s capability", typ)
					continue
				}
				if n.Rule != want {
					t.Errorf("%s rule = %q, want %q", typ, n.Rule, want)
				}
				if !strings.HasPrefix(n.Source, dir+"/globalState.json#/") {
					t.Errorf("%s source = %q, want a globalState.json pointer", typ, n.Source)
				}
			}
			// No read secret can appear (cline config carries none here), and the
			// graph is value-clean by construction; sanity: the encoded form holds
			// the rule text we expect for read files.
			if !strings.Contains(string(out), tc.wantRule[CapReadFiles]) {
				t.Errorf("encoded graph missing rule %q", tc.wantRule[CapReadFiles])
			}
		})
	}
}
