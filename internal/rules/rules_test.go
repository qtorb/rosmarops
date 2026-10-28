package rules_test

import (
	"encoding/base64"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// The scenario has the shape of the owner's machine (status-actual §1, run
// 21450b6): Claude Code with user settings and no defaultMode (the built-in
// auto), Cursor's ~/.cursor/mcp.json, CLAUDE.md in three repositories, two
// Supabase keys in webapp/dashboard/.env.local (secret on line 2,
// service_role on line 4), an OpenAI key, and a database URL only in a
// template (.env.example).
const (
	home      = "/home/albert"
	claudeDir = home + "/.claude"
	userJSON  = claudeDir + "/settings.json"
	cursorDir = home + "/.cursor"
	cursorMCP = cursorDir + "/mcp.json"
	repoWeb   = "/src/webapp"
	repoWeb2  = "/src/webapp-copy"
	repoIF    = "/src/api-backend"
	repoNotes = "/src/notes"
	dashEnv   = repoWeb + "/dashboard/.env.local"
)

var (
	salt = []byte("0123456789abcdef0123456789abcdef")
	now  = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
)

func seg(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func jwt(role string) string {
	return seg(`{"alg":"HS256","typ":"JWT"}`) + "." +
		seg(`{"iss":"supabase","ref":"abcdefghijklmnopqrst","role":"`+role+`"}`) + ".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"
}

var (
	serviceRole = jwt("service_role")
	anonKey     = jwt("anon")
	sbSecret    = "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8)
	openAIKey   = "sk-proj-" + strings.Repeat("O", 48)
	dbURL       = "postgres://app:apppass@db.example.com:5432/app"
)

type entry struct {
	path  string
	line  int
	key   string
	value string
}

var defaultEntries = []entry{
	{dashEnv, 2, "SUPABASE_SECRET_KEY", sbSecret},
	{dashEnv, 4, "SUPABASE_SERVICE_ROLE_KEY", serviceRole},
	{repoNotes + "/.env", 1, "OPENAI_API_KEY", openAIKey},
	{repoIF + "/.env.example", 10, "DATABASE_URL", dbURL},
	{repoNotes + "/.env", 2, "SUPABASE_ANON_KEY", anonKey},
}

// scenario builds and infers the graph with perms as the user settings.
func scenario(t *testing.T, perms platform.ClaudePermissions, entries []entry) graph.Graph {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range entries {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	return scenarioWith(t, perms, cf)
}

// scenarioWith is scenario with a credential files scan.
func scenarioWith(t *testing.T, perms platform.ClaudePermissions, cf platform.CredentialFileScan) graph.Graph {
	t.Helper()
	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: cursorMCP, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: userJSON, ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: perms,
	}}}
	f, _, err := graph.FromScans(mcp, settings, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	var files []platform.InstructionFile
	for _, r := range []string{repoWeb, repoIF, repoNotes} {
		files = append(files, platform.InstructionFile{Product: "claude-code", ConfigDir: claudeDir, Path: r + "/CLAUDE.md"})
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: files}, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, now),
		[]string{repoWeb, repoIF, repoNotes, repoWeb2}, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	return claudeCodeOnly(g)
}

// claudeCodeOnly drops the edges Cursor's inference added (BV-48): these
// scenarios pin Claude Code's findings, which Cursor does not change. Cursor
// stays as a target; its own findings have their tests (cursor_test.go).
func claudeCodeOnly(g graph.Graph) graph.Graph {
	cursor := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Platform == knowledge.CursorProduct {
			cursor[n.ID] = true
		}
	}
	edges := g.Edges[:0:0]
	for _, e := range g.Edges {
		if cursor[e.From] && (e.Type == graph.EdgeReaches || e.Type == graph.EdgeWritesConfigOf) {
			continue
		}
		edges = append(edges, e)
	}
	g.Edges = edges
	return g
}

func evaluate(t *testing.T, g graph.Graph) rules.Result {
	t.Helper()
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: g, Base: base,
		RepoRoots: []string{repoWeb, repoIF, repoNotes, repoWeb2},
		Families:  map[string]string{repoWeb: repoWeb, repoWeb2: repoWeb},
	})
}

// summary is "rule severity confidence radius scope/target count".
func summary(f rules.Finding) string {
	what := f.Params.Scope
	if f.Params.TargetAgent != "" {
		what = f.Params.TargetAgent
	}
	if what == "" {
		what = f.Params.Service + f.Params.ResourceType
	}
	return strings.Join([]string{f.Rule, f.Severity, f.Confidence, f.Radius, what}, " ")
}

func findingBy(t *testing.T, res rules.Result, id string) rules.Finding {
	t.Helper()
	for _, f := range res.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no finding %s", id)
	return rules.Finding{}
}

func actionBy(t *testing.T, f rules.Finding, id string) rules.Action {
	t.Helper()
	for _, a := range f.Actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("finding %s has no action %s", f.ID, id)
	return rules.Action{}
}

// The real case: the built-in auto mode is the way out, the Bash read-only
// commands read from any project.
func TestEvaluate_RealShape(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}}, defaultEntries))
	var got []string
	for _, f := range res.Findings {
		got = append(got, summary(f))
	}
	want := []string{
		// The two Supabase keys of dashboard/.env.local are one finding
		// (BV-39); its label is one of the two high keys.
		"",
		"foreign_agent_config_writable red medium  Cursor",
		"own_instructions_writable yellow medium  agent_instructions",
		"cred_read_and_send yellow medium any_project openai",
	}
	if len(got) != len(want) || !strings.HasPrefix(got[0], "cred_read_and_send red medium any_project ") || !reflect.DeepEqual(got[1:], want[1:]) {
		t.Fatalf("findings:\n%s", strings.Join(got, "\n"))
	}

	// Posture: the anon key (low, readable) and the template database URL.
	posture := map[string]map[string]int{}
	for _, p := range res.Posture {
		posture[p.Code] = p.ByClass
	}
	if posture[rules.PostureReadableWithoutPrompt][rules.ClassLow] != 1 || posture[rules.PostureInTemplatesOnly][rules.ClassHigh] != 1 {
		t.Errorf("posture %+v", res.Posture)
	}

	// Today: the first finding, block_reads_outside (A1: 2 points / 2 =
	// ask_read_rule's 1 / 1; it touches the two any_project credential
	// findings, ask_read_rule one), with what remains.
	today := findingBy(t, res, res.Today.FindingID)
	if today.ID != res.Findings[0].ID {
		t.Errorf("today is not the first finding")
	}
	act := actionBy(t, today, res.Today.ActionID)
	if act.Code != rules.ActionBlockReadsOutside || act.Completeness != rules.CompletenessRadiusOnly || act.Magnitude != 2 {
		t.Errorf("today's action %+v", act)
	}
	if act.File != userJSON || !act.FileExists || act.Key != "permissions.blockReadsOutsideWorkingDirectories" ||
		act.Value != "{\n  \"permissions\": {\n    \"blockReadsOutsideWorkingDirectories\": true\n  }\n}" {
		t.Errorf("today's action target %+v", act)
	}
	if res.Today.Remains == nil || res.Today.Remains.Radius != rules.RadiusSameProject || len(res.Today.Remains.Ways) == 0 {
		t.Errorf("remains %+v", res.Today.Remains)
	}

	// The service_role finding: one location, its actions in magnitude order.
	sr := res.Findings[0]
	if !reflect.DeepEqual(sr.Evidence.Locations, []string{dashEnv + ":2", dashEnv + ":4"}) || sr.Group.Count != 2 || sr.Params.Count != 2 ||
		sr.Group.Members[0].Family != repoWeb || sr.Params.Class != rules.ClassHigh {
		t.Errorf("Supabase evidence %+v group %+v params %+v", sr.Evidence, sr.Group, sr.Params)
	}
	var codes []string
	for _, a := range sr.Actions {
		codes = append(codes, a.Code+"/"+a.Completeness)
	}
	if !reflect.DeepEqual(codes, []string{"ask_read_rule/partial", "block_reads_outside/radius_only", "manual_default_mode/full", "remove_from_disk/full"}) {
		t.Errorf("service_role actions %v", codes)
	}
	for _, a := range sr.Actions {
		if a.Code == rules.ActionAskReadRule && !reflect.DeepEqual(a.Rules, []string{"Read(//src/webapp/dashboard/.env.local)"}) {
			t.Errorf("ask rule %v", a.Rules)
		}
	}
	for _, a := range []string{graph.AssumeAutoClassifierPermits, graph.AssumeBashToolAvailable} {
		if !contains(sr.Evidence.Assumptions, a) {
			t.Errorf("assumptions %v lack %s", sr.Evidence.Assumptions, a)
		}
	}

	// The CLAUDE.md finding groups the three files.
	var own rules.Finding
	for _, f := range res.Findings {
		if f.Rule == rules.RuleOwnInstructionsWritable {
			own = f
		}
	}
	if own.Group.Count != 3 || own.Params.Count != 3 || len(own.Group.Members) != 3 {
		t.Errorf("own_instructions_writable group %+v", own.Group)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Ids are f_ + sha16(rule, key) with no version; stable across runs.
func TestEvaluate_StableIDs(t *testing.T) {
	g := scenario(t, platform.ClaudePermissions{}, defaultEntries)
	a, b := evaluate(t, g), evaluate(t, g)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two evaluations of one graph differ")
	}
	var client, cred graph.Node
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Platform == "claude-code" {
			client = n
		}
		if n.Kind == graph.KindCredential && strings.HasPrefix(n.Locations[0], dashEnv+":4") {
			cred = n
		}
	}
	// The key since BV-39: agent + service + files (the label does not
	// enter it).
	_ = cred
	want := "f_" + graph.KeyHash(rules.RuleCredReadAndSend, client.ID, "supabase", dashEnv)
	for _, f := range a.Findings {
		if f.Params.Scope == "service_role" && f.ID != want {
			t.Errorf("id %s, want %s", f.ID, want)
		}
		if !strings.HasPrefix(f.ID, "f_") || len(f.ID) != 18 {
			t.Errorf("id %q", f.ID)
		}
	}
}

// A2: a credential only in templates is posture; one also in a real file is
// evaluated as usual, with both locations.
func TestEvaluate_TemplatesOnly(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{}, []entry{
		{repoIF + "/.env.example", 10, "DATABASE_URL", dbURL},
		{repoIF + "/config.sample.example", 3, "DATABASE_URL", "postgres://other:otherpass@db2.example.com:5432/x"},
	}))
	for _, f := range res.Findings {
		if f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable {
			t.Errorf("template credential is a finding: %s", summary(f))
		}
	}
	if res.Posture[1].Code != rules.PostureInTemplatesOnly || res.Posture[1].Count != 2 {
		t.Errorf("posture %+v", res.Posture)
	}

	res = evaluate(t, scenario(t, platform.ClaudePermissions{}, []entry{
		{repoIF + "/.env.example", 10, "DATABASE_URL", dbURL},
		{repoIF + "/.env", 1, "DATABASE_URL", dbURL},
	}))
	var n int
	for _, f := range res.Findings {
		if f.Rule == rules.RuleCredReadAndSend {
			n++
			if len(f.Evidence.Locations) != 2 || f.Severity != rules.SeverityRed {
				t.Errorf("mixed credential %s %v", summary(f), f.Evidence.Locations)
			}
		}
	}
	if n != 1 {
		t.Errorf("mixed credential findings: %d", n)
	}
}

// No way out (Manual mode, no shell "*"): a high credential is
// cred_readable yellow; medium and low go to posture.
func TestEvaluate_NoEgress(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{DefaultMode: "default"}, defaultEntries))
	var got []string
	for _, f := range res.Findings {
		got = append(got, summary(f))
	}
	sort.Strings(got)
	if len(got) != 1 || !strings.HasPrefix(got[0], "cred_readable yellow high any_project ") {
		t.Errorf("findings:\n%s", strings.Join(got, "\n"))
	}
	if res.Posture[0].ByClass[rules.ClassMedium] != 1 || res.Posture[0].ByClass[rules.ClassLow] != 1 {
		t.Errorf("posture %+v", res.Posture)
	}
}

// The ways out (§0): bypassPermissions, a shell "*" allow, a launcher rule;
// a deny without specifier overrides the allow; WebFetch and ask are none.
func TestEvaluate_Egress(t *testing.T) {
	cases := []struct {
		name  string
		perms platform.ClaudePermissions
		out   bool
		conf  string
	}{
		{"manual", platform.ClaudePermissions{DefaultMode: "default"}, false, ""},
		{"bypass", platform.ClaudePermissions{DefaultMode: "bypassPermissions"}, true, rules.ConfidenceMedium},
		{"shell all", platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash"}}, true, rules.ConfidenceHigh},
		{"launcher", platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(node -e ' *)"}}, true, rules.ConfidenceHigh},
		{"launcher denied", platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(node -e ' *)"}, Deny: []string{"Bash"}}, false, ""},
		{"not a launcher", platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(npx eslint *)"}}, false, ""},
		{"webfetch and ask", platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"WebFetch"}, Ask: []string{"Bash"}}, false, ""},
	}
	for _, c := range cases {
		res := evaluate(t, scenario(t, c.perms, []entry{{dashEnv, 4, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}}))
		var f rules.Finding
		for _, x := range res.Findings {
			if x.Params.Scope == "service_role" {
				f = x
			}
		}
		if got := f.Rule == rules.RuleCredReadAndSend; got != c.out {
			t.Errorf("%s: rule %q, want egress %v", c.name, f.Rule, c.out)
			continue
		}
		if c.out && f.Confidence != c.conf {
			t.Errorf("%s: confidence %s, want %s (%v)", c.name, f.Confidence, c.conf, f.Evidence.Assumptions)
		}
		launcher := contains(f.Evidence.Assumptions, rules.AssumeCodeLauncherRule)
		if launcher != (c.name == "launcher") {
			t.Errorf("%s: code_launcher_rule %v", c.name, launcher)
		}
		if c.name == "launcher" && (len(res.Launchers) != 1 || res.Launchers[0].Rule != "Bash(node -e ' *)") {
			t.Errorf("launchers %+v", res.Launchers)
		}
	}
}

// Radius (§1 b): blockReadsOutsideWorkingDirectories leaves same_project,
// and then block_reads_outside does not apply.
func TestEvaluate_Radius(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{BlockReadsOutsideWorkingDirectories: true}, []entry{{dashEnv, 4, "K", serviceRole}}))
	f := res.Findings[0]
	if f.Radius != rules.RadiusSameProject {
		t.Errorf("radius %s", f.Radius)
	}
	for _, a := range f.Actions {
		if a.Code == rules.ActionBlockReadsOutside {
			t.Error("block_reads_outside offered while in force")
		}
	}
}

// The class table (§1).
func TestEvaluate_Classes(t *testing.T) {
	cases := []struct {
		value string
		rule  string
	}{
		{"AKIAABCDEFGHIJ123456", rules.RuleCredReadAndSend + " red"},                     // AWS: high
		{"sk_live_" + strings.Repeat("L", 24), rules.RuleCredReadAndSend + " red"},       // Stripe secret: high
		{dbURL, rules.RuleCredReadAndSend + " red"},                                      // db credential: high
		{openAIKey, rules.RuleCredReadAndSend + " yellow"},                               // model key: medium
		{"ghp_" + strings.Repeat("G", 36), rules.RuleCredReadAndSend + " yellow"},        // unclassified: medium
		{"sb_publishable_" + strings.Repeat("P", 22) + "_" + strings.Repeat("Q", 8), ""}, // low: never
		{anonKey, ""},
	}
	for _, c := range cases {
		res := evaluate(t, scenario(t, platform.ClaudePermissions{}, []entry{{repoNotes + "/.env", 1, "DATABASE_URL", c.value}}))
		got := ""
		for _, f := range res.Findings {
			if f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable {
				got = f.Rule + " " + f.Severity
			}
		}
		if got != c.rule {
			t.Errorf("%.12s…: got %q, want %q", c.value, got, c.rule)
		}
	}
}

// bypassPermissions writes Claude Code's own protected config: own_config_writable
// red; acceptEdits writes the instructions inside the working roots only.
func TestEvaluate_WriteRules(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{DefaultMode: "bypassPermissions"}, nil))
	rulesOf := map[string]string{}
	for _, f := range res.Findings {
		rulesOf[f.Rule] = f.Severity
	}
	if rulesOf[rules.RuleOwnConfigWritable] != rules.SeverityRed || rulesOf[rules.RuleForeignAgentConfigWritable] != rules.SeverityRed ||
		rulesOf[rules.RuleOwnInstructionsWritable] != rules.SeverityYellow {
		t.Errorf("bypass rules %v", rulesOf)
	}
	if res.Findings[0].Rule != rules.RuleForeignAgentConfigWritable || res.Findings[1].Rule != rules.RuleOwnConfigWritable {
		t.Errorf("order: %s, %s", res.Findings[0].Rule, res.Findings[1].Rule)
	}

	// Edit allow on the CLAUDE.md files only: ask_edit_rule is complete.
	res = evaluate(t, scenario(t, platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Edit(//src/**/CLAUDE.md)"}}, nil))
	if len(res.Findings) != 1 || res.Findings[0].Rule != rules.RuleOwnInstructionsWritable {
		t.Fatalf("edit allow findings %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Confidence != rules.ConfidenceHigh || len(f.Actions) != 1 || f.Actions[0].Code != rules.ActionAskEditRule || f.Actions[0].Completeness != rules.CompletenessFull {
		t.Errorf("edit allow finding %s actions %+v", summary(f), f.Actions)
	}
	if res.Today.ActionID != f.Actions[0].ID || res.Today.Remains != nil {
		t.Errorf("today %+v", res.Today)
	}
}

func TestEvaluate_NothingUrgent(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{DefaultMode: "default"}, []entry{{repoNotes + "/.env", 1, "K", openAIKey}}))
	if len(res.Findings) != 0 || res.Today.Code != rules.TodayNothingUrgent || res.Today.FindingID != "" {
		t.Errorf("findings %d, today %+v", len(res.Findings), res.Today)
	}
}

// Members are grouped by family: two clones of webapp are one family.
func TestEvaluate_GroupByFamily(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{DefaultMode: "default"}, []entry{
		{dashEnv, 4, "K", serviceRole},
		{repoWeb2 + "/dashboard/.env.local", 4, "K", serviceRole},
		{home + "/.supabase/service-role", 1, "K", serviceRole},
	}))
	g := res.Findings[0].Group
	if g.Count != 3 || len(g.Members) != 2 || g.Members[0].Family != repoWeb || g.Members[0].Count != 2 || g.Members[1].Family != "" {
		t.Errorf("group %+v", g)
	}
}

// BV-39: credentials of one agent, one service and one set of files are one
// finding: the Railway CLI's accessToken and refreshToken in
// ~/.railway/config.json. Another file, or another service, stays apart.
func TestEvaluate_MergeSameFiles(t *testing.T) {
	railway := home + "/.railway/config.json"
	mcp := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: railway, Pointer: "/user/accessToken", Key: "accessToken", Kind: "railway_cli_access_token", Value: secrets.NewSecretValue("rwac" + strings.Repeat("A", 32))},
		{Path: railway, Pointer: "/user/refreshToken", Key: "refreshToken", Kind: "railway_cli_refresh_token", Value: secrets.NewSecretValue("rwrf" + strings.Repeat("R", 32))},
		{Path: repoNotes + "/.env", Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue(openAIKey)},
		{Path: repoNotes + "/.env", Line: 2, Key: "OPENROUTER_API_KEY", Value: secrets.NewSecretValue("sk-or-v1-" + strings.Repeat("b", 64))},
		{Path: repoIF + "/.env", Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + strings.Repeat("P", 48))},
	}}
	res := evaluate(t, scenarioWith(t, platform.ClaudePermissions{}, mcp))
	var got []string
	for _, f := range res.Findings {
		if f.Rule == rules.RuleCredReadAndSend {
			got = append(got, f.Params.Service+" "+strings.Join(f.Evidence.Locations, ","))
		}
	}
	sort.Strings(got)
	want := []string{
		"openai " + repoIF + "/.env:1",
		"openai " + repoNotes + "/.env:1",
		"openrouter " + repoNotes + "/.env:2",
		"railway " + railway + "#/user/accessToken," + railway + "#/user/refreshToken",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings:\n%s", strings.Join(got, "\n"))
	}
	for _, f := range res.Findings {
		if f.Params.Service == "railway" {
			// The highest class wins the label: both unclassified, so the
			// lowest credential id; the id is keyed by agent, service, files.
			client := f.Evidence.Nodes[0]
			if f.ID != "f_"+graph.KeyHash(rules.RuleCredReadAndSend, client, "railway", railway) || f.Group.Count != 2 {
				t.Errorf("railway finding %s %+v", f.ID, f.Group)
			}
		}
	}
}

// BV-40, decision índice de postura agéntica §1.4: the realistic scenario
// scores 27 (P = 30 + 15 + 12 + 4 = 61, band exposed); today's action
// (block_reads_outside) takes both credential findings to same_project
// (P = 47): 30. Each finding's "without" is the score with it fixed.
func TestIndex_RealShape(t *testing.T) {
	g := scenario(t, platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}}, defaultEntries)
	res := evaluate(t, g)
	var agents []index.Agent
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			agents = append(agents, index.Agent{ID: n.ID, Name: n.Name, Product: n.Platform, ConfigDir: n.ConfigPath})
		}
	}
	medium := map[string]int{}
	for a, c := range res.MediumReadable {
		medium[a] = len(c)
	}
	idx := index.Compute(index.Input{
		GraphBuilt: true, Agents: agents, Inference: map[string]bool{"claude-code": true},
		Findings: res.Findings, Today: res.Today, MediumReadable: medium, Repos: 4,
		RulesVersion: rules.Version, KnowledgeVersion: knowledge.Version,
	})
	if len(idx.Agents) != 1 {
		t.Fatalf("agents %+v", idx.Agents)
	}
	a := idx.Agents[0]
	if a.Value == nil || *a.Value != 27 || a.Band != index.BandExposed {
		t.Fatalf("score %v %s, want 27 exposed", a.Value, a.Band)
	}
	if a.Projection == nil || a.Projection.Value != 30 || a.Projection.ActionID != res.Today.ActionID {
		t.Errorf("projection %+v, want 30", a.Projection)
	}
	without := map[string]string{}
	for _, w := range a.Weights {
		f := findingBy(t, res, w.FindingID)
		without[summary(f)[:strings.Index(summary(f)+" ", " ")]+" "+f.Params.Service+f.Params.TargetAgent+f.Params.ResourceType] = strconv.FormatFloat(w.Weight, 'f', -1, 64) + "→" + strconv.Itoa(w.Without)
	}
	want := map[string]string{
		"cred_read_and_send supabase":                      "30→35",
		"foreign_agent_config_writable Cursoragent_config": "15→31",
		"cred_read_and_send openai":                        "12→30",
		"own_instructions_writable agent_instructions":     "4→28",
	}
	if !reflect.DeepEqual(without, want) {
		t.Errorf("weights %v, want %v", without, want)
	}
	// Cursor is present and not evaluated: no machine score.
	if idx.Machine.Value != nil || idx.Machine.Unavailable != index.ReasonAgentsNotEvaluated ||
		!reflect.DeepEqual(idx.Scope.NotEvaluated, []string{"Cursor"}) {
		t.Errorf("machine %+v scope %+v", idx.Machine, idx.Scope)
	}
}

// M12: the title of a merged credential finding comes from class, then
// privilege level, then authenticates_to observed over inferred, service,
// scope and id: of the two Supabase keys of dashboard/.env.local, the
// service_role JWT (observed) wins over sb_secret_ (inferred); one more.
func TestEvaluate_TitleOrder(t *testing.T) {
	res := evaluate(t, scenario(t, platform.ClaudePermissions{}, defaultEntries))
	f := res.Findings[0]
	if f.Params.Service != "supabase" || f.Params.Scope != "service_role" || f.OthersCount() != 1 || len(f.Evidence.Locations) != 2 {
		t.Errorf("title %s/%s, others %d, locations %v", f.Params.Service, f.Params.Scope, f.OthersCount(), f.Evidence.Locations)
	}
}

// BV-94b, 4 (blindaje B7): a Read(//…) value is never cut or altered. A
// file whose path untrusted.PlainText would change (a backtick, a control
// character, more than 200 characters) gets no rule; the finding keeps
// every location and the rule of the clean file is still proposed.
func TestAskReadRule_PathNotPlainNotProposed(t *testing.T) {
	tick := repoWeb + "/a`b/.env.local"
	long := repoWeb + "/" + strings.Repeat("d", 210) + "/.env.local"
	g := scenario(t, platform.ClaudePermissions{}, []entry{
		{dashEnv, 4, "SUPABASE_SERVICE_ROLE_KEY", serviceRole},
		{tick, 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole},
		{long, 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole},
	})
	res := evaluate(t, g)
	var found bool
	for _, f := range res.Findings {
		if !contains(f.Evidence.Locations, tick+":1") {
			continue
		}
		found = true
		if !contains(f.Evidence.Locations, long+":1") || !contains(f.Evidence.Locations, dashEnv+":4") {
			t.Errorf("locations %v", f.Evidence.Locations)
		}
		asked := false
		for _, a := range f.Actions {
			if a.Code != rules.ActionAskReadRule {
				continue
			}
			asked = true
			if !reflect.DeepEqual(a.Rules, []string{"Read(/" + dashEnv + ")"}) || a.Completeness != rules.CompletenessPartial {
				t.Errorf("ask rule %v (%s): only the clean file's", a.Rules, a.Completeness)
			}
		}
		if !asked {
			t.Error("no ask_read_rule action: the clean file's rule must still be proposed")
		}
	}
	if !found {
		t.Fatal("no finding with the backtick path — test would be vacuous")
	}
}
