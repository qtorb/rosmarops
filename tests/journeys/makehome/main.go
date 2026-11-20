// Command makehome builds a realistic test computer in a new folder,
// deterministically (fixed dates, authors and content, fixed fake secrets, no
// network), on Windows, macOS and Linux. It is the generator behind the journey
// tests (journeys.yml) and scripts/e2e/make-home.ps1. It only writes under the
// home folder it is given and the manifest file, never runs rosmarops and
// never touches the network. Never part of the program.
//
// What it builds under -home:
//   - the agents and traps of testdata/sample-home (Claude Code, Cursor,
//     Hermes, Claude Desktop, .mcp.json, shared skills, a project hook, an .env
//     committed to git, a malicious package in a .venv), with the placeholders
//     replaced by fixed fake secrets, and a pinned malicious MCP server in
//     Cursor (npx -y @ifasn/md-doc-mcp@1.0.4);
//   - the two sample projects, shop-api and landing-page, as git repositories
//     under projects/, plus -medium generated repositories (med-NN) and -large
//     ones (big-NN: a path longer than 180 characters in the first, a fake AWS
//     key in the history of the first three), all with git fast-import.
//
// The fake secrets go to -manifest, outside the home, so a run never scans
// them and a check can prove none appears in full in any output. Every file is
// checked to hold no {name} placeholder, so an unfilled catalog parameter in a
// report can only come from RosmarOps.
//
//	go run ./tests/journeys/makehome -home <dir> -manifest <file>.json [-medium 40 -large 6]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The fixed fake secrets: prefixes RosmarOps's matchers know, with fixed
// bodies. None is a real credential.
var secrets = []struct{ placeholder, value string }{
	{"__SAMPLE_GITHUB_TOKEN__", "ghp_" + strings.Repeat("E2e0", 9)},
	{"__SAMPLE_POSTGRES_PASSWORD__", "E2ePgPassw0rd00000000"},
	{"__SAMPLE_OPENAI_KEY__", "sk-proj-" + strings.Repeat("E2e0", 14)},
	{"__SAMPLE_STRIPE_KEY__", "sk_live_" + strings.Repeat("E2e0", 8)},
}

// awsKeys are the fake keys planted in the history of the first three large
// repositories.
var awsKeys = []string{"AKIAE2E0EXAMPLE00001", "AKIAE2E0EXAMPLE00002", "AKIAE2E0EXAMPLE00003"}

// placeholder is a catalog parameter, which no file of the test computer may
// hold.
var placeholder = regexp.MustCompile(`\{[a-z_]+\}`)

// base is 2020-01-01T00:00:00Z, the fixed time of the first generated commit.
const base = 1577836800

type options struct {
	home, fixture, manifest                                             string
	medium, large, mediumCommits, mediumFiles, largeCommits, largeFiles int
	// paperclip adds a fake Paperclip orchestrator (BV-264): one instance in
	// its default mode, its secrets next to its database, installed at a
	// version before the fix of its known flaws.
	paperclip bool
}

func main() {
	var o options
	flag.StringVar(&o.home, "home", "", "the test computer's home folder (must not exist)")
	flag.StringVar(&o.fixture, "fixture", filepath.Join("testdata", "sample-home"), "the sample home to copy")
	flag.StringVar(&o.manifest, "manifest", "", "where to write the manifest (outside the home)")
	flag.IntVar(&o.medium, "medium", 40, "generated medium repositories")
	flag.IntVar(&o.large, "large", 6, "generated large repositories")
	flag.IntVar(&o.mediumCommits, "medium-commits", 100, "commits per medium repository")
	flag.IntVar(&o.mediumFiles, "medium-files", 200, "files per medium repository")
	flag.IntVar(&o.largeCommits, "large-commits", 3000, "commits per large repository")
	flag.IntVar(&o.largeFiles, "large-files", 2000, "files per large repository")
	flag.BoolVar(&o.paperclip, "paperclip", false, "add a fake Paperclip orchestrator")
	flag.Parse()
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "makehome:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.home == "" || o.manifest == "" {
		return errors.New("-home and -manifest are required")
	}
	if _, err := os.Lstat(o.home); err == nil {
		return fmt.Errorf("%s already exists: the test computer is made from scratch", o.home)
	}
	if err := copyTree(o.fixture, o.home); err != nil {
		return fmt.Errorf("copy the sample home: %w", err)
	}
	for _, rel := range []string{".claude.json", filepath.Join(".cursor", "mcp.json"), filepath.Join("AppData", "Local", "hermes", ".env"), filepath.Join("projects", "shop-api", ".env")} {
		if err := fillSecrets(filepath.Join(o.home, rel)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(o.home, "projects", "shop-api", ".gitignore"), []byte(".venv/\n"), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(o.home, "AppData", "Local", "Temp"), 0o755); err != nil {
		return err
	}
	if err := addPinnedMCP(filepath.Join(o.home, ".cursor", "mcp.json")); err != nil {
		return err
	}
	if o.paperclip {
		if err := addPaperclip(o.home); err != nil {
			return err
		}
	}
	projects := filepath.Join(o.home, "projects")
	for _, name := range []string{"shop-api", "landing-page"} {
		if err := sampleRepo(filepath.Join(projects, name), name); err != nil {
			return err
		}
	}
	for n := 1; n <= o.medium; n++ {
		if err := fastImportRepo(filepath.Join(projects, fmt.Sprintf("med-%02d", n)), o.mediumCommits, o.mediumFiles, false, ""); err != nil {
			return err
		}
	}
	for n := 1; n <= o.large; n++ {
		key := ""
		if n <= len(awsKeys) {
			key = awsKeys[n-1]
		}
		if err := fastImportRepo(filepath.Join(projects, fmt.Sprintf("big-%02d", n)), o.largeCommits, o.largeFiles, n == 1, key); err != nil {
			return err
		}
	}
	if err := noPlaceholders(o.home); err != nil {
		return err
	}
	all := make([]string, 0, len(secrets)+len(awsKeys))
	for _, s := range secrets {
		all = append(all, s.value)
	}
	all = append(all, awsKeys...)
	want := o.medium + o.large + 2
	m, _ := json.MarshalIndent(map[string]any{"home": o.home, "projects": projects, "expected": want, "secrets": all}, "", "  ")
	if err := os.WriteFile(o.manifest, append(m, '\n'), 0o644); err != nil {
		return err
	}
	got, err := countRepos(projects)
	if err != nil {
		return err
	}
	fmt.Printf("Test computer built at %s\n  git repositories under projects: %d (want %d)\n  manifest: %s\n", o.home, got, want, o.manifest)
	if got != want {
		return fmt.Errorf("built %d repositories, want %d", got, want)
	}
	return nil
}

// copyTree copies the fixture, leaving out the .gitkeep markers.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(out, 0o755)
		case d.Name() == ".gitkeep":
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

func fillSecrets(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec.placeholder, sec.value)
	}
	if regexp.MustCompile(`__SAMPLE_[A-Z_]+__`).MatchString(s) {
		return fmt.Errorf("placeholder left in %s", path)
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// addPinnedMCP adds the pinned malicious MCP server to Cursor's mcp.json.
func addPinnedMCP(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	doc["mcpServers"]["md-doc"] = map[string]any{"command": "npx", "args": []string{"-y", "@ifasn/md-doc-mcp@1.0.4"}}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// gitEnv is git's environment for a deterministic repository: no system
// configuration, fixed identity and dates.
func gitEnv(extra ...string) []string {
	return append(os.Environ(), append([]string{"GIT_CONFIG_NOSYSTEM=1"}, extra...)...)
}

func git(dir string, env []string, stdin []byte, args ...string) error {
	cmd := exec.Command("git", append([]string{
		"-C", dir, "-c", "core.autocrlf=false", "-c", "core.longpaths=true",
		"-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false", "-c", "user.name=Sample Developer",
		"-c", "user.email=sample@example.com",
	}, args...)...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s in %s: %w\n%s", strings.Join(args, " "), dir, err, out)
	}
	return nil
}

func sampleRepo(dir, name string) error {
	env := gitEnv("GIT_AUTHOR_DATE=2026-10-20T10:00:00Z", "GIT_COMMITTER_DATE=2026-10-20T10:00:00Z")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"commit", "-q", "-m", "Initial commit"},
		{"remote", "add", "origin", "https://git.example.com/sample/" + name + ".git"},
	} {
		if err := git(dir, env, nil, args...); err != nil {
			return err
		}
	}
	return nil
}

// fastImportRepo makes one generated repository: commits commits on main,
// each writing one of files files, with fixed authors and dates; with
// longPath, a file at a path longer than 180 characters; with awsKey, the key
// added in commit 5 and removed in commit 7, so it is in the history only.
func fastImportRepo(dir string, commits, files int, longPath bool, awsKey string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	env := gitEnv()
	if err := git(dir, env, nil, "init", "-q"); err != nil {
		return err
	}
	longRel := strings.Repeat(strings.Repeat("d", 10)+"/", 17) + "leaf.txt"
	var b bytes.Buffer
	data := func(s string) { fmt.Fprintf(&b, "data %d\n%s\n", len(s), s) }
	mark, prev := 0, 0
	for i := 1; i <= commits; i++ {
		when := fmt.Sprintf("%d +0000", base+i*60)
		idx := (i-1)%files + 1
		rel := fmt.Sprintf("src/f%04d.txt", idx)
		mark++
		blob := mark
		fmt.Fprintf(&b, "blob\nmark :%d\n", blob)
		data(fmt.Sprintf("file %d, revision at commit %d\n", idx, i))
		var extra []string
		if longPath && i == 2 {
			mark++
			fmt.Fprintf(&b, "blob\nmark :%d\n", mark)
			data("deep file at a path longer than 180 characters\n")
			extra = append(extra, fmt.Sprintf("M 100644 :%d %s", mark, longRel))
		}
		if awsKey != "" && i == 5 {
			mark++
			fmt.Fprintf(&b, "blob\nmark :%d\n", mark)
			data("aws_access_key_id = " + awsKey + "\n")
			extra = append(extra, fmt.Sprintf("M 100644 :%d config/credentials.ini", mark))
		}
		if awsKey != "" && i == 7 {
			extra = append(extra, "D config/credentials.ini")
		}
		mark++
		commit := mark
		fmt.Fprintf(&b, "commit refs/heads/main\nmark :%d\n", commit)
		fmt.Fprintf(&b, "author Dev Tester <dev@example.com> %s\ncommitter Dev Tester <dev@example.com> %s\n", when, when)
		data(fmt.Sprintf("commit %d\n", i))
		if prev != 0 {
			fmt.Fprintf(&b, "from :%d\n", prev)
		}
		fmt.Fprintf(&b, "M 100644 :%d %s\n", blob, rel)
		for _, e := range extra {
			b.WriteString(e + "\n")
		}
		b.WriteString("\n")
		prev = commit
	}
	b.WriteString("done\n")
	if err := git(dir, env, b.Bytes(), "fast-import", "--quiet", "--done"); err != nil {
		return err
	}
	return git(dir, env, nil, "reset", "--hard", "main", "--quiet")
}

// noPlaceholders fails if a file name or a file's content outside .git holds
// a {name} placeholder.
func noPlaceholders(home string) error {
	return filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if placeholder.MatchString(d.Name()) {
			return fmt.Errorf("a placeholder in a name: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if m := placeholder.Find(b); m != nil {
			return fmt.Errorf("a placeholder %s in %s", m, p)
		}
		return nil
	})
}

// countRepos counts the immediate subfolders of projects that hold a .git.
func countRepos(projects string) (int, error) {
	entries, err := os.ReadDir(projects)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(projects, e.Name(), ".git")); e.IsDir() && err == nil {
			n++
		}
	}
	return n, nil
}

// addPaperclip writes a fake Paperclip under <home>/.paperclip: config.json in
// its default mode (local_trusted, loopback, sign-up open), secrets/master.key
// next to db/, the agent JWT secret in .env, and cli/install.json at
// 2026.415.0, the version before 2026.416.0 fixed CVE-2026-41679. The secret
// values are fake and fixed; RosmarOps must never read them.
func addPaperclip(home string) error {
	inst := filepath.Join(home, ".paperclip", "instances", "default")
	files := map[string]string{
		filepath.Join(inst, "config.json"):                       `{"$meta":{"version":1},"server":{"deploymentMode":"local_trusted","exposure":"private","bind":"loopback","host":"127.0.0.1","port":3100},"database":{"mode":"embedded-postgres"}}` + "\n",
		filepath.Join(inst, "secrets", "master.key"):             "FAKEpaperclipMASTERkeyFORjourneysONLY0000\n",
		filepath.Join(inst, ".env"):                              "PAPERCLIP_AGENT_JWT_SECRET=FAKEpaperclipJWTsecretFORjourneysONLY0000\n",
		filepath.Join(inst, "db", "PG_VERSION"):                  "16\n",
		filepath.Join(home, ".paperclip", "cli", "install.json"): `{"version":"2026.415.0","source":"install.sh","channel":"stable"}` + "\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}
