package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// TestDoubleClickPackageFailDoesNotKillReview is BV-152 point 1/5: on a computer
// connected to a space, a failure to build or seal this run's results package no
// longer stops the review. The report is still written (the normal report.html,
// not the red "could not finish" page), the run exits 0, and the «Your space»
// section says the check couldn't be sent (report.space_rejected).
func TestDoubleClickPackageFailDoesNotKillReview(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(root, strings.ToLower(name))
		if name == "HOME" || name == "USERPROFILE" {
			dir = home
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
	t.Chdir(root)

	// Config, a connected state and a kit with a credential: the double click
	// skips first-use init and reaches the package step.
	rosmaDir := filepath.Join(home, ".rosmarops")
	if err := os.MkdirAll(rosmaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(filepath.Join(rosmaDir, "config.json"), config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SpacePermission = true
	if err := state.SaveState(filepath.Join(rosmaDir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
	salt, _ := st.Salt()
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kitID, _ := kit.NewKitID()
	cred := make([]byte, 32)
	rand.Read(cred)
	storePath, _ := kit.StorePath()
	if err := kit.Save(storePath, kit.Stored{KitID: kitID, Secret: sec, SpaceCredential: cred}); err != nil {
		t.Fatal(err)
	}

	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	prevSend, prevWrite := sendPackage, writeResultsPkg
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
		sendPackage, writeResultsPkg = prevSend, prevWrite
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0
	sendPackage = func(context.Context, spacenet.Permission, string, []byte, []byte) (int, error) { return 200, nil }
	// The package fails exactly as the v0.7.1 bug did: an over-long field makes
	// the space view fail schema validation. The error carries only field paths.
	pkgCalled := false
	writeResultsPkg = func(report.V2, *graph.Graph, string, string, []byte, string, string, time.Time, string) (string, error) {
		pkgCalled = true
		return "", fmt.Errorf("space view fails schema validation:\n  findings.0.text.en.steps.0: String length must be less than or equal to 1000")
	}

	if code := runNoArgs(); code != 0 {
		t.Fatalf("the review did not finish: exit %d (want 0)", code)
	}
	if !pkgCalled {
		t.Fatal("the package step was not reached (built graph is nil?)")
	}

	reportDir := filepath.Join(rosmaDir, "reports")
	htmlPath := filepath.Join(reportDir, report.HTMLFileName)
	raw, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("no report.html written: %v", err)
	}
	page := string(raw)
	// The «Your space» section shows report.space_rejected (its apostrophe is
	// HTML-escaped in the page).
	if !strings.Contains(page, "This check couldn&#39;t be sent to your space.") {
		t.Error("the «Your space» section does not show report.space_rejected")
	}
	// It is the normal report: it still has the step-1 situation section, which
	// the red run-failed page does not.
	if !strings.Contains(page, `class="s1"`) {
		t.Error("report.html is not the normal report (no step-1 section)")
	}
}
