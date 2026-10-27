package trust_test

// Trust test: secrets read from MCP server env blocks, settings files and
// instruction files never reach the v2 graph file, and the state file holds
// the local salt but no secret value.
//
// Unlike no_full_secret_leak_test.go, this drives the real production path:
// config files on disk → windows adapter (ListMCPServers / ReadMCPFile) →
// graph.FromMCPScan → graph.WriteFile (Encode), and state.SaveState.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/state"
)

// fakeSupabaseJWT is a fake Supabase service_role key: a JWT whose payload
// names the issuer and project. The extra claim (email) must never reach
// the graph.
var fakeSupabaseJWT = jwtSegment(`{"alg":"HS256","typ":"JWT"}`) + "." +
	jwtSegment(`{"iss":"supabase","ref":"abcdefghijklmnopqrst","role":"service_role","email":"owner@example.com"}`) +
	".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"

func jwtSegment(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// fakeHeaderSecret sits in the headers of a remote (type http) server in
// ~/.claude.json; fakeAccountEmail is account data in the same file that is
// not MCP configuration. Neither may reach the graph.
// fakeRuleToken is a fake GitHub token written inside a permission rule of a
// repository's .claude/settings.local.json. The rule becomes a capability;
// the token must not reach the graph.
const fakeRuleToken = "ghp_RULE" + "ZYXWVUTSRQPONMLKJIHGFEDCBA98765432"

// fakeSettingsEnvToken sits in the env block of ~/.claude/settings.json and
// fakeHookToken inside a hook command of the same file. The env value becomes
// a credential fingerprint; the hook command text is never kept. Neither
// value may reach the graph.
// fakeNpmrcToken (npm_), fakeHostsToken (gho_) and fakeGhuToken (ghu_) sit
// in ~/.npmrc and GitHub CLI's hosts.yml (credential files collector, BV-32;
// patterns BV-34).
const (
	fakeNpmrcToken = "npm_NPMR" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeHostsToken = "gho_HOST" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeGhuToken   = "ghu_UTOS" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
)

// fakeRailwayToken, fakeRailwayAccess and fakeRailwayRefresh sit under
// user in the Railway CLI's ~/.railway/config.json (BV-36): credentials by
// key, matched by no secret pattern.
var (
	fakeRailwayToken   = "rwtk" + strings.Repeat("G", 28) + "GRPH"
	fakeRailwayAccess  = "rwac" + strings.Repeat("H", 28) + "GRPH"
	fakeRailwayRefresh = "rwrf" + strings.Repeat("J", 28) + "GRPH"
)

// fakeCursorAllowToken sits inside a terminalAllowlist entry of
// ~/.cursor/permissions.json, and fakeCursorEnvFileToken in the envFile of
// a repository's .cursor/mcp.json (BV-48). Neither may reach the graph.
const (
	fakeCursorAllowToken   = "ghp_CALW" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeCursorEnvFileToken = "ghp_CENV" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
)

// The agents detected and not evaluated (BV-88): Codex's auth.json, Hermes
// Agent's .env and a profile's, Antigravity's MCP env and headers (global
// and project), and Claude Desktop's MCP env.
var (
	fakeCodexSecrets = map[string]string{
		"/OPENAI_API_KEY":        "sk-proj-CODX" + strings.Repeat("K", 40) + "CODX",
		"/tokens/access_token":   "eyJhbGciOiJSUzI1NiJ9.CODXACCESS" + strings.Repeat("a", 30) + ".sigCODX",
		"/tokens/refresh_token":  "rt_CODX" + strings.Repeat("r", 40) + "CODX",
		"/tokens/id_token":       "eyJhbGciOiJSUzI1NiJ9.CODXIDTOKN" + strings.Repeat("i", 30) + ".sigCODX",
		"/personal_access_token": "pat_CODX" + strings.Repeat("p", 40) + "CODX",
	}
	fakeHermesKey        = "sk-ant-api03-" + strings.Repeat("H", 93) + "-" + strings.Repeat("E", 10) + "AA"
	fakeHermesProfileKey = "sk-ant-api03-" + strings.Repeat("P", 93) + "-" + strings.Repeat("F", 10) + "AA"
	fakeAgyEnvToken      = "ghp_AGEN" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeAgyHeaderToken   = "ghp_AGHD" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeAgyProjectToken  = "ghp_AGPR" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeDesktopToken     = "ghp_CDSK" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
)

// fakeInstructionToken is written in a repository's CLAUDE.md. The file
// becomes an agent_instructions resource; its content is never read.
const fakeInstructionToken = "ghp_INST" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"

// fakeCommittedToken is committed in the repository's current tree, and
// fakeHistoryToken only in a past commit (CTO-7 §2.2): the repository scan
// keeps their salted fingerprints, never the values.
const (
	fakeCommittedToken = "ghp_GITT" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeHistoryToken   = "ghp_GITH" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
)

const (
	fakeSettingsEnvToken = "ghp_SENV" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
	fakeHookToken        = "ghp_HOOK" + "ZYXWVUTSRQPONMLKJIHGFEDCBA987654"
)

const (
	fakeHeaderSecret = "github_pat_11HEADER" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	fakeAccountEmail = "trust.fake.owner@example.com"
)

// Obviously fake values, each matching a different built-in pattern.
var fakeEnvSecrets = map[string]string{
	"PRIVATE_KEY":               "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ\n-----END PRIVATE KEY-----",
	"RAILWAY_TOKEN":             "rw" + strings.Repeat("Q1w2E3r4T5", 4),
	"SUPABASE_SERVICE_ROLE_KEY": fakeSupabaseJWT,
	"ANTHROPIC_API_KEY":         fakeAnthropicKey,
	"GITHUB_TOKEN":              "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
	"AWS_ACCESS_KEY_ID":         "AKIAABCDEFGHIJ123456",
	"STRIPE_KEY":                "sk_live_ABCDEFGHIJKLMNOPQRSTUVWx",
	"OPENAI_API_KEY":            "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuv",
	"SLACK_BOT_TOKEN":           "xoxb-12345678-87654321-ABCDEFGHIJKLMNOPQRSTUVWX",
}

func writeMCPConfig(t *testing.T, path string, servers map[string]map[string]string) {
	t.Helper()
	cfg := map[string]any{"mcpServers": map[string]any{}}
	for name, env := range servers {
		cfg["mcpServers"].(map[string]any)[name] = map[string]any{"command": "npx", "env": env}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// assertNoSecret fails if out contains any fake secret in full or its middle
// section (value[4:len-4]), or the salt.
func assertNoSecret(t *testing.T, what string, out []byte, salt []byte) {
	t.Helper()
	s := string(out)
	for key, v := range fakeEnvSecrets {
		if strings.Contains(s, v) {
			t.Errorf("%s contains the full value of %s", what, key)
		}
		if mid := v[4 : len(v)-4]; strings.Contains(s, mid) {
			t.Errorf("%s contains the middle of %s", what, key)
		}
	}
	if payload := strings.Split(fakeSupabaseJWT, ".")[1]; strings.Contains(s, payload) {
		t.Errorf("%s contains the JWT payload segment", what)
	}
	if strings.Contains(s, "owner@example.com") {
		t.Errorf("%s contains a JWT claim other than iss/ref/role", what)
	}
	for _, v := range []string{fakeHeaderSecret, fakeAccountEmail} {
		if strings.Contains(s, v) {
			t.Errorf("%s contains %s…", what, v[:12])
		}
	}
	if mid := fakeHeaderSecret[4 : len(fakeHeaderSecret)-4]; strings.Contains(s, mid) {
		t.Errorf("%s contains the middle of the header secret", what)
	}
	for _, v := range []string{fakeRailwayToken, fakeRailwayAccess, fakeRailwayRefresh} {
		if strings.Contains(s, v[4:len(v)-4]) {
			t.Errorf("%s contains a Railway CLI token", what)
		}
	}
	if salt != nil && strings.Contains(s, hex.EncodeToString(salt)) {
		t.Errorf("%s contains the local salt", what)
	}
}

func TestNoSecretInGraph(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	// Cursor: every fake secret in one server, plus a non-secret.
	cursorEnv := map[string]string{"LOG_LEVEL": "debug"}
	for k, v := range fakeEnvSecrets {
		cursorEnv[k] = v
	}
	writeMCPConfig(t, filepath.Join(home, ".cursor", "mcp.json"), map[string]map[string]string{"tools": cursorEnv})
	// Claude Code: the GitHub token again, in another client, plus a settings
	// env block and a hook whose command carries a token.
	userSettings, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{"github": map[string]any{"command": "npx", "env": map[string]string{"GH_TOKEN": fakeEnvSecrets["GITHUB_TOKEN"]}}},
		"env":        map[string]string{"GITHUB_TOKEN": fakeSettingsEnvToken},
		"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{
			map[string]any{"type": "command", "command": `curl -H "Authorization: Bearer ` + fakeHookToken + `" https://hooks.example.invalid`},
		}}}},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), userSettings, 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	// ~/.claude.json: a remote server with a secret in its headers, plus
	// account data that is not MCP configuration.
	claudeJSON := `{"oauthAccount": {"emailAddress": "` + fakeAccountEmail + `"},
	  "mcpServers": {"remote": {"type": "http", "url": "https://mcp.example.com/mcp",
	                            "headers": {"Authorization": "Bearer ` + fakeHeaderSecret + `"}}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(claudeJSON), 0o600); err != nil {
		t.Fatalf("write ~/.claude.json: %v", err)
	}

	// A repository whose local settings allow a curl call with a hardcoded
	// bearer token.
	repo := filepath.Join(home, "src", "app")
	rule := `Bash(curl -H "Authorization: Bearer ` + fakeRuleToken + `" *)`
	rawRule, err := json.Marshal(map[string]any{"permissions": map[string]any{"allow": []string{rule}}})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "settings.local.json"), rawRule, 0o600); err != nil {
		t.Fatalf("write settings.local.json: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("Deploy with "+fakeInstructionToken+"\n"), 0o600); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	// Credential files (BV-32): the repository's .env, ~/.npmrc and GitHub
	// CLI's hosts.yml.
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SUPABASE_SERVICE_ROLE_KEY="+fakeSupabaseJWT+"\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("//registry.npmjs.org/:_authToken="+fakeNpmrcToken+"\n"), 0o600); err != nil {
		t.Fatalf("write .npmrc: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "gh", "hosts.yml"), []byte("github.com:\n    oauth_token: "+fakeHostsToken+"\nghe.example.com:\n    oauth_token: "+fakeGhuToken+"\n"), 0o600); err != nil {
		t.Fatalf("write hosts.yml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".railway"), 0o700); err != nil {
		t.Fatal(err)
	}
	railwayConfig := `{"projects": {}, "user": {"token": "` + fakeRailwayToken + `", "accessToken": "` + fakeRailwayAccess + `", "refreshToken": "` + fakeRailwayRefresh + `"}}`
	if err := os.WriteFile(filepath.Join(home, ".railway", "config.json"), []byte(railwayConfig), 0o600); err != nil {
		t.Fatalf("write railway config.json: %v", err)
	}
	// Cursor (BV-48): a permissions.json (JSONC) whose terminalAllowlist
	// entry carries a token, and a repository mcp.json whose server reads an
	// envFile with a token and names a ${env:...} variable.
	cursorPerms := "// allowlist\n{\"terminalAllowlist\": [\"curl -H \\\"Authorization: Bearer " + fakeCursorAllowToken + "\\\"\", \"cat\",]}"
	if err := os.WriteFile(filepath.Join(home, ".cursor", "permissions.json"), []byte(cursorPerms), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".cursor"), 0o700); err != nil {
		t.Fatal(err)
	}
	repoMCP := `{"mcpServers": {"api": {"command": "npx", "envFile": "${workspaceFolder}/.cursor/mcp.env", "env": {"GITHUB_TOKEN": "${env:GITHUB_TOKEN}"}}}}`
	if err := os.WriteFile(filepath.Join(repo, ".cursor", "mcp.json"), []byte(repoMCP), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".cursor", "mcp.env"), []byte("GITHUB_TOKEN="+fakeCursorEnvFileToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The agents detected and not evaluated (BV-88).
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HERMES_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	codexAuth, err := json.Marshal(map[string]any{
		"OPENAI_API_KEY": fakeCodexSecrets["/OPENAI_API_KEY"],
		"tokens": map[string]any{
			"id_token": fakeCodexSecrets["/tokens/id_token"], "access_token": fakeCodexSecrets["/tokens/access_token"],
			"refresh_token": fakeCodexSecrets["/tokens/refresh_token"], "account_id": "acct",
		},
		"personal_access_token": fakeCodexSecrets["/personal_access_token"],
	})
	if err != nil {
		t.Fatal(err)
	}
	hermesDir := filepath.Join(home, "AppData", "Local", "hermes")
	geminiDir := filepath.Join(home, ".gemini")
	desktopConfig := filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	for p, c := range map[string]string{
		filepath.Join(home, ".codex", "auth.json"):            string(codexAuth),
		filepath.Join(hermesDir, "config.yaml"):               "model: x\n",
		filepath.Join(hermesDir, ".env"):                      "ANTHROPIC_API_KEY=" + fakeHermesKey + "\n",
		filepath.Join(hermesDir, "profiles", "work", ".env"):  "ANTHROPIC_API_KEY=" + fakeHermesProfileKey + "\n",
		filepath.Join(geminiDir, "config", "mcp_config.json"): `{"mcpServers": {"gh": {"command": "npx", "env": {"GITHUB_TOKEN": "` + fakeAgyEnvToken + `"}}, "remote": {"serverUrl": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer ` + fakeAgyHeaderToken + `"}}}}`,
		filepath.Join(repo, ".agents", "mcp_config.json"):     `{"mcpServers": {"gh": {"command": "npx", "env": {"GITHUB_TOKEN": "` + fakeAgyProjectToken + `"}}}}`,
		desktopConfig: `{"mcpServers": {"gh": {"command": "npx", "env": {"GITHUB_TOKEN": "` + fakeDesktopToken + `"}}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("npm_config_userconfig", "")
	t.Setenv("NPM_CONFIG_USERCONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", "")
	t.Setenv("GH_CONFIG_DIR", "")
	credFiles := credfiles.Collect(home, []string{repo}, nil)

	scan, err := windows.New().ListMCPServers(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	var envFiles []string
	for _, srv := range scan.Servers {
		if srv.EnvFile != "" {
			envFiles = append(envFiles, srv.EnvFile)
		}
	}
	credFiles = credfiles.AddEnvFiles(credFiles, envFiles)
	cursorScan, err := windows.New().ListCursor(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListCursor: %v", err)
	}
	settings, err := windows.New().ListClaudeSettings(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListClaudeSettings: %v", err)
	}

	st, err := state.DefaultState("0.1.0-dev")
	if err != nil {
		t.Fatalf("DefaultState: %v", err)
	}
	salt, err := st.Salt()
	if err != nil {
		t.Fatalf("Salt: %v", err)
	}

	// The repository is a git repository with one key in its tree and one
	// only in its history (CTO-7 §2.2), scanned with the salted identify.
	var repoGraph graph.Repos
	if git, err := exec.LookPath("git"); err == nil {
		write := func(name, content string) {
			if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		run := func(args ...string) {
			cmd := exec.Command(git, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		write("deploy.sh", "export TOKEN="+fakeCommittedToken+"\n")
		write("old.txt", "token "+fakeHistoryToken+"\n")
		run("init", "-q")
		run("add", "deploy.sh", "old.txt")
		run("commit", "-q", "-m", "one")
		run("rm", "-q", "old.txt")
		run("commit", "-q", "-m", "two")
		var logBuf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		fs, _, _ := repos.ScanAllRepos(context.Background(), []string{repo}, nil,
			repos.ScanConfig{Patterns: secrets.BuiltinPatterns(), Identify: graph.Identifier(salt)})
		slog.SetDefault(prev)
		repoGraph = graph.Repos{Findings: fs}
		for _, v := range []string{fakeCommittedToken, fakeHistoryToken} {
			if strings.Contains(logBuf.String(), v) || strings.Contains(logBuf.String(), v[4:len(v)-4]) {
				t.Error("the repository scan's log contains a committed secret")
			}
			if dump := fmt.Sprintf("%+v %#v", fs, fs); strings.Contains(dump, v) || strings.Contains(dump, v[4:len(v)-4]) {
				t.Error("the repository findings hold a committed secret")
			}
		}
	} else {
		t.Log("git absent: committed secrets not covered")
	}

	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	frag, _, err := graph.FromScansWithRepos(scan, settings, cursorScan, credFiles, repoGraph, salt, now)
	if err != nil {
		t.Fatalf("FromScan: %v", err)
	}
	instructions, err := windows.New().ListAgentInstructions(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListAgentInstructions: %v", err)
	}
	frag = graph.AddInstructions(frag, instructions, now)
	g := graph.Build(frag, graph.Provenance{EngineVersion: "0.1.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now)

	reports := filepath.Join(home, ".rosmarops", "reports")
	path, err := graph.WriteFile(reports, g)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}

	// The test is only meaningful if the secrets were actually picked up.
	var creds int
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCredential {
			creds++
		}
	}
	if creds < len(fakeEnvSecrets)+1 { // +1: the header secret
		t.Fatalf("credential nodes: got %d, want at least %d — secrets not detected", creds, len(fakeEnvSecrets)+1)
	}
	var ruleCap bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Basis == graph.BasisConfigured && strings.HasPrefix(n.Rule, "Bash(curl -H") {
			ruleCap = true
		}
	}
	if !ruleCap {
		t.Fatal("the settings.local.json rule did not become a capability — rule not read")
	}
	if strings.Contains(string(out), fakeRuleToken) || strings.Contains(string(out), fakeRuleToken[4:len(fakeRuleToken)-4]) {
		t.Error("graph.v2.json contains the token written in a permission rule")
	}
	var envCred, hookExec bool
	for _, n := range g.Nodes {
		for _, loc := range n.Locations {
			envCred = envCred || strings.HasSuffix(loc, "settings.json#/env/GITHUB_TOKEN")
		}
		hookExec = hookExec || (n.Type == graph.ResourceAgentExecutable && strings.HasSuffix(n.Path, "#/hooks/PreToolUse/0/hooks/0/command"))
	}
	if len(fakeSettingsEnvToken) != 40 || !envCred || !hookExec {
		t.Fatalf("settings env credential found %v, hook executable found %v — settings not read", envCred, hookExec)
	}
	for name, v := range map[string]string{"settings env token": fakeSettingsEnvToken, "hook token": fakeHookToken} {
		if strings.Contains(string(out), v) || strings.Contains(string(out), v[4:len(v)-4]) {
			t.Errorf("graph.v2.json contains the %s", name)
		}
	}
	// Cursor (BV-48): the allowlist entry is a capability without its token;
	// the envFile's token is a credential without its value.
	var allowCap bool
	for _, n := range g.Nodes {
		allowCap = allowCap || n.Kind == graph.KindCapability && strings.HasPrefix(n.Rule, "terminalAllowlist:curl")
	}
	if !allowCap {
		t.Fatal("the terminalAllowlist entry did not become a capability — permissions.json not read")
	}
	for name, v := range map[string]string{"Cursor allowlist token": fakeCursorAllowToken, "Cursor envFile token": fakeCursorEnvFileToken} {
		if strings.Contains(string(out), v) || strings.Contains(string(out), v[4:len(v)-4]) {
			t.Errorf("graph.v2.json contains the %s", name)
		}
	}
	credFileLocs := map[string]bool{}
	for _, n := range g.Nodes {
		for _, loc := range n.Locations {
			credFileLocs[loc] = true
		}
	}
	for _, loc := range []string{filepath.Join(repo, ".env") + ":1", filepath.Join(home, ".npmrc") + ":1", filepath.Join(home, ".config", "gh", "hosts.yml") + ":2", filepath.Join(repo, ".cursor", "mcp.env") + ":1"} {
		if !credFileLocs[loc] {
			t.Fatalf("no credential at %s — credential files not read", loc)
		}
	}
	for _, loc := range []string{filepath.Join(home, ".config", "gh", "hosts.yml") + ":4"} {
		if !credFileLocs[loc] {
			t.Fatalf("no credential at %s — ghu_ token not detected", loc)
		}
	}
	for _, key := range []string{"token", "accessToken", "refreshToken"} {
		if loc := filepath.Join(home, ".railway", "config.json") + "#/user/" + key; !credFileLocs[loc] {
			t.Fatalf("no credential at %s — Railway CLI config not read", loc)
		}
	}
	for name, v := range map[string]string{
		"npmrc token": fakeNpmrcToken, "hosts.yml token": fakeHostsToken, "ghu token": fakeGhuToken,
		"railway token": fakeRailwayToken, "railway accessToken": fakeRailwayAccess, "railway refreshToken": fakeRailwayRefresh,
	} {
		if strings.Contains(string(out), v) || strings.Contains(string(out), v[4:len(v)-4]) {
			t.Errorf("graph.v2.json contains the %s", name)
		}
	}
	// The agents detected and not evaluated (BV-88): each is an ai_client,
	// each secret a credential at its location, and no value in the graph.
	clients := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == graph.AgentAIClient {
			clients[n.Platform] = true
		}
	}
	for _, p := range []string{"codex", "hermes", "antigravity", "claude-desktop"} {
		if !clients[p] {
			t.Fatalf("no %s ai_client — agent not detected", p)
		}
	}
	newLocs := []string{
		filepath.Join(hermesDir, ".env") + ":1",
		filepath.Join(hermesDir, "profiles", "work", ".env") + ":1",
		filepath.Join(geminiDir, "config", "mcp_config.json") + "#/mcpServers/gh/env/GITHUB_TOKEN",
		filepath.Join(geminiDir, "config", "mcp_config.json") + "#/mcpServers/remote/headers/Authorization",
		filepath.Join(repo, ".agents", "mcp_config.json") + "#/mcpServers/gh/env/GITHUB_TOKEN",
		desktopConfig + "#/mcpServers/gh/env/GITHUB_TOKEN",
	}
	for pointer := range fakeCodexSecrets {
		newLocs = append(newLocs, filepath.Join(home, ".codex", "auth.json")+"#"+pointer)
	}
	for _, loc := range newLocs {
		if !credFileLocs[loc] {
			t.Fatalf("no credential at %s — file not read", loc)
		}
	}
	newSecrets := map[string]string{
		"Hermes .env key": fakeHermesKey, "Hermes profile key": fakeHermesProfileKey,
		"Antigravity env token": fakeAgyEnvToken, "Antigravity header token": fakeAgyHeaderToken,
		"Antigravity project token": fakeAgyProjectToken, "Claude Desktop token": fakeDesktopToken,
	}
	for pointer, v := range fakeCodexSecrets {
		newSecrets["Codex "+pointer] = v
	}
	for name, v := range newSecrets {
		if strings.Contains(string(out), v) || strings.Contains(string(out), v[4:len(v)-4]) {
			t.Errorf("graph.v2.json contains the %s", name)
		}
	}

	var instructionRes bool
	for _, n := range g.Nodes {
		instructionRes = instructionRes || (n.Type == graph.ResourceAgentInstructions && strings.HasSuffix(n.Path, "CLAUDE.md"))
	}
	if !instructionRes {
		t.Fatal("CLAUDE.md is not an agent_instructions resource — instructions not listed")
	}
	if strings.Contains(string(out), fakeInstructionToken) || strings.Contains(string(out), fakeInstructionToken[4:len(fakeInstructionToken)-4]) {
		t.Error("graph.v2.json contains the token written in CLAUDE.md")
	}
	if strings.Contains(string(out), "hooks.example.invalid") {
		t.Error("graph.v2.json contains hook command text")
	}

	assertNoSecret(t, "graph.v2.json", out, salt)

	// Committed secrets: a git_repository resource and committed_in edges,
	// tracked and history_only, without the values.
	if len(repoGraph.Findings) > 0 {
		presence := map[string]bool{}
		for _, e := range g.Edges {
			if e.Type == graph.EdgeCommittedIn {
				presence[e.Presence] = true
			}
		}
		if !presence["tracked"] || !presence["history_only"] {
			t.Fatalf("committed_in presences %v — committed secrets not in the graph", presence)
		}
		for name, v := range map[string]string{"committed token": fakeCommittedToken, "history token": fakeHistoryToken} {
			if strings.Contains(string(out), v) || strings.Contains(string(out), v[4:len(v)-4]) {
				t.Errorf("graph.v2.json contains the %s", name)
			}
		}
	}

	// The Supabase key is identified from its own claims.
	if !strings.Contains(string(out), `"path": "https://abcdefghijklmnopqrst.supabase.co"`) {
		t.Error("graph lacks the Supabase project resource")
	}

	// State: holds the salt, never a secret value.
	stPath := filepath.Join(home, ".rosmarops", "state.json")
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	stRaw, err := os.ReadFile(stPath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !strings.Contains(string(stRaw), st.LocalSalt) {
		t.Error("state.json does not contain the local salt")
	}
	assertNoSecret(t, "state.json", stRaw, nil)
}
