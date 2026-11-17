package trust_test

// BV-252 (CTO-63 §4, Albert 17-11): WSL and containers in the report.
//   - Windows: a fixed coverage line, coverage.windows.wsl_containers_not_read,
//     in the report and in the console; it reads nothing new.
//   - Linux in WSL (hostenv): the reads row of an agent that does not keep its
//     reads out of /mnt carries wsl_windows_drive_readable, with no change of
//     score or class.
//
// The environment comes from hostenv.DetectLinux over a fake file system.

import (
	"encoding/json"
	"html"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/hostenv"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// bv252WSL is a WSL2 distribution as /proc/version shows it, with C: mounted.
var bv252WSL = fstest.MapFS{
	"proc/version":        {Data: []byte("Linux version 5.15.167.4-microsoft-standard-WSL2 (root@build) #1 SMP\n")},
	"proc/self/mountinfo": {Data: []byte("60 1 8:48 / / rw,relatime - ext4 /dev/sdd rw\n80 60 0:60 / /mnt/c rw,noatime - 9p C:\\134 rw,aname=drvfs\n")},
}

// bv252Linux is a plain Linux: no container, WSL or virtual machine signal.
var bv252Linux = fstest.MapFS{
	"proc/version":        {Data: []byte("Linux version 6.8.0-49-generic (buildd@lcy02) #49-Ubuntu SMP\n")},
	"proc/self/mountinfo": {Data: []byte("29 1 259:2 / / rw,relatime - ext4 /dev/nvme0n1p2 rw\n")},
}

// bv252Claude is Claude Code alone on R2 with the user settings changed by
// edit (nil: product defaults), in the environment env, on Linux.
func bv252Claude(t *testing.T, env *hostenv.Env, profile string, edit func(*platform.ClaudeSandbox, *platform.ClaudePermissions)) report.V2 {
	t.Helper()
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	in := pipeline.Input{
		Settings: settings, CredFiles: platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		RepoRoots:    []string{fcRepo},
	}
	if profile != "" {
		p, ok := pipeline.Profile(in, profile)
		if !ok {
			t.Fatalf("%s not applied", profile)
		}
		in = p
	}
	if edit != nil {
		for i := range in.Settings.Files {
			if in.Settings.Files[i].Scope == platform.MCPScopeUser {
				edit(&in.Settings.Files[i].Permissions.Sandbox, &in.Settings.Files[i].Permissions)
			}
		}
	}
	g := r2Assemble(t, in)
	s := in.Settings
	return r2BuildV2(t, &g, &s, func(v *report.V2Input) { v.Environment = env; v.HostOS = graph.HostOS })
}

// readsCarries reports whether the product's reads row carries the WSL
// assumption.
func readsCarries(t *testing.T, v report.V2, product string) bool {
	t.Helper()
	for _, c := range overviewByProduct(v, product).Rows {
		if c.Row == rules.RowReadsFiles {
			return slices.Contains(c.Data.Assumptions, rules.AssumeWSLWindowsDriveReadable)
		}
	}
	t.Fatalf("%s: no reads row", product)
	return false
}

func TestBV252_WSLWindowsDrive(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	graph.HostOS = "linux"

	wsl, plain := hostenv.DetectLinux(bv252WSL), hostenv.DetectLinux(bv252Linux)
	if wsl == nil || wsl.Kind != hostenv.KindWSL {
		t.Fatalf("fake WSL file system detected as %+v, want wsl", wsl)
	}
	if plain != nil {
		t.Fatalf("fake Linux file system detected as %+v, want nothing", plain)
	}
	noBlock := func(_ *platform.ClaudeSandbox, p *platform.ClaudePermissions) {
		p.BlockReadsOutsideWorkingDirectories = false
	}
	mntDenied := func(s *platform.ClaudeSandbox, p *platform.ClaudePermissions) {
		noBlock(s, p)
		s.DenyRead = append(s.DenyRead, "/mnt/")
	}
	p2Mnt := func(s *platform.ClaudeSandbox, _ *platform.ClaudePermissions) {
		s.DenyRead = append(s.DenyRead, "/mnt/")
	}
	cases := []struct {
		name    string
		env     *hostenv.Env
		profile string
		edit    func(*platform.ClaudeSandbox, *platform.ClaudePermissions)
		want    bool
	}{
		{"WSL, no sandbox", wsl, "", nil, true},
		{"WSL, P2 + /mnt/ in denyRead", wsl, profiles.P2, p2Mnt, false},
		{"WSL, P2 alone (sandbox + blockReads)", wsl, profiles.P2, nil, false},
		{"WSL, P2 without blockReads", wsl, profiles.P2, noBlock, true},
		{"WSL, P2 without blockReads, /mnt/ in denyRead", wsl, profiles.P2, mntDenied, false},
		{"Linux without WSL, no sandbox", plain, "", nil, false},
	}
	for _, c := range cases {
		v := bv252Claude(t, c.env, c.profile, c.edit)
		if got := readsCarries(t, v, knowledge.ClaudeCodeProduct); got != c.want {
			t.Errorf("%s: reads row carries wsl_windows_drive_readable = %v, want %v", c.name, got, c.want)
		}
		// Only the reads row.
		for _, r := range overviewByProduct(v, knowledge.ClaudeCodeProduct).Rows {
			if r.Row != rules.RowReadsFiles && slices.Contains(r.Data.Assumptions, rules.AssumeWSLWindowsDriveReadable) {
				t.Errorf("%s: row %s carries the WSL assumption", c.name, r.Row)
			}
		}
		// No change of score or class against the same computer outside WSL.
		base := bv252Claude(t, nil, c.profile, c.edit)
		s1, b1, _ := r2AgentIndexID(t, v, knowledge.ClaudeCodeProduct)
		s0, b0, _ := r2AgentIndexID(t, base, knowledge.ClaudeCodeProduct)
		if s1 != s0 || b1 != b0 {
			t.Errorf("%s: score %d %s in WSL, %d %s outside", c.name, s1, b1, s0, b0)
		}
		if c1, c0 := classOf(t, v), classOf(t, base); c1 != c0 {
			t.Errorf("%s: class %q in WSL, %q outside", c.name, c1, c0)
		}
		if slices.Contains(v.Coverage.Notes, report.CoverageNoteWSLContainersNotRead) {
			t.Errorf("%s: the Windows line on Linux", c.name)
		}
	}

	// The text, in both languages, in the HTML window of assumptions.
	v := bv252Claude(t, wsl, "", nil)
	for _, lang := range []string{"en", "es"} {
		page, cat := bv252HTML(t, v, lang)
		if want := esc(cat.Text("unknown."+rules.AssumeWSLWindowsDriveReadable, nil)); !strings.Contains(page, want) {
			t.Errorf("%s: HTML lacks %q", lang, want)
		}
	}
}

// classOf is the Claude Code class letter of v's index, as text.
func classOf(t *testing.T, v report.V2) string {
	t.Helper()
	_, _, id := r2AgentIndexID(t, v, knowledge.ClaudeCodeProduct)
	for _, a := range v.Index.Agents {
		if a.AgentID == id {
			b, _ := json.Marshal(a.Letter)
			return string(b)
		}
	}
	return ""
}

// bv252HTML is the HTML report of v in lang.
func bv252HTML(t *testing.T, v report.V2, lang string) (string, catalog.Catalog) {
	t.Helper()
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	return report.RenderHTML(v, cat, fcHome, time.UTC), cat
}

// esc is a text as the HTML writes it.
func esc(s string) string { return html.EscapeString(s) }

func TestBV252_WindowsLine(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	for _, goos := range []string{"windows", "linux", "darwin"} {
		graph.HostOS = goos
		v := bv252Claude(t, nil, "", nil)
		has := slices.Contains(v.Coverage.Notes, report.CoverageNoteWSLContainersNotRead)
		if has != (goos == "windows") {
			t.Errorf("%s: coverage line present = %v", goos, has)
		}
		for _, lang := range []string{"en", "es"} {
			page, cat := bv252HTML(t, v, lang)
			line := cat.Text(report.CoverageNoteWSLContainersNotRead, nil)
			console := report.ConsoleSummary(v, cat, "report.html")
			if strings.Contains(console, line) != (goos == "windows") {
				t.Errorf("%s %s: console line present = %v:\n%s", goos, lang, !has, console)
			}
			if strings.Contains(page, esc(line)) != (goos == "windows") {
				t.Errorf("%s %s: HTML line present = %v", goos, lang, !has)
			}
			if goos == "windows" && readsCarries(t, v, knowledge.ClaudeCodeProduct) {
				t.Errorf("windows: reads row carries the WSL assumption")
			}
		}
	}
}
