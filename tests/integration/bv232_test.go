package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-232 §7: for each point, one configuration with the change and one without
// it; the score and the graph's ways move in the expected direction. A small
// machine: a home folder with ~/.aws/credentials (outside the project) and one
// project with a .env (inside it).
const (
	b232Home    = "/home/u"
	b232Repo    = "/home/u/src/app"
	b232AWS     = "/home/u/.aws/credentials"
	b232Env     = "/home/u/src/app/.env"
	b232Claude  = "/home/u/.claude"
	b232Grok    = "/home/u/.grok"
	b232UserCfg = "/home/u/.claude/settings.json"
	b232ProjCfg = "/home/u/src/app/.claude/settings.json"
)

var b232Now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// b232Machine is the machine's input: Claude Code with the given settings
// files (and, when grok is set, Grok Build with that user layer), the two
// credentials, and the listed VS Code extensions.
type b232Machine struct {
	files []platform.ClaudeSettingsFile
	grok  *platform.GrokLayer
	exts  []platform.VSCodeExtension
	roots []string
}

func b232User(p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: b232UserCfg, ConfigDir: b232Claude, Status: platform.MCPSourceRead, Permissions: p}
}

func b232Project(p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{Scope: platform.MCPScopeProject, ScopePath: b232Repo, ConfigPath: b232ProjCfg, ConfigDir: b232Claude, Status: platform.MCPSourceRead, Permissions: p}
}

// b232Result is one machine's assembled graph and report.
type b232Result struct {
	g  graph.Graph
	v2 report.V2
}

func (m b232Machine) run(t *testing.T, hostOS string) b232Result {
	t.Helper()
	prev := graph.HostOS
	graph.HostOS = hostOS
	t.Cleanup(func() { graph.HostOS = prev })
	roots := m.roots
	if roots == nil {
		roots = []string{b232Repo}
	}
	var trust []platform.ProjectTrust
	for _, r := range roots {
		trust = append(trust, platform.ProjectTrust{Path: r, Accepted: true})
	}
	settings := platform.ClaudeSettingsScan{Files: m.files, Trust: trust}
	creds := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: b232AWS, Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
		{Path: b232Env, Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + repeat("Q", 48))},
	}}
	var grok platform.GrokScan
	if m.grok != nil {
		grok = platform.GrokScan{ConfigDir: b232Grok, Exists: true, Layers: []platform.GrokLayer{*m.grok}}
	}
	in := pipeline.Input{
		Settings: settings, Grok: grok, CredFiles: creds, Extensions: m.exts,
		Provenance: graph.Provenance{EngineVersion: "0.11.7-test", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(fmSalt)},
		Salt:       fmSalt, RepoRoots: roots, Home: b232Home, Now: b232Now,
	}
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	g := res.Graph
	v1 := report.Build("0.11.7-test", nil, nil, len(roots), 0, 0, 0, nil)
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: graphSHA(res.Encoded), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: roots, V1: v1, GeneratedAt: b232Now, Salt: fmSalt, ClaudeSettings: &settings, Home: b232Home, Grok: grok,
	})
	return b232Result{g: g, v2: v2}
}

// score is the score of product's agent.
func (r b232Result) score(t *testing.T, product string) int {
	t.Helper()
	id := ""
	for _, o := range r.v2.AgentsOverview {
		if o.Product == product {
			id = o.AgentID
		}
	}
	for _, a := range r.v2.Index.Agents {
		if a.AgentID == id && a.Value != nil {
			return *a.Value
		}
	}
	t.Fatalf("no score for %s", product)
	return 0
}

// reachVias are the capability rules (or knowledge-base entries) through which
// product's client reaches the credential in file.
func (r b232Result) reachVias(product, file string) []string {
	nodes := map[string]graph.Node{}
	for _, n := range r.g.Nodes {
		nodes[n.ID] = n
	}
	var out []string
	for _, e := range r.g.Edges {
		if e.Type != graph.EdgeReaches || nodes[e.From].Platform != product {
			continue
		}
		cred := nodes[e.To]
		if len(cred.Locations) == 0 || !strings.HasPrefix(cred.Locations[0], file) {
			continue
		}
		c := nodes[e.Via]
		name := c.Rule
		if name == "" {
			name = c.Source
		}
		out = append(out, name+" "+strings.Join(e.Assumptions, ","))
	}
	return out
}

func (r b232Result) rules(agentProduct string) map[string]int {
	nodes := map[string]graph.Node{}
	for _, n := range r.g.Nodes {
		nodes[n.ID] = n
	}
	out := map[string]int{}
	for _, f := range r.v2.Findings {
		if len(f.Evidence.Nodes) > 0 && nodes[f.Evidence.Nodes[0]].Platform == agentProduct {
			out[f.Rule]++
		}
	}
	return out
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

var b232Sandbox = platform.ClaudeSandbox{
	Enabled: true, EnabledSet: true, FailIfUnavailable: true, FailIfUnavailableSet: true,
	AllowUnsandboxedCommands: false, AllowUnsandboxedCommandsSet: true,
	DenyRead: []string{"~/.aws"},
}

// §1, Claude Code: an effective sandbox with ~/.aws in denyRead cuts the
// terminal's read of ~/.aws/credentials on Linux, and the score rises; on
// native Windows the same settings change nothing.
func TestBV232_ClaudeSandbox(t *testing.T) {
	without := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{})}}
	with := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{Sandbox: b232Sandbox})}}

	linuxOff, linuxOn := without.run(t, "linux"), with.run(t, "linux")
	if v := linuxOff.reachVias("claude-code", b232AWS); len(v) == 0 {
		t.Fatal("without sandbox: no way reaches ~/.aws/credentials")
	}
	if v := linuxOn.reachVias("claude-code", b232AWS); len(v) != 0 {
		t.Errorf("Linux, effective sandbox: ~/.aws/credentials still reached: %v", v)
	}
	if a, b := linuxOff.score(t, "claude-code"), linuxOn.score(t, "claude-code"); b <= a {
		t.Errorf("Linux: score with sandbox %d, without %d: want higher", b, a)
	}
	winOff, winOn := without.run(t, "windows"), with.run(t, "windows")
	if a, b := winOff.score(t, "claude-code"), winOn.score(t, "claude-code"); a != b {
		t.Errorf("Windows: sandbox moved the score %d -> %d; it does not exist there", a, b)
	}
	if v := winOn.reachVias("claude-code", b232AWS); len(v) == 0 {
		t.Error("Windows: the sandbox cut a way; it does not exist there")
	}
	// Without failIfUnavailable the sandbox is not taken for sure.
	unsure := b232Sandbox
	unsure.FailIfUnavailable, unsure.FailIfUnavailableSet = false, false
	m := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{Sandbox: unsure})}}
	if a, b := linuxOff.score(t, "claude-code"), m.run(t, "linux").score(t, "claude-code"); a != b {
		t.Errorf("sandbox without failIfUnavailable moved the score %d -> %d", a, b)
	}
}

// §1, Grok Build: the strict profile limits reads to the project on Linux; on
// Windows it changes nothing.
func TestBV232_GrokSandbox(t *testing.T) {
	layer := func(profile string) *platform.GrokLayer {
		return &platform.GrokLayer{Kind: platform.GrokLayerUser, Path: b232Grok + "/config.toml", Status: platform.MCPSourceRead, SandboxProfile: profile}
	}
	base := []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{})}
	off := b232Machine{files: base, grok: layer("")}
	on := b232Machine{files: base, grok: layer("strict")}
	lOff, lOn := off.run(t, "linux"), on.run(t, "linux")
	if len(lOff.reachVias("grok-build", b232AWS)) == 0 || len(lOn.reachVias("grok-build", b232AWS)) != 0 {
		t.Errorf("Linux strict: ~/.aws reach %v -> %v", lOff.reachVias("grok-build", b232AWS), lOn.reachVias("grok-build", b232AWS))
	}
	if len(lOn.reachVias("grok-build", b232Env)) == 0 {
		t.Error("Linux strict: the project's .env is no longer reached; strict reads the CWD")
	}
	if a, b := lOff.score(t, "grok-build"), lOn.score(t, "grok-build"); b <= a {
		t.Errorf("Linux: Grok score with strict %d, without %d: want higher", b, a)
	}
	if a, b := off.run(t, "windows").score(t, "grok-build"), on.run(t, "windows").score(t, "grok-build"); a != b {
		t.Errorf("Windows: Grok profile moved the score %d -> %d", a, b)
	}
}

// §2: a Read deny cuts the terminal's recognized commands; a program the agent
// launches still reads the file on Windows, even with a sandbox configured,
// and only an effective sandbox closes it.
func TestBV232_ReadDenyAndTerminal(t *testing.T) {
	deny := []string{"Read(~/.aws/**)"}
	plain := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default"})}}
	denied := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default", Deny: deny})}}
	a, b := plain.run(t, "windows"), denied.run(t, "windows")
	if !anyContains(a.reachVias("claude-code", b232AWS), "") {
		t.Fatal("without deny: ~/.aws/credentials not reached")
	}
	if v := b.reachVias("claude-code", b232AWS); len(v) != 0 {
		t.Errorf("deny, default mode: the terminal's way stays open: %v", v)
	}
	if sa, sb := a.score(t, "claude-code"), b.score(t, "claude-code"); sb <= sa {
		t.Errorf("deny: score %d, without %d: want higher", sb, sa)
	}
	// A program the agent launches without a person (a Bash allow) still
	// reads it on Windows, with the sandbox configured: read_deny_partial.
	prog := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default", Deny: deny, Allow: []string{"Bash"}, Sandbox: b232Sandbox})}}
	if v := prog.run(t, "windows").reachVias("claude-code", b232AWS); !anyContains(v, graph.AssumeReadDenyPartial) {
		t.Errorf("Windows, sandbox configured: the launched program's way is closed: %v", v)
	}
	// On Linux the effective sandbox (Read deny rules merged in) closes it.
	if v := prog.run(t, "linux").reachVias("claude-code", b232AWS); len(v) != 0 {
		t.Errorf("Linux, effective sandbox: the launched program's way stays open: %v", v)
	}
}

// §3: Bash(curl *) and Bash(wget *) in allow are egress: a readable key
// becomes one that can be sent out, and the score falls.
func TestBV232_CurlWgetEgress(t *testing.T) {
	plain := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default"})}}
	a := plain.run(t, "windows")
	for _, rule := range []string{"Bash(curl *)", "Bash(wget *)"} {
		m := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default", Allow: []string{rule}})}}
		b := m.run(t, "windows")
		if a.rules("claude-code")["cred_read_and_send"] != 0 || b.rules("claude-code")["cred_read_and_send"] == 0 {
			t.Errorf("%s: cred_read_and_send %d -> %d", rule, a.rules("claude-code")["cred_read_and_send"], b.rules("claude-code")["cred_read_and_send"])
		}
		if sa, sb := a.score(t, "claude-code"), b.score(t, "claude-code"); sb >= sa {
			t.Errorf("%s: score %d, without %d: want lower", rule, sb, sa)
		}
	}
	// A specific destination is not "any argument".
	m := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(curl https://api.example.com/*)"}})}}
	if n := m.run(t, "windows").rules("claude-code")["cred_read_and_send"]; n != 0 {
		t.Errorf("curl to one destination counted as egress: %d", n)
	}
}

// §4: a project's plan wins over the user's auto in that project's sessions;
// with the Claude Code extension for VS Code installed, the less restrictive
// (the user's auto) counts.
func TestBV232_ProjectMode(t *testing.T) {
	user := b232User(platform.ClaudePermissions{DefaultMode: "auto"})
	plain := b232Machine{files: []platform.ClaudeSettingsFile{user}}
	plan := b232Machine{files: []platform.ClaudeSettingsFile{user, b232Project(platform.ClaudePermissions{DefaultMode: "plan"})}}
	ext := plan
	ext.exts = []platform.VSCodeExtension{{ID: "anthropic.claude-code"}}

	a, b, c := plain.run(t, "windows"), plan.run(t, "windows"), ext.run(t, "windows")
	if !anyContains(a.reachVias("claude-code", b232Env), "defaultMode=auto") || anyContains(b.reachVias("claude-code", b232Env), "defaultMode=auto") {
		t.Errorf("project plan: auto way to the project's .env %v -> %v", a.reachVias("claude-code", b232Env), b.reachVias("claude-code", b232Env))
	}
	sa, sb, sc := a.score(t, "claude-code"), b.score(t, "claude-code"), c.score(t, "claude-code")
	if sb <= sa {
		t.Errorf("project plan: score %d, user auto alone %d: want higher", sb, sa)
	}
	if sc != sa {
		t.Errorf("plan + VS Code extension: score %d, want the user's auto %d", sc, sa)
	}
	// acceptEdits from the project writes inside it; auto from the project has
	// no effect.
	autoProj := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default"}), b232Project(platform.ClaudePermissions{DefaultMode: "auto"})}}
	def := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "default"})}}
	if x, y := autoProj.run(t, "windows").score(t, "claude-code"), def.run(t, "windows").score(t, "claude-code"); x != y {
		t.Errorf("project auto took effect: %d vs %d", x, y)
	}
}

// §5: ~/.claude/settings.local.json counts only for sessions in the home
// folder: with the home folder not a project it changes nothing; when it is
// one, its rules apply.
func TestBV232_HomeLocalSettings(t *testing.T) {
	homeLocal := platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeLocal, ScopePath: b232Home, ConfigPath: b232Home + "/.claude/settings.local.json", ConfigDir: b232Claude,
		Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Bash"}},
	}
	user := b232User(platform.ClaudePermissions{DefaultMode: "default"})
	notProject := homeLocal
	notProject.HomeLocalOnly = true
	a := b232Machine{files: []platform.ClaudeSettingsFile{user}}.run(t, "windows")
	b := b232Machine{files: []platform.ClaudeSettingsFile{user, notProject}}.run(t, "windows")
	if b.rules("claude-code")["cred_read_and_send"] != a.rules("claude-code")["cred_read_and_send"] {
		t.Errorf("home settings.local.json (home not a project) changed the findings: %v -> %v", a.rules("claude-code"), b.rules("claude-code"))
	}
	roots := []string{b232Repo, b232Home}
	c := b232Machine{files: []platform.ClaudeSettingsFile{user}, roots: roots}.run(t, "windows")
	d := b232Machine{files: []platform.ClaudeSettingsFile{user, homeLocal}, roots: roots}.run(t, "windows")
	if c.rules("claude-code")["cred_read_and_send"] != 0 || d.rules("claude-code")["cred_read_and_send"] == 0 {
		t.Errorf("home a project: its local Bash allow is egress: %v -> %v", c.rules("claude-code"), d.rules("claude-code"))
	}
	if sc, sd := c.score(t, "claude-code"), d.score(t, "claude-code"); sd >= sc {
		t.Errorf("home a project: score with its local Bash allow %d, without %d: want lower", sd, sc)
	}
}

// §6, §7: the pages of a machine with one project in one folder, as each of the
// three systems shows them, say every count of one in its form for one.
func TestBV232_OneProjectPages(t *testing.T) {
	m := b232Machine{files: []platform.ClaudeSettingsFile{b232User(platform.ClaudePermissions{DefaultMode: "auto"})}}
	dir := t.TempDir()
	for _, goos := range []string{"windows", "darwin", "linux"} {
		r := m.run(t, goos)
		r.v2.OS = goos
		r.v2.Coverage.Projects = &report.ProjectsCoverage{
			Search: "complete", Found: 1, Scanned: 1, Roots: []string{b232Home}, New: 1,
			NotSearched: []report.ProjectNotSearched{{Reason: "hidden", Count: 1}},
		}
		for _, lang := range []string{"en", "es"} {
			out := filepath.Join(dir, goos, lang)
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := report.WriteHTMLPages(r.v2, lang, b232Home, nil, out); err != nil {
				t.Fatal(err)
			}
			checkPagesShown(t, goos+"/"+lang, out)
		}
	}
}
