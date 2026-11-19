package report_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/report"
)

// On Linux the default reports folder is ~/RosmarOps; elsewhere it stays
// ~/.rosmarops/reports.
func TestDefaultReportDirByOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := report.DefaultReportDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != report.DefaultReportDirIn(home) {
		t.Errorf("DefaultReportDir %q, DefaultReportDirIn %q", got, report.DefaultReportDirIn(home))
	}
	if want := report.LegacyReportDir(home); filepath.Base(got) != "RosmarOps" && got != want {
		t.Errorf("default %q", got)
	}
}

// The old folder moves whole to ~/RosmarOps on Linux, once: data/ included;
// never when ~/RosmarOps exists already, never without the old folder, never
// on another system.
func TestMoveOldReports(t *testing.T) {
	seed := func(t *testing.T) string {
		home := t.TempDir()
		old := report.LegacyReportDir(home)
		if err := os.MkdirAll(filepath.Join(old, report.DataSubdir), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"report.html", filepath.Join(report.DataSubdir, "findings-memory.json")} {
			if err := os.WriteFile(filepath.Join(old, f), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return home
	}
	t.Run("moves once", func(t *testing.T) {
		home := seed(t)
		moved, both, err := report.MoveOldReports("linux", home)
		if err != nil || !moved || both {
			t.Fatalf("moved %v both %v err %v", moved, both, err)
		}
		if _, err := os.Stat(filepath.Join(home, "RosmarOps", report.DataSubdir, "findings-memory.json")); err != nil {
			t.Errorf("data/ not moved: %v", err)
		}
		if _, err := os.Stat(report.LegacyReportDir(home)); !os.IsNotExist(err) {
			t.Errorf("old folder still there: %v", err)
		}
		if moved, both, err := report.MoveOldReports("linux", home); moved || both || err != nil {
			t.Errorf("second call: moved %v both %v err %v", moved, both, err)
		}
	})
	t.Run("both exist", func(t *testing.T) {
		home := seed(t)
		if err := os.Mkdir(filepath.Join(home, "RosmarOps"), 0o700); err != nil {
			t.Fatal(err)
		}
		moved, both, err := report.MoveOldReports("linux", home)
		if err != nil || moved || !both {
			t.Fatalf("moved %v both %v err %v", moved, both, err)
		}
		if _, err := os.Stat(filepath.Join(report.LegacyReportDir(home), "report.html")); err != nil {
			t.Errorf("the old folder was touched: %v", err)
		}
	})
	t.Run("no old folder", func(t *testing.T) {
		home := t.TempDir()
		if moved, both, err := report.MoveOldReports("linux", home); moved || both || err != nil {
			t.Errorf("moved %v both %v err %v", moved, both, err)
		}
		if _, err := os.Stat(filepath.Join(home, "RosmarOps")); !os.IsNotExist(err) {
			t.Errorf("~/RosmarOps created: %v", err)
		}
	})
	for _, goos := range []string{"windows", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			home := seed(t)
			if moved, both, err := report.MoveOldReports(goos, home); moved || both || err != nil {
				t.Errorf("moved %v both %v err %v", moved, both, err)
			}
		})
	}
}

// The package's name carries the first 8 hex of host_id, without «host_».
func TestResultsFileName(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	if got, want := report.ResultsFileName(now, "host_d662f5eb0123456789abcdef"), "rosmarops-results-2026-11-19-d662f5eb.locked.json"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if got := report.ResultsFileName(now, "host_abc"); got != "rosmarops-results-2026-11-19-abc.locked.json" {
		t.Errorf("short id: %q", got)
	}
}

// Both the score and its factory reference at 0: neither the reference nor
// the comparison is shown; otherwise both are, as before.
func TestSaturatedScoreHidesReference(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		ref := htmlEscape(cat.Text("sheet.reference", nil))
		same := htmlEscape(cat.Text("sheet.reading.L4", nil))
		zero, forty := 0, 40
		v.Index.Machine.Value, v.Index.Machine.FactoryValue = &zero, &zero
		page := renderHTML(t, lang, v)
		if strings.Contains(page, `<div class="res-k">`+ref+`</div>`) || strings.Contains(page, same) {
			t.Errorf("%s: reference or comparison shown with both at 0", lang)
		}
		v.Index.Machine.Value, v.Index.Machine.FactoryValue = &forty, &forty
		page = renderHTML(t, lang, v)
		if !strings.Contains(page, `<div class="res-k">`+ref+`</div>`) || !strings.Contains(page, same) {
			t.Errorf("%s: reference or comparison missing with both at 40", lang)
		}
	}
}

// A project that ran out of time makes the findings' count a lower bound,
// with how many projects did not finish.
func TestCountWhenProjectsDidNotFinish(t *testing.T) {
	g := v2Graph(t)
	timeout := func(repo string) finding.Finding {
		return finding.Finding{ID: "scan_timeout:" + repo, Severity: finding.SeverityYellow, Category: "scan_timeout", Evidence: finding.Evidence{RepoPath: repo}}
	}
	for _, c := range []struct {
		name string
		fs   []finding.Finding
		key  string
		k    int
	}{
		{"one", []finding.Finding{timeout("/src/a")}, "findings.count.at_least.one_project", 1},
		{"two", []finding.Finding{timeout("/src/a"), timeout("/src/b")}, "findings.count.at_least", 2},
	} {
		v := buildV2(t, &g, report.Build("t", c.fs, nil, 0, 0, 0, 0, nil))
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			found := false
			for n := 0; n < 50; n++ {
				want := cat.Text(c.key, map[string]string{"n": strconv.Itoa(n), "k": strconv.Itoa(c.k)})
				if strings.Contains(page, `<em>`+htmlEscape(want)+`</em>`) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %s: no %s in the sheet", c.name, lang, c.key)
			}
			if strings.Contains(visibleText(page), "{k}") || strings.Contains(visibleText(page), "{n}") {
				t.Errorf("%s %s: placeholder left", c.name, lang)
			}
		}
	}
	// No time-out: the plain count, never «at least».
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	cat, _ := catalog.Load("en")
	if at := strings.SplitN(cat.Text("findings.count.at_least", nil), "{n}", 2)[0]; strings.Contains(renderHTML(t, "en", v), "<em>"+at) {
		t.Error("«at least» without a time-out")
	}
}
