package trust_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// The report does not say «RosmarOps will ask at the end of the next scan»
// when it asks in this very review (BV-285, as the problem list does). With
// someone at a terminal and no answer, the report has no usage line and the
// console asks; Enter alone then records nothing, so the question stays for
// the next review. A scheduled review cannot ask, and its report says so.
func TestUsageReportLineWhenAskedNow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the pseudo-terminal is implemented for Linux")
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	undecided := cat.Text("usage.state.undecided", nil)
	prompt := cat.Text("usage.ask.prompt", nil)
	bin := buildBin(t, "./cmd/rosmarops", publishedBuild)
	env := func(home string) []string {
		return append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
			"HTTPS_PROXY="+sandboxDeadProxy, "https_proxy="+sandboxDeadProxy, "NO_PROXY=", "no_proxy=")
	}
	page := func(home string) string {
		raw, err := os.ReadFile(filepath.Join(report.DefaultReportDirIn(home), "report.en.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	// At a terminal: asked now, so no «next scan» line; Enter leaves it for later.
	home := usageHome(t, nil)
	out := runInPTY(t, &sandbox{work: home, env: env(home)}, []string{bin, "run", "--lang", "en"}, strings.Repeat("\n", 20))
	if !strings.Contains(out, prompt) {
		t.Fatalf("the usage question was not asked:\n%s", out)
	}
	if strings.Contains(page(home), undecided) {
		t.Error("the report says it will ask at the next scan while it asks in this one")
	}
	st, err := state.LoadState(filepath.Join(home, ".rosmarops", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Usage != nil {
		t.Errorf("Enter alone recorded an answer: %+v", st.Usage)
	}

	// Scheduled: not asked, and the report says when it will be.
	home = usageHome(t, nil)
	cmd := exec.Command(bin, "run", "--unattended", "--lang", "en")
	cmd.Dir, cmd.Env = home, env(home)
	_ = cmd.Run()
	if !strings.Contains(page(home), undecided) {
		t.Error("a scheduled review's report lacks the «off until you answer» line")
	}
}
