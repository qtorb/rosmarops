package repos

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

// BV-273: a repository whose check finished is reported as completed, and
// only once.
func TestScanAllRepos_OnCompleted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	gitT(t, r, "commit", "-q", "--allow-empty", "-m", "init")
	var completed []string
	ScanAllRepos(context.Background(), []string{r}, nil, ScanConfig{OnCompleted: func(roots []string) { completed = roots }})
	if !reflect.DeepEqual(completed, []string{r}) {
		t.Errorf("completed: %v, want [%s]", completed, r)
	}
}
