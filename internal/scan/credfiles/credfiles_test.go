package credfiles_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Fake secrets, each matching a built-in pattern.
func ghp(i int) string { return fmt.Sprintf("ghp_%s%06d", strings.Repeat("C", 30), i) }

const awsKey = "AKIAABCDEFGHIJ123456"

var supabaseJWT = seg(`{"alg":"HS256","typ":"JWT"}`) + "." +
	seg(`{"iss":"supabase","ref":"abcdefghijklmnopqrst","role":"service_role"}`) + ".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"

func seg(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// noEnv is an environment with no variables set.
func noEnv(string) (string, bool) { return "", false }

// TestMain clears npm_config_userconfig, which the npmrc path reads from the
// process environment (globals.UserNpmrcPath).
func TestMain(m *testing.M) {
	os.Unsetenv("npm_config_userconfig")
	os.Unsetenv("NPM_CONFIG_USERCONFIG")
	os.Exit(m.Run())
}

func keysByFile(scan platform.CredentialFileScan) map[string][]string {
	out := map[string][]string{}
	for _, e := range scan.Entries {
		out[e.Path] = append(out[e.Path], fmt.Sprintf("%d:%s", e.Line, e.Key))
	}
	return out
}

// One case per source: every user file and the .env files of a repository.
func TestCollect_EverySource(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "app")
	files := map[string]string{
		filepath.Join(home, ".npmrc"):                     "registry=https://registry.npmjs.org/\n//registry.npmjs.org/:_authToken=" + ghp(1) + "\n",
		filepath.Join(home, ".aws", "credentials"):        "[default]\naws_access_key_id = " + awsKey + "\naws_secret_access_key = x\n",
		filepath.Join(home, ".config", "gh", "hosts.yml"): "github.com:\n    user: me\n    oauth_token: " + ghp(2) + "\n",
		filepath.Join(home, ".git-credentials"):           "https://me:" + ghp(3) + "@github.com\n",
		filepath.Join(home, ".railway", "config.json"):    "{\"user\": {\"token\": \"" + strings.Repeat("r", 36) + "\"}}\n",
		filepath.Join(home, ".supabase", "access-token"):  supabaseJWT + "\n",
		filepath.Join(repo, ".env"):                       "# comment\nexport SUPABASE_KEY=\"" + supabaseJWT + "\"\n",
		filepath.Join(repo, "apps", "web", ".env.local"):  "X=1\n", // depth 3: not read
		filepath.Join(repo, "apps", ".env.production"):    "GITHUB_TOKEN=" + ghp(4) + "\n",
		filepath.Join(repo, "node_modules", ".env"):       "GITHUB_TOKEN=" + ghp(5) + "\n",
		filepath.Join(repo, ".env.example"):               "SUPABASE_KEY=your-key-here\n",
		filepath.Join(repo, ".gitignore"):                 ".env\n",
		filepath.Join(repo, "README.env"):                 "GITHUB_TOKEN=" + ghp(6) + "\n", // not a .env name
	}
	for p, c := range files {
		write(t, p, c)
	}
	scan := credfiles.Collect(home, []string{repo}, noEnv)
	got := keysByFile(scan)
	want := map[string][]string{
		filepath.Join(home, ".npmrc"):                     {"2:_authToken"},
		filepath.Join(home, ".aws", "credentials"):        {"2:aws_access_key_id", "3:aws_secret_access_key"},
		filepath.Join(home, ".config", "gh", "hosts.yml"): {"3:oauth_token"},
		filepath.Join(home, ".git-credentials"):           {"1:password"},
		filepath.Join(home, ".railway", "config.json"):    {"0:token"},
		filepath.Join(home, ".supabase", "access-token"):  {"1:"},
		filepath.Join(repo, ".env"):                       {"2:SUPABASE_KEY"},
		filepath.Join(repo, "apps", ".env.production"):    {"1:GITHUB_TOKEN"},
		filepath.Join(repo, ".env.example"):               {"1:SUPABASE_KEY"},
	}
	for p, w := range want {
		if strings.Join(got[p], ",") != strings.Join(w, ",") {
			t.Errorf("%s: got %v, want %v", p, got[p], w)
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected file read: %s", p)
		}
	}
	if len(scan.Failed) != 0 {
		t.Errorf("failed: %+v", scan.Failed)
	}

	// The graph: one credential per secret, with file:line locations, no
	// receives; the placeholder is no credential; Supabase authenticates_to.
	f, _, err := graph.FromScans(platform.MCPScan{}, platform.ClaudeSettingsScan{}, scan, []byte("0123456789abcdef0123456789abcdef"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	locs := map[string]bool{}
	var authTo int
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCredential {
			for _, l := range n.Locations {
				locs[l] = true
			}
		}
	}
	for _, e := range f.Edges {
		if e.Type == graph.EdgeReceives {
			t.Errorf("receives edge from a credential file: %+v", e)
		}
		if e.Type == graph.EdgeAuthenticatesTo {
			authTo++
		}
	}
	for _, l := range []string{
		filepath.Join(home, ".npmrc") + ":2", filepath.Join(home, ".aws", "credentials") + ":2",
		filepath.Join(home, ".config", "gh", "hosts.yml") + ":3", filepath.Join(home, ".git-credentials") + ":1",
		filepath.Join(home, ".railway", "config.json") + "#/user/token", filepath.Join(home, ".supabase", "access-token") + ":1",
		filepath.Join(repo, ".env") + ":2", filepath.Join(repo, "apps", ".env.production") + ":1",
	} {
		if !locs[l] {
			t.Errorf("no credential at %s; got %v", l, locs)
		}
	}
	if locs[filepath.Join(repo, ".env.example")+":1"] {
		t.Error("placeholder became a credential")
	}
	if authTo == 0 {
		t.Error("no authenticates_to for the Supabase key")
	}
}

// GH_CONFIG_DIR, AWS_SHARED_CREDENTIALS_FILE and npm_config_userconfig move
// the user files.
func TestCollect_EnvironmentMovesFiles(t *testing.T) {
	home, alt := t.TempDir(), t.TempDir()
	write(t, filepath.Join(alt, "gh", "hosts.yml"), "h:\n  oauth_token: "+ghp(1)+"\n")
	write(t, filepath.Join(alt, "aws-creds"), "aws_access_key_id = "+awsKey+"\n")
	write(t, filepath.Join(alt, "npmrc"), "//r/:_authToken="+ghp(2)+"\n")
	t.Setenv("npm_config_userconfig", filepath.Join(alt, "npmrc"))
	env := func(k string) (string, bool) {
		v, ok := map[string]string{"GH_CONFIG_DIR": filepath.Join(alt, "gh"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(alt, "aws-creds")}[k]
		return v, ok
	}
	got := keysByFile(credfiles.Collect(home, nil, env))
	for _, p := range []string{filepath.Join(alt, "gh", "hosts.yml"), filepath.Join(alt, "aws-creds"), filepath.Join(alt, "npmrc")} {
		if len(got[p]) == 0 {
			t.Errorf("%s not read: %v", p, got)
		}
	}
}

// D2: more than 50 .env files in a repository: the first 50, in path order,
// are read; the repository goes to coverage.
func TestCollect_FileCountLimit(t *testing.T) {
	repo := t.TempDir()
	for i := 0; i < 51; i++ {
		write(t, filepath.Join(repo, fmt.Sprintf(".env.%02d", i)), "GITHUB_TOKEN="+ghp(i)+"\n")
	}
	scan := credfiles.Collect(t.TempDir(), []string{repo}, noEnv)
	var read []string
	for p := range keysByFile(scan) {
		read = append(read, filepath.Base(p))
	}
	sort.Strings(read)
	if len(read) != 50 || read[0] != ".env.00" || read[49] != ".env.49" {
		t.Errorf("read %d: %v", len(read), read)
	}
	if len(scan.Failed) != 1 || scan.Failed[0].Path != repo || scan.Failed[0].Reason != graph.ReasonFileCountLimitExceeded+": "+repo {
		t.Errorf("coverage: %+v", scan.Failed)
	}
}

// A symbolic link is not followed: not read, and in coverage.
func TestCollect_SymlinkNotFollowed(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "real.env")
	write(t, target, "GITHUB_TOKEN="+ghp(1)+"\n")
	if err := os.Symlink(target, filepath.Join(repo, ".env")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	scan := credfiles.Collect(t.TempDir(), []string{repo}, noEnv)
	if len(scan.Entries) != 0 {
		t.Errorf("link followed: %+v", scan.Entries)
	}
	if len(scan.Failed) != 1 || !strings.HasPrefix(scan.Failed[0].Reason, graph.ReasonSourceUnsupported+": ") {
		t.Errorf("coverage: %+v", scan.Failed)
	}
}

// The collector's log carries paths and counts, never a value or a key.
func TestCollect_LogHasNoValues(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".npmrc"), "//r/:_authToken="+ghp(1)+"\n")
	write(t, filepath.Join(repo, ".env"), "SECRET_NAME_MARKER="+ghp(2)+"\n")
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	credfiles.Collect(home, []string{repo}, noEnv)
	log := buf.String()
	for _, v := range []string{ghp(1), ghp(2), "SECRET_NAME_MARKER", strings.Repeat("C", 30)} {
		if strings.Contains(log, v) {
			t.Errorf("log contains %q", v)
		}
	}
	for _, want := range []string{"credentials: user_file", "exists=true", "credentials: repo", "env_files=1", "credentials: summary"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// BV-34: GitHub CLI's hosts.yml with a gho_ token and ~/.npmrc with an npm_
// token become github and npm credentials.
func TestCollect_GitHubCLIAndNpmTokens(t *testing.T) {
	home := t.TempDir()
	body := strings.Repeat("Z9y8X7", 6)
	write(t, filepath.Join(home, ".config", "gh", "hosts.yml"), "github.com:\n    user: me\n    oauth_token: gho_"+body+"\n")
	write(t, filepath.Join(home, ".npmrc"), "//registry.npmjs.org/:_authToken=npm_"+body+"\n")
	scan := credfiles.Collect(home, nil, noEnv)
	f, _, err := graph.FromScans(platform.MCPScan{}, platform.ClaudeSettingsScan{}, scan, []byte("0123456789abcdef0123456789abcdef"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCredential {
			got[n.Locations[0]] = n.Type + "/" + n.Service
		}
	}
	want := map[string]string{
		filepath.Join(home, ".config", "gh", "hosts.yml") + ":3": "oauth_token/github",
		filepath.Join(home, ".npmrc") + ":1":                     "api_key/npm",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("credentials %v, want %v", got, want)
	}
}
