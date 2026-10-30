// Package trust contains mechanical enforcement of RosmarOps's security invariants.
// These tests must pass at every milestone from M3 onwards.
package trust_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// fakeAnthropicKey is a synthetic API key that matches the real Anthropic
// pattern exactly: "sk-ant-api03-" + 93 chars + "-" + 10 chars + "AA".
// TestFakeSecretsMatchRealPatterns proves the match, so this test always
// exercises a value the scanner would really flag.
var fakeAnthropicKey = "sk-ant-api03-" + strings.Repeat("A", 93) + "-" + strings.Repeat("B", 10) + "AA"

// fakeGitHubPAT is a synthetic fine-grained GitHub PAT matching the real
// pattern: "github_pat_" + 82 chars of [A-Za-z0-9_] (pattern needs 70+).
var fakeGitHubPAT = "github_pat_11" + strings.Repeat("A", 22) + "_" + strings.Repeat("B", 59)

// BV-34 fakes: gho_, ghu_ and npm_ tokens (36 characters after the prefix).
var (
	fakeGitHubOAuth = "gho_" + strings.Repeat("Q", 30) + "OAUTH1"
	fakeGitHubU2S   = "ghu_" + strings.Repeat("R", 30) + "USERS2"
	fakeNpmToken    = "npm_" + strings.Repeat("N", 30) + "NPMTK3"
)

// BV-36 fakes: Railway CLI tokens, opaque strings read by key from
// ~/.railway/config.json (no secret pattern matches them).
var fakeRailwayTokens = map[string]string{
	"token":        "rwtk" + strings.Repeat("K", 28) + "LEAK",
	"accessToken":  "rwac" + strings.Repeat("C", 28) + "LEAK",
	"refreshToken": "rwrf" + strings.Repeat("R", 28) + "LEAK",
}

// fakeSecrets lists each fake with the pattern that must detect it.
var fakeSecrets = []struct {
	name, value, patternID string
}{
	{"anthropic_key", fakeAnthropicKey, "anthropic_api_key"},
	{"github_pat", fakeGitHubPAT, "github_pat_fine_grained"},
	{"github_oauth", fakeGitHubOAuth, "github_oauth_token"},
	{"github_u2s", fakeGitHubU2S, "github_user_to_server_token"},
	{"npm_token", fakeNpmToken, "npm_access_token"},
}

// TestFakeSecretsMatchRealPatterns guards against the fakes drifting away
// from the real patterns, which would let the tests below pass without
// testing anything.
func TestFakeSecretsMatchRealPatterns(t *testing.T) {
	for _, tc := range fakeSecrets {
		matches := secrets.Match([]byte("x = "+tc.value+"\n"), secrets.BuiltinPatterns())
		found := false
		for _, m := range matches {
			if m.PatternID == tc.patternID && m.RedactedPreview == secrets.Redact(tc.value) {
				found = true
			}
		}
		if !found {
			t.Errorf("[%s] fake value is not detected as %s by secrets.Match", tc.name, tc.patternID)
		}
	}
}

// TestNoFullSecretInMarkdown verifies that a Finding whose evidence contains a
// properly-redacted preview does NOT leak the full secret value into the
// Markdown report.
func TestNoFullSecretInMarkdown(t *testing.T) {
	for _, tc := range fakeSecrets {
		t.Run(tc.name, func(t *testing.T) {
			preview := secrets.Redact(tc.value)

			f := finding.NewFinding(
				"secret_tracked_file:abcd1234abcd1234",
				finding.SeverityRed,
				"secret_tracked_file",
				"Secret found in tracked file",
			)
			f.WhyItMatters = "A token-shaped match was found in a tracked file. If pushed to a remote, the value is public."
			f.SuggestedAction = "Rotate the secret immediately, then remove it from the file."
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				File:            "src/config.ts",
				Line:            42,
				RedactedPreview: preview,
			}

			r := report.Build("0.1.0-dev", []finding.Finding{f}, nil, 1, 0, 0, 0, nil)
			md := report.RenderMarkdown(r)

			// INVARIANT: the full secret must NOT appear anywhere in the output.
			if strings.Contains(md, tc.value) {
				t.Errorf("[%s] full secret value leaked into markdown output", tc.name)
			}

			// Sanity: the redacted preview MUST appear (we're not just stripping everything).
			if !strings.Contains(md, preview) {
				t.Errorf("[%s] redacted preview %q not found in markdown — redaction may be over-zealous", tc.name, preview)
			}
		})
	}
}

// TestNoFullSecretInJSON verifies the same invariant for the JSON sidecar.
func TestNoFullSecretInJSON(t *testing.T) {
	for _, tc := range fakeSecrets {
		t.Run(tc.name, func(t *testing.T) {
			preview := secrets.Redact(tc.value)

			f := finding.NewFinding(
				"secret_tracked_file:abcd1234abcd1234",
				finding.SeverityRed,
				"secret_tracked_file",
				"Secret found in tracked file",
			)
			f.WhyItMatters = "A token-shaped match was found in a tracked file."
			f.SuggestedAction = "Rotate immediately."
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RedactedPreview: preview,
			}

			r := report.Build("0.1.0-dev", []finding.Finding{f}, nil, 1, 0, 0, 0, nil)
			jsonData, err := report.RenderJSON(r)
			if err != nil {
				t.Fatalf("RenderJSON: %v", err)
			}

			// INVARIANT: full secret must NOT appear in JSON output.
			if strings.Contains(string(jsonData), tc.value) {
				t.Errorf("[%s] full secret value leaked into JSON output", tc.name)
			}

			// The JSON must be valid.
			var probe any
			if err := json.Unmarshal(jsonData, &probe); err != nil {
				t.Errorf("[%s] JSON output is invalid: %v", tc.name, err)
			}

			// Sanity: redacted preview must appear.
			if !strings.Contains(string(jsonData), preview) {
				t.Errorf("[%s] redacted preview %q not found in JSON", tc.name, preview)
			}
		})
	}
}

// TestRedactionFormat verifies the redaction function produces the exact format
// required by the invariant: 4 chars + " ... " + 4 chars.
func TestRedactionFormat(t *testing.T) {
	preview := secrets.Redact(fakeAnthropicKey)

	if !strings.HasPrefix(preview, fakeAnthropicKey[:4]) {
		t.Errorf("preview does not start with first 4 chars of secret")
	}
	if !strings.HasSuffix(preview, fakeAnthropicKey[len(fakeAnthropicKey)-4:]) {
		t.Errorf("preview does not end with last 4 chars of secret")
	}
	if !strings.Contains(preview, " ... ") {
		t.Error("preview does not contain ' ... ' separator")
	}
	// The full middle of the key must NOT appear in the preview.
	middle := fakeAnthropicKey[4 : len(fakeAnthropicKey)-4]
	if strings.Contains(preview, middle) {
		t.Error("preview contains middle portion of secret — redaction failed")
	}
}

// TestNoFullSecretInRunLog scans a repository holding the fake secrets in a
// tracked file and in its history, with the run log at DEBUG (per-check
// timing, BV-30, included), and asserts the log never holds a full value or
// its middle.
func TestNoFullSecretInRunLog(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	content := ""
	for _, tc := range fakeSecrets {
		content += tc.name + " = " + tc.value + "\n"
	}
	if err := os.WriteFile(filepath.Join(repo, "config.py"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "config.py"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	findings, _, _ := repos.ScanAllRepos(context.Background(), []string{repo},
		nil, repos.ScanConfig{Patterns: secrets.BuiltinPatterns()})
	var tracked, history int
	for _, f := range findings {
		switch f.Category {
		case "secret_tracked":
			tracked++
		case "secret_in_history":
			history++
		}
	}
	if tracked < len(fakeSecrets) || history < len(fakeSecrets) {
		t.Fatalf("secrets not detected (tracked %d, history %d) — test would be vacuous", tracked, history)
	}
	log := buf.String()
	if !strings.Contains(log, `msg="repos: check"`) {
		t.Fatal("no per-check timing lines in the log")
	}
	for _, tc := range fakeSecrets {
		if strings.Contains(log, tc.value) || strings.Contains(log, tc.value[4:len(tc.value)-4]) {
			t.Errorf("[%s] run log contains the secret", tc.name)
		}
	}
}

// TestNoFullSecretInCredentialCollectorLog runs the credential files
// collector (BV-32) over the fake secrets in a .env, ~/.npmrc, GitHub CLI's
// hosts.yml and the Railway CLI's config.json (BV-36), with the log at
// DEBUG, and asserts the log never holds a full value, its middle, or the
// variable names.
func TestNoFullSecretInCredentialCollectorLog(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	files := map[string]string{
		filepath.Join(repo, ".env"):                       "LEAKCHECK_ANTHROPIC=" + fakeAnthropicKey + "\n",
		filepath.Join(home, ".npmrc"):                     "//registry.npmjs.org/:_authToken=" + fakeNpmToken + "\n",
		filepath.Join(home, ".config", "gh", "hosts.yml"): "github.com:\n    oauth_token: " + fakeGitHubOAuth + "\nx.example.com:\n    oauth_token: " + fakeGitHubU2S + "\n",
		filepath.Join(home, ".railway", "config.json"):    `{"user": {"token": "` + fakeRailwayTokens["token"] + `", "accessToken": "` + fakeRailwayTokens["accessToken"] + `", "refreshToken": "` + fakeRailwayTokens["refreshToken"] + `"}}`,
		// Codex's auth.json and Hermes Agent's .env and a profile's (BV-88).
		filepath.Join(home, ".codex", "auth.json"): `{"OPENAI_API_KEY": "` + fakeCodexTokens["OPENAI_API_KEY"] + `", "tokens": {"id_token": "` + fakeCodexTokens["id_token"] +
			`", "access_token": "` + fakeCodexTokens["access_token"] + `", "refresh_token": "` + fakeCodexTokens["refresh_token"] + `"}, "personal_access_token": "` + fakeCodexTokens["personal_access_token"] + `"}`,
		filepath.Join(home, "AppData", "Local", "hermes", ".env"):                     "LEAKCHECK_HERMES=" + fakeCodexTokens["hermes_env"] + "\n",
		filepath.Join(home, "AppData", "Local", "hermes", "profiles", "work", ".env"): "LEAKCHECK_HERMES=" + fakeCodexTokens["hermes_profile"] + "\n",
	}
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("npm_config_userconfig", "")
	t.Setenv("NPM_CONFIG_USERCONFIG", "")
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	scan := credfiles.Collect(home, []string{repo}, func(string) (string, bool) { return "", false })
	if len(scan.Entries) < 14 {
		t.Fatalf("entries: %d — test would be vacuous", len(scan.Entries))
	}
	log := buf.String()
	if strings.Contains(log, "LEAKCHECK_ANTHROPIC") {
		t.Error("collector log contains a variable name")
	}
	for _, tc := range fakeSecrets {
		if strings.Contains(log, tc.value) || strings.Contains(log, tc.value[4:len(tc.value)-4]) {
			t.Errorf("[%s] collector log contains the secret", tc.name)
		}
	}
	for key, v := range fakeRailwayTokens {
		if strings.Contains(log, v) || strings.Contains(log, v[4:len(v)-4]) {
			t.Errorf("[railway %s] collector log contains the secret", key)
		}
	}
	for key, v := range fakeCodexTokens {
		if strings.Contains(log, v) || strings.Contains(log, v[4:len(v)-4]) {
			t.Errorf("[%s] collector log contains the secret", key)
		}
	}
	if strings.Contains(log, "LEAKCHECK_HERMES") {
		t.Error("collector log contains a variable name")
	}
}

// fakeCodexTokens are Codex's auth.json values and Hermes Agent's .env
// values (BV-88).
var fakeCodexTokens = map[string]string{
	"OPENAI_API_KEY":        "sk-proj-LEAK" + strings.Repeat("K", 40) + "LEAK",
	"id_token":              "eyJhbGciOiJSUzI1NiJ9.LEAKIDTOKN" + strings.Repeat("i", 30) + ".sigLEAK",
	"access_token":          "eyJhbGciOiJSUzI1NiJ9.LEAKACCESS" + strings.Repeat("a", 30) + ".sigLEAK",
	"refresh_token":         "rt_LEAK" + strings.Repeat("r", 40) + "LEAK",
	"personal_access_token": "pat_LEAK" + strings.Repeat("p", 40) + "LEAK",
	"hermes_env":            "sk-ant-api03-" + strings.Repeat("L", 93) + "-" + strings.Repeat("E", 10) + "AA",
	"hermes_profile":        "sk-ant-api03-" + strings.Repeat("M", 93) + "-" + strings.Repeat("F", 10) + "AA",
}

// TestNoFullSecretInReportV2 runs the v2 report (BV-37) over the fake
// secrets and asserts neither report.v2.json nor the Markdown holds a full
// value or its middle. The secrets sit in a repository's .env and in the
// Railway CLI config, Claude Code has the built-in auto mode (so every
// credential is a finding), and the v1 findings carry their previews.
func TestNoFullSecretInReportV2(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	var env strings.Builder
	for _, tc := range fakeSecrets {
		env.WriteString("LEAKCHECK_" + strings.ToUpper(tc.name) + "=" + tc.value + "\n")
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(env.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".railway"), 0o700); err != nil {
		t.Fatal(err)
	}
	railway := `{"user": {"token": "` + fakeRailwayTokens["token"] + `", "accessToken": "` + fakeRailwayTokens["accessToken"] + `", "refreshToken": "` + fakeRailwayTokens["refreshToken"] + `"}}`
	if err := os.WriteFile(filepath.Join(home, ".railway", "config.json"), []byte(railway), 0o600); err != nil {
		t.Fatal(err)
	}
	// Codex's auth.json and Hermes Agent's .env (BV-88).
	codexAuth := `{"OPENAI_API_KEY": "` + fakeCodexTokens["OPENAI_API_KEY"] + `", "tokens": {"id_token": "` + fakeCodexTokens["id_token"] +
		`", "access_token": "` + fakeCodexTokens["access_token"] + `", "refresh_token": "` + fakeCodexTokens["refresh_token"] + `"}, "personal_access_token": "` + fakeCodexTokens["personal_access_token"] + `"}`
	hermesEnv := filepath.Join(home, "AppData", "Local", "hermes", ".env")
	for p, c := range map[string]string{
		filepath.Join(home, ".codex", "auth.json"): codexAuth,
		hermesEnv: "LEAKCHECK_HERMES=" + fakeCodexTokens["hermes_env"] + "\n",
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
	credFiles := credfiles.Collect(home, []string{repo}, func(string) (string, bool) { return "", false })

	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	claudeDir := filepath.Join(home, ".claude")
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: filepath.Join(claudeDir, "settings.json"), ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}},
	}}}
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, credFiles, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), []string{repo}, home)
	if err != nil {
		t.Fatal(err)
	}

	var v1 []finding.Finding
	for i, tc := range fakeSecrets {
		f := finding.NewFinding("secret_tracked:"+strings.Repeat(string(rune('a'+i)), 16), finding.SeverityRed, "secret_tracked", "Secret in tracked file")
		f.GroupDetail = tc.patternID
		f.Evidence = finding.Evidence{RepoPath: repo, File: ".env", RedactedPreview: secrets.Redact(tc.value)}
		v1 = append(v1, f)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	r := report.Build("0.1.0-dev", v1, nil, 1, 0, 0, 0, nil)
	v, _ := report.BuildV2(report.V2Input{Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: []string{repo}, V1: r, GeneratedAt: now})
	// Every fake secret is in a finding: each .env line and each Railway
	// pointer is a location of a cred_read_and_send finding (same-file
	// credentials of one service share one finding, BV-39).
	located := map[string]bool{}
	for _, f := range v.Findings {
		if f.Rule == "cred_read_and_send" {
			for _, l := range f.Evidence.Locations {
				located[l] = true
			}
		}
	}
	var want []string
	for i := range fakeSecrets {
		want = append(want, filepath.Join(repo, ".env")+":"+strconv.Itoa(i+1))
	}
	for k := range fakeRailwayTokens {
		want = append(want, filepath.Join(home, ".railway", "config.json")+"#/user/"+k)
	}
	for _, p := range []string{"/OPENAI_API_KEY", "/tokens/id_token", "/tokens/access_token", "/tokens/refresh_token", "/personal_access_token"} {
		want = append(want, filepath.Join(home, ".codex", "auth.json")+"#"+p)
	}
	want = append(want, hermesEnv+":1")
	for _, l := range want {
		if !located[l] {
			t.Fatalf("no finding at %s — test would be vacuous; located %v", l, located)
		}
	}
	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	md := report.RenderV2Markdown(v, cat, time.UTC)
	values := map[string]string{}
	for _, tc := range fakeSecrets {
		values[tc.name] = tc.value
	}
	for k, val := range fakeRailwayTokens {
		values["railway "+k] = val
	}
	for k, val := range fakeCodexTokens {
		values[k] = val
	}
	outputs := map[string]string{"report.v2.json": string(js), "markdown": md}
	// BV-40: the HTML in en and es, the card in it, and index-history.json.
	for _, lang := range []string{"en", "es"} {
		lc, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		page := report.RenderHTML(v, lc, home, time.UTC)
		if !strings.Contains(page, `<template id="card-svg">`) {
			t.Fatalf("%s: no card in the HTML — test would be vacuous", lang)
		}
		outputs["html "+lang] = page
		svg, text, ok := index.Card(v.Index.Agents[0], v.Silenced.Count, now, lc)
		if !ok {
			t.Fatalf("%s: no card", lang)
		}
		outputs["card "+lang] = svg + "\n" + text
	}
	histDir := t.TempDir()
	if err := index.WriteHistory(histDir, index.History{}, index.EntryOf(v.Index, "2026-10-24")); err != nil {
		t.Fatal(err)
	}
	hist, err := os.ReadFile(filepath.Join(histDir, index.HistoryFileName))
	if err != nil {
		t.Fatal(err)
	}
	outputs["index-history.json"] = string(hist)
	// BV-42: findings-memory.json.
	if err := memory.Write(histDir, v.NextMemory()); err != nil {
		t.Fatal(err)
	}
	mem, err := os.ReadFile(filepath.Join(histDir, memory.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mem), `"mid"`) {
		t.Fatal("findings memory has no entry — test would be vacuous")
	}
	outputs["findings-memory.json"] = string(mem)
	for name, out := range outputs {
		for secret, val := range values {
			if strings.Contains(out, val) || strings.Contains(out, val[4:len(val)-4]) {
				t.Errorf("%s contains the %s secret", name, secret)
			}
		}
		if strings.Contains(out, "LEAKCHECK_") {
			t.Errorf("%s contains a variable name", name)
		}
	}
}

// informe HTML v2 §8, the three checks of CTO-8 §6 on the export for your
// AI (report-<base>.ai.md, and the template in the HTML), in en and es: no
// value, preview, fingerprint, host id, id or path naming the user; at most
// 48 KB; the same output for two runs with another salt and host id and the
// same paths.
func TestNoFullSecretInAIExport(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Users", "leakcheckuser")
	repo := filepath.Join(home, "src", "app")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	var env strings.Builder
	for _, tc := range fakeSecrets {
		env.WriteString("LEAKCHECK_" + strings.ToUpper(tc.name) + "=" + tc.value + "\n")
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(env.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("npm_config_userconfig", "")
	t.Setenv("NPM_CONFIG_USERCONFIG", "")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	build := func(salt []byte) (report.V2, []string) {
		credFiles := credfiles.Collect(home, []string{repo}, func(string) (string, bool) { return "", false })
		claudeDir := filepath.Join(home, ".claude")
		settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
			Scope: platform.MCPScopeUser, ConfigPath: filepath.Join(claudeDir, "settings.json"), ConfigDir: claudeDir,
			Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}},
		}}}
		frag, _, err := graph.FromScans(platform.MCPScan{}, settings, credFiles, salt, now)
		if err != nil {
			t.Fatal(err)
		}
		g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), []string{repo}, home)
		if err != nil {
			t.Fatal(err)
		}
		var private []string // what the export must never carry
		private = append(private, graph.HostID(salt))
		for _, n := range g.Nodes {
			if n.Fingerprint != "" {
				private = append(private, n.Fingerprint)
			}
		}
		v, _ := report.BuildV2(report.V2Input{Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: []string{repo}, V1: report.Build("0.1.0-dev", nil, nil, 1, 0, 0, 0, nil), GeneratedAt: now, Salt: salt})
		if len(v.Findings) == 0 {
			t.Fatal("no finding — test would be vacuous")
		}
		return v, private
	}
	vA, privA := build([]byte("0123456789abcdef0123456789abcdef"))
	vB, privB := build([]byte("fedcba9876543210fedcba9876543210"))
	if privA[0] == privB[0] {
		t.Fatal("same host id for two salts — test would be vacuous")
	}
	idShape := regexp.MustCompile(`\b(f|a|cred|cap|res_[a-z_]+|edge_[a-z_]+|agent_client|agent_server)_[0-9a-f]{16}\b`)
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		path, err := report.WriteAIExport(vA, cat, home, dir, "report-x")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "report-x.ai.md" || filepath.Dir(path) != dir {
			t.Errorf("%s: export written to %s", lang, path)
		}
		raw, _ := os.ReadFile(path)
		out := string(raw)
		page := report.RenderHTML(vA, cat, home, time.UTC)
		tmpl := regexp.MustCompile(`(?s)<template id="ai-export">(.*?)</template>`).FindStringSubmatch(page)
		if tmpl == nil || html.UnescapeString(tmpl[1]) != out {
			t.Fatalf("%s: the HTML's template is not the export", lang)
		}
		if !strings.Contains(out, "~"+filepath.FromSlash("/src/app/.env")) { // the path keeps its own separators
			t.Fatalf("%s: no location in the export — test would be vacuous", lang)
		}
		for _, tc := range fakeSecrets {
			if strings.Contains(out, tc.value) || strings.Contains(out, tc.value[4:len(tc.value)-4]) || strings.Contains(out, secrets.Redact(tc.value)) {
				t.Errorf("%s: export carries the %s secret or its preview", lang, tc.name)
			}
		}
		for _, p := range append(privA, privB...) {
			if strings.Contains(out, p) {
				t.Errorf("%s: export carries %q", lang, p)
			}
		}
		if m := idShape.FindString(out); m != "" {
			t.Errorf("%s: export carries the id %s", lang, m)
		}
		if strings.Contains(out, "leakcheckuser") || strings.Contains(out, "LEAKCHECK_") {
			t.Errorf("%s: export names the user or a variable", lang)
		}
		if len(out) > report.AIMaxBytes {
			t.Errorf("%s: export is %d bytes, over %d", lang, len(out), report.AIMaxBytes)
		}
		if other := report.RenderAIExport(vB, cat, home); other != out {
			t.Errorf("%s: export differs with another salt and host id", lang)
		}
	}
}

// CTO-7 §2.2: a key committed in a repository reaches the graph as a
// committed_in edge, and no file the run writes (graph, reports, state,
// memory, the accepted configuration copy) holds it, or its middle.
func TestNoCommittedSecretInRunOutputs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	s := newSandbox(t)
	s.runAll(t, nil)
	dir := filepath.Join(s.home, ".rosmarops")
	graphRaw, err := os.ReadFile(filepath.Join(dir, "reports", "graph.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(graphRaw), `"type": "committed_in"`) {
		t.Fatal("no committed_in edge: the committed key was not scanned")
	}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if v := fakeCommittedToken; strings.Contains(string(raw), v) || strings.Contains(string(raw), v[4:len(v)-4]) {
			t.Errorf("%s holds the committed key", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoMCPDestinationSecretInReports is BV-91 (D6): a secret in a new MCP
// server's remote URL never reaches the report, the Markdown or the export for
// your AI. The «new MCP server» line shows only the host; the URL's userinfo,
// path, query and fragment never appear. A prior run without the server makes
// it «new», so the line is rendered.
func TestNoMCPDestinationSecretInReports(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Users", "mcpleakuser")
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const (
		pw    = "urlpwSECRET1234"
		query = "qrySECRET1234"
		frag  = "fragSECRET1234"
		path  = "pathSECRET1234"
	)
	secretURL := "https://user:" + pw + "@api.secret.example.com/mcp/" + path + "?api_key=" + query + "#" + frag
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	source := platform.MCPSource{
		Platform: "claude-code", ConfigPath: filepath.Join(home, ".claude.json"), ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, LoadsMCP: true,
	}
	server := platform.MCPServer{
		Client: "claude-code", Name: "remote", ConfigPath: source.ConfigPath, ConfigDir: claudeDir,
		LoadsMCP: true, Scope: platform.MCPScopeUser, Transport: "http", URL: secretURL,
	}
	buildV2 := func(scan platform.MCPScan, mem memory.File) report.V2 {
		frag, err := graph.FromMCPScan(scan, salt, now)
		if err != nil {
			t.Fatal(err)
		}
		g := graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now)
		v, _ := report.BuildV2(report.V2Input{
			Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: nil,
			V1: report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now, Memory: mem, Salt: salt, Home: home,
		})
		return v
	}
	// Run 1: the client, no server — the base for the comparison.
	v1 := buildV2(platform.MCPScan{Sources: []platform.MCPSource{source}}, memory.File{})
	// Run 2: the client plus the remote server — now «new».
	v2 := buildV2(platform.MCPScan{Sources: []platform.MCPSource{source}, Servers: []platform.MCPServer{server}}, v1.NextMemory())
	if len(v2.NewMCP) == 0 {
		t.Fatal("the remote server is not reported as new — test would be vacuous")
	}

	forbidden := []string{pw, query, frag, path, "/mcp/", "api_key=", "user:" + pw}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs := map[string]string{
			"HTML":     report.RenderHTML(v2, cat, home, time.UTC),
			"Markdown": report.RenderV2Markdown(v2, cat, time.UTC),
			"AI":       report.RenderAIExport(v2, cat, home),
		}
		for which, out := range outputs {
			for _, f := range forbidden {
				if strings.Contains(out, f) {
					t.Errorf("%s/%s carries the URL secret %q", lang, which, f)
				}
			}
			if !strings.Contains(out, "api.secret.example.com") {
				t.Errorf("%s/%s lacks the host — the new-MCP line was not rendered", lang, which)
			}
		}
	}
}

// BV-118 (point 6): the kit key, the phrase and the recovery code never
// reach a file RosmarOps writes but the kit key file itself: not the log,
// the reports, the state, the graph, the export for your AI (in the HTML) or
// the kit file. The test goes through the whole cycle: create, a run, import in another
// profile, new-phrase there, and import of that file in a third profile with
// the old phrase (refused) and then the code (opens the same key): the
// BV-118 final check.
func TestNoKitSecretInOutputs(t *testing.T) {
	s := newSandbox(t)
	s.initHome(t, s.home)
	code, file := s.kitCreate(t, s.home)
	key := storedKey(t, s.home)
	for _, step := range [][]string{{"baseline"}, {"run"}, {"run", "--unattended"}} {
		out, err := s.kitRun(t, s.home, step, func(string) (string, bool) { return "", false })
		var exitErr *exec.ExitError
		if err != nil && !(step[0] == "run" && errors.As(err, &exitErr)) {
			t.Fatalf("%s: %v\n%s", step[0], err, out)
		}
	}
	reports := filepath.Join(s.home, ".rosmarops", "reports")
	for _, f := range []string{"graph.v2.json", "report.v2.json", "report.html", "last-scheduled-run-log.txt"} {
		if _, err := os.Stat(filepath.Join(reports, f)); err != nil {
			t.Fatalf("run did not complete: %v", err)
		}
	}

	second := filepath.Join(s.root, "home2")
	s.initHome(t, second)
	out, err := s.kitRun(t, second, []string{"kit", "import", file}, func(line string) (string, bool) { return kitPhrase, true })
	if err != nil || !strings.Contains(out, "Key imported.") {
		t.Fatalf("import with the phrase: %v\n%s", err, out)
	}
	salt := func(home string) string {
		st, err := os.ReadFile(filepath.Join(home, ".rosmarops", "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			LocalSalt string `json:"local_salt"`
		}
		if err := json.Unmarshal(st, &v); err != nil {
			t.Fatal(err)
		}
		return v.LocalSalt
	}
	if salt(second) != salt(s.home) || storedKey(t, second) != key {
		t.Fatal("import did not take the kit's key and user salt")
	}
	const newPhrase = "the second phrase, new one"
	out, err = s.kitRun(t, second, []string{"kit", "new-phrase"}, func(line string) (string, bool) { return newPhrase, true })
	if err != nil || !strings.Contains(out, "New phrase set.") {
		t.Fatalf("new-phrase: %v\n%s", err, out)
	}
	newFile := filepath.Join(second, ".rosmarops", "reports", filepath.Base(file))
	third := filepath.Join(s.root, "home3")
	s.initHome(t, third)
	answers := []string{kitPhrase, strings.ToLower(code)}
	out, err = s.kitRun(t, third, []string{"kit", "import", newFile}, func(line string) (string, bool) {
		if !strings.HasSuffix(line, ":") || len(answers) == 0 {
			return "", false
		}
		a := answers[0]
		answers = answers[1:]
		return a, true
	})
	if err != nil || !strings.Contains(out, "That doesn't open this key file.") || !strings.Contains(out, "Key imported.") {
		t.Fatalf("import with the old phrase, then the code: %v\n%s", err, out)
	}
	if storedKey(t, third) != key {
		t.Fatal("the code opened another key")
	}

	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{
		"key (base64)": key, "key (hex)": hex.EncodeToString(raw), "key (raw)": string(raw),
		"phrase": kitPhrase, "new phrase": newPhrase, "code": code, "grouped code": codeLine.FindStringSubmatch("Your recovery code is: " + groupOf4(code) + ".")[1],
	}
	files := 0
	err = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "kit.key" || d.Type()&os.ModeSymlink != 0 {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for what, v := range secrets {
			if bytes.Contains(content, []byte(v)) {
				t.Errorf("%s holds the kit %s", path, what)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 10 {
		t.Fatalf("walked %d files", files)
	}
}

// groupOf4 is code in groups of 4, as kit create shows it.
func groupOf4(code string) string {
	var g []string
	for i := 0; i < len(code); i += 4 {
		g = append(g, code[i:i+4])
	}
	return strings.Join(g, "-")
}
