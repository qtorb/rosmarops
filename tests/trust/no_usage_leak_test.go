package trust_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/usage"
)

// The usage package never carries a path, a project, file,
// computer or user name, or a key, even when the review reads all of them.
// A home with Claude Code, a project with a recognisable name and a .env
// holding a recognisable fake key is reviewed with usage statistics on (the
// send fails at a closed local proxy, so nothing leaves); the package kept in
// rosmarops-usage-last.json and the sends log line carry none of them, while
// the package does carry what the review found (the agent and findings), so
// the test cannot pass on an empty package.
func TestUsagePackageCarriesNoSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sandboxed black-box run is implemented for Unix-like hosts")
	}
	bin := buildBin(t, "./cmd/rosmarops", nil)
	home := usageHome(t, usageAnswer(true))
	const (
		project = "zz-planted-project-name"
		secret  = "sk-proj-PLANTEDSECRETVALUE0123456789abcdefABCDEF0123456789"
		file    = "zz-planted-file-name.env"
	)
	repo := filepath.Join(home, "code", project)
	for _, dir := range []string{filepath.Join(repo, ".git"), filepath.Join(home, ".claude")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(repo, ".env"):                     "OPENAI_API_KEY=" + secret + "\n",
		filepath.Join(repo, file):                       "TOKEN=" + secret + "\n",
		filepath.Join(home, ".claude", "settings.json"): `{"permissions":{"defaultMode":"bypassPermissions"}}`,
		filepath.Join(home, ".claude.json"):             `{"mcpServers":{"zz-planted-mcp":{"command":"npx","args":["-y","zz-planted-package"],"env":{"KEY":"` + secret + `"}}}}`,
		filepath.Join(repo, ".git", "HEAD"):             "ref: refs/heads/main\n",
		filepath.Join(repo, ".git", "config"):           "[core]\n\trepositoryformatversion = 0\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "run", "--unattended")
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1")
	_, _ = cmd.CombinedOutput()

	data := report.DataDir(filepath.Join(report.DefaultReportDirIn(home)))
	raw, err := os.ReadFile(filepath.Join(data, usage.LastFileName))
	if err != nil {
		t.Fatalf("no %s: %v", usage.LastFileName, err)
	}
	log, err := os.ReadFile(filepath.Join(data, "rosmarops-sends.log"))
	if err != nil {
		t.Fatal(err)
	}
	var usageLines []string
	for _, l := range strings.Split(string(log), "\n") {
		if strings.Contains(l, usage.SendsLabel) {
			usageLines = append(usageLines, l)
		}
	}
	planted := []string{home, repo, project, file, secret, secret[:12], "zz-planted-mcp", "zz-planted-package", "PLANTEDSECRET"}
	if h, err := os.Hostname(); err == nil && len(h) >= 4 {
		planted = append(planted, h)
	}
	if u, err := user.Current(); err == nil && len(u.Username) >= 5 {
		planted = append(planted, u.Username)
	}
	for _, out := range []string{string(raw), strings.Join(usageLines, "\n")} {
		for _, p := range planted {
			if strings.Contains(out, p) {
				t.Errorf("%q leaked into %q", p, out)
			}
		}
	}
	var p usage.Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Agents) == 0 || len(p.Findings) == 0 {
		t.Errorf("the package carries nothing of the review (agents %v, findings %v): the test is empty", p.Agents, p.Findings)
	}
	t.Logf("package: %s", raw)
}
