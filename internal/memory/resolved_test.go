package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BV-273: an open finding gone from a repository is resolved only when that
// repository finished its check in this run; otherwise it is not checked and
// stays open.

const (
	repoA = "/home/ana/src/shop-api"
	repoB = "/home/ana/src/landing"
)

func repoFnd(id, repo string) Finding {
	f := fnd(id)
	f.Rule = "cred_in_git"
	f.Repos = []string{repo}
	return f
}

func completeRun(day string, repos ...string) Run {
	r := run(day)
	r.ReposComplete = repos
	return r
}

func TestResolvedOnlyWhereChecked(t *testing.T) {
	base, _, _ := Update(File{}, completeRun("2026-11-01", repoA, repoB),
		[]Finding{repoFnd("f_a", repoA), repoFnd("f_b", repoB)}, salt)

	// A real fix: both repositories finished, both findings are gone.
	_, _, root := Update(base, completeRun("2026-11-08", repoA, repoB), nil, salt)
	if len(root.Resolved) != 2 || root.Counts[VerdictNotChecked] != 0 || len(root.NoLongerObserved) != 0 {
		t.Errorf("fixed: %+v", root)
	}

	// repoB timed out (or was deleted): it is not in repos_complete, so its
	// finding is not checked and stays open; repoA's is resolved.
	m, _, root := Update(base, completeRun("2026-11-08", repoA), nil, salt)
	if len(root.Resolved) != 1 || root.Counts[VerdictNotChecked] != 1 || len(root.NoLongerObserved) != 0 {
		t.Fatalf("one repository not checked: %+v", root)
	}
	var open []string
	for _, e := range m.Entries {
		if e.Open {
			open = append(open, e.LastVerdict)
		}
	}
	if len(open) != 1 || open[0] != VerdictNotChecked {
		t.Errorf("open entries: %v, want one not_checked", open)
	}
	// Next time repoB finishes and the finding is still there: it was never
	// closed, so it is still observed with its first date.
	_, v, _ := Update(m, completeRun("2026-11-15", repoA, repoB), []Finding{repoFnd("f_b", repoB)}, salt)
	if v["f_b"].Verdict != VerdictStillObserved || v["f_b"].OpenSince != "2026-11-01" {
		t.Errorf("after the gap: %+v", v["f_b"])
	}
}

// An agent's finding is resolved only while the agent is still here.
func TestResolvedNeedsTheAgent(t *testing.T) {
	base, _, _ := Update(File{}, completeRun("2026-11-01"), []Finding{fnd("f_a")}, salt)
	r := completeRun("2026-11-08")
	r.Present = nil // the agent was uninstalled: its finding was not checked
	_, _, root := Update(base, r, nil, salt)
	if len(root.Resolved) != 0 || root.Counts[VerdictNotChecked] != 1 {
		t.Errorf("agent gone: %+v", root)
	}
}

// A v3 memory has no repos per entry and no repos_complete: nothing it holds
// can be resolved in the first v4 run; it is no longer observed.
func TestV3MemoryNeverResolves(t *testing.T) {
	base, _, _ := Update(File{}, completeRun("2026-11-01", repoA), []Finding{repoFnd("f_a", repoA), fnd("f_b")}, salt)
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	// Strip what v4 adds, as a v3 file would be.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["schema_version"] = 3
	delete(m["last_checked"].(map[string]any), "repos_complete")
	for _, e := range m["entries"].([]any) {
		delete(e.(map[string]any), "repos")
	}
	dir := t.TempDir()
	v3, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, FileName), v3, 0o600); err != nil {
		t.Fatal(err)
	}
	prev, reset, err := Read(dir)
	if err != nil || reset {
		t.Fatalf("read v3: reset %v, %v", reset, err)
	}
	next, _, root := Update(prev, completeRun("2026-11-08", repoA), nil, salt)
	if len(root.Resolved) != 0 || len(root.NoLongerObserved) != 2 {
		t.Errorf("first v4 run after v3: resolved %d, no longer observed %d", len(root.Resolved), len(root.NoLongerObserved))
	}
	if err := Write(dir, next); err != nil {
		t.Fatalf("write v4: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(out), `"schema_version": 4`) || !strings.Contains(string(out), `"repos_complete"`) {
		t.Errorf("written memory is not v4:\n%s", out)
	}
}

// The memory holds no repository path: only salted references.
func TestReposAreReferences(t *testing.T) {
	m, _, _ := Update(File{}, completeRun("2026-11-01", repoA), []Finding{repoFnd("f_a", repoA)}, salt)
	dir := t.TempDir()
	if err := Write(dir, m); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, leak := range []string{"shop-api", "/home/ana", "src"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("memory holds %q:\n%s", leak, out)
		}
	}
	want := ref(salt, repoA)
	if !strings.Contains(string(out), want) {
		t.Errorf("no repository reference %s in:\n%s", want, out)
	}
}
