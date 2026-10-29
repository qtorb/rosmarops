package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// BV-107b: a run with a project hook that runs a repository script (the
// JFrog hook, project_hook_runs_repo_script) finishes: exit 0, the findings
// memory written and valid against its schema (the rule is not tracked in
// it), the state saved, and the finding in report.v2.json with a
// not_comparable verdict. Before, the memory's schema rejected the rule and
// the run stopped with exit 1 before saving its state.
func TestRunWithProjectHookWritesMemory(t *testing.T) {
	bin, work, home, env := integrityEnv(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	repo := filepath.Join(home, "projects", "app")
	write(t, filepath.Join(repo, ".claude", "settings.json"),
		`{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"node .github/init.js || bun .github/init.js"}]}]}}`)
	write(t, filepath.Join(repo, ".github", "init.js"), `console.log("init");`+"\n")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if code := rosmarops(t, bin, work, env, "init", "--repos-under", filepath.Join(home, "projects")); code != 0 {
		t.Fatalf("init: exit %d", code)
	}
	if code := rosmarops(t, bin, work, env, "run", "--lang", "en"); code != 0 {
		t.Fatalf("run: exit %d, want 0", code)
	}

	reports := filepath.Join(home, ".rosmarops", "reports")
	raw, err := os.ReadFile(filepath.Join(reports, memory.FileName))
	if err != nil {
		t.Fatalf("findings memory not written: %v", err)
	}
	schema, err := schemaval.Compile(schemas.FindingsMemoryV3Schema)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := schema.Validate(raw)
	if err != nil || !valid.Valid() {
		t.Fatalf("findings memory not valid against its schema: %v %v", err, valid.Errors())
	}

	var st struct {
		LastRun string `json:"last_run"`
	}
	readJSON(t, filepath.Join(home, ".rosmarops", "state.json"), &st)
	if st.LastRun == "" || st.LastRun == "0001-01-01T00:00:00Z" {
		t.Errorf("state not saved by the run: last_run %q", st.LastRun)
	}

	var v2 struct {
		Findings []struct {
			Rule   string `json:"rule"`
			Memory struct {
				Verdict string `json:"verdict"`
			} `json:"memory"`
		} `json:"findings"`
	}
	readJSON(t, filepath.Join(reports, "report.v2.json"), &v2)
	found := false
	for _, f := range v2.Findings {
		if f.Rule == "project_hook_runs_repo_script" {
			found = true
			if f.Memory.Verdict != memory.VerdictNotComparable {
				t.Errorf("verdict %q, want %q", f.Memory.Verdict, memory.VerdictNotComparable)
			}
		}
	}
	if !found {
		t.Error("no project_hook_runs_repo_script finding in report.v2.json")
	}
	var mem struct {
		Entries []struct {
			Rule string `json:"rule"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &mem); err != nil {
		t.Fatal(err)
	}
	for _, e := range mem.Entries {
		if e.Rule == "project_hook_runs_repo_script" {
			t.Error("the findings memory tracks project_hook_runs_repo_script")
		}
	}
}
