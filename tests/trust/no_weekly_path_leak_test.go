package trust_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// BV-158 part 1/5: the space-view's weekly field carries the automatic review's
// state, day and time, but NEVER the path of the other copy of rosmarops.exe
// that runs it. With a weekly task of another copy at a known path, neither the
// path nor any 8-character fragment of its directory appears in the sealed
// package content.
func TestNoWeeklyPathInSealedPackage(t *testing.T) {
	const otherPath = `C:\Users\distinctuser7\Downloads\zqvigilbackup9\rosmarops.exe`
	salt := []byte("0123456789abcdef0123456789abcdef")
	home, repo := `C:\Users\distinctuser7`, `C:\Users\distinctuser7\code\shop`
	day := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

	claudeDir := home + `\.claude`
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: claudeDir + `\settings.json`, ConfigDir: claudeDir,
		Status: platform.MCPSourceRead,
	}}}
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{}, salt, day)
	if err != nil {
		t.Fatal(err)
	}
	g, err := pipeline.BuildGraph(frag, pipeline.GraphOpts{Provenance: graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, RepoRoots: []string{repo}, Home: home, Now: day})
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: []string{repo},
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: day,
		Weekly: scheduledtasks.Weekly{State: scheduledtasks.WeeklyOther, Other: otherPath},
	})

	sv, err := report.BuildSpaceView(v, &g, home, "0.7.4")
	if err != nil {
		t.Fatal(err)
	}
	// The weekly field must be present and say "other", with no path fields.
	if sv.Weekly == nil || sv.Weekly.State != "other" {
		t.Fatalf("weekly = %+v, want state other", sv.Weekly)
	}
	js, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatal(err)
	}
	s := string(js)
	if strings.Contains(s, otherPath) {
		t.Error("the sealed package contains the other copy's full path")
	}
	// No 8-character fragment of the path's directory (the identifying part;
	// the generic "rosmarops.exe" filename is left out).
	prefix := otherPath[:strings.LastIndex(otherPath, `\`)+1]
	for i := 0; i+8 <= len(prefix); i++ {
		if frag := prefix[i : i+8]; strings.Contains(s, frag) {
			t.Errorf("the sealed package contains an 8-char path fragment %q", frag)
		}
	}
}
