package repos_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// CTO-7 §2.2: with Identify, the tracked and history checks carry every
// committed secret as a salted reference, where it is (file and line in
// the tree; commit and file in the history), and whether .git/config
// declares a remote. The secret itself is never kept; the identify sees it
// once. The progress callback counts the repositories.
func TestScanAllRepos_CommittedSecrets(t *testing.T) {
	root := initGitRepo(t)
	tracked := "ghp_" + strings.Repeat("T", 36)
	history := "ghp_" + strings.Repeat("H", 36)
	writeFile(t, root, "app/deploy.sh", "a\nexport GH="+tracked+"\n")
	writeFile(t, root, "notes/old.txt", "token "+history+"\n")
	gitCommit(t, root, "one", "app/deploy.sh", "notes/old.txt")
	cmd := exec.Command("git", "rm", "-q", "notes/old.txt")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rm: %v\n%s", err, out)
	}
	gitCommit(t, root, "two")
	cmd = exec.Command("git", "remote", "add", "origin", "https://example.invalid/r.git")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v\n%s", err, out)
	}

	seen := map[string]int{}
	var progress []int
	cfg := repos.ScanConfig{
		Patterns: secrets.BuiltinPatterns(),
		Identify: func(patternID, secret string) secrets.Ref {
			seen[secret]++
			return secrets.Ref{Fingerprint: "fp:" + secret[len(secret)-4:], Service: "github"}
		},
		OnProgress: func(done, total int) { progress = append(progress, done, total) },
	}
	fs, _, _ := repos.ScanAllRepos(context.Background(), []string{root}, nil, cfg)
	type got struct {
		presence, file, fp string
		line               int
		remote             bool
	}
	var cs []got
	for _, f := range fs {
		for _, c := range f.Committed {
			if c.Repo != root {
				t.Errorf("repo %q", c.Repo)
			}
			if c.Presence == finding.PresenceHistoryOnly && len(c.Commit) != 40 {
				t.Errorf("commit %q", c.Commit)
			}
			cs = append(cs, got{c.Presence, c.File, c.Ref.Fingerprint, c.Line, c.HasRemote})
		}
	}
	want := map[got]bool{
		{finding.PresenceTracked, "app/deploy.sh", "fp:TTTT", 2, true}:     false,
		{finding.PresenceHistoryOnly, "app/deploy.sh", "fp:TTTT", 0, true}: false,
		{finding.PresenceHistoryOnly, "notes/old.txt", "fp:HHHH", 0, true}: false,
	}
	for _, c := range cs {
		if _, ok := want[c]; !ok {
			t.Errorf("unexpected %+v", c)
		}
		want[c] = true
	}
	for c, ok := range want {
		if !ok {
			t.Errorf("missing %+v", c)
		}
	}
	if seen[tracked] == 0 || seen[history] == 0 {
		t.Errorf("identify saw %v", seen)
	}
	if len(progress) != 4 || progress[0] != 0 || progress[2] != 1 || progress[3] != 1 {
		t.Errorf("progress %v", progress)
	}
	// Without Identify nothing is carried (the v1 findings stay as they were).
	plain, _, _ := repos.ScanAllRepos(context.Background(), []string{root}, nil, defaultCfg())
	for _, f := range plain {
		if len(f.Committed) > 0 {
			t.Fatalf("committed without identify: %+v", f)
		}
	}
}
