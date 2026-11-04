package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-176: when the connect offer at the end of a double click ends in
// connected, the program sends this run's results package right then, by the
// normal send path, and the console says the result.

// bv176Setup lays out a fake home with config, a NOT-connected state and a kit
// with a credential, so the double click reaches the package step, writes a
// real package, and (not connected yet) leaves it unsent until the offer
// connects. It returns the state path and the reports directory.
func bv176Setup(t *testing.T) (stPath, reportDir string) {
	t.Helper()
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
	st.SpacePermission = false // not connected yet: the offer will connect
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
	return filepath.Join(rosmaDir, "state.json"), filepath.Join(rosmaDir, "reports")
}

// bv176Stubs installs the common seams and returns a pointer to the send-call
// counter and the captured console output. sendStatus is what the stubbed
// network returns.
func bv176Stubs(t *testing.T, stPath string, sendStatus int, sends *int) *bytes.Buffer {
	t.Helper()
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	prevSend, prevHook, prevOut := sendPackage, connectOfferHook, kitOutput
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
		sendPackage, connectOfferHook, kitOutput = prevSend, prevHook, prevOut
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0

	var out bytes.Buffer
	kitOutput = &out

	sendPackage = func(context.Context, spacenet.Permission, string, []byte, []byte) (int, error) {
		*sends++
		return sendStatus, nil
	}
	// The connect offer "connects": record the space permission in the state,
	// exactly as a real connect does, so the program is now connected and the
	// send proceeds. The credential is already in the kit store.
	connectOfferHook = func(_ spacePaths, _ catalog.Catalog) connectResult {
		cur, err := state.LoadState(stPath)
		if err != nil {
			t.Fatal(err)
		}
		cur.SpacePermission = true
		if err := state.SaveState(stPath, cur); err != nil {
			t.Fatal(err)
		}
		return connectConnected
	}
	return &out
}

func TestConnectSendsThisRunsPackage(t *testing.T) {
	stPath, reportDir := bv176Setup(t)
	sends := 0
	out := bv176Stubs(t, stPath, 201, &sends) // 201 Created: accepted

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	// Exactly one send: this run's package, sent the moment the offer connected
	// (runWith's own send did nothing while not connected).
	if sends != 1 {
		t.Fatalf("sendPackage called %d times, want 1", sends)
	}
	// The send log records it as sent.
	log, err := os.ReadFile(filepath.Join(reportDir, SendsLogName))
	if err != nil {
		t.Fatalf("no sends log: %v", err)
	}
	if !strings.Contains(string(log), " "+sendSent+" ") {
		t.Errorf("sends log has no accepted send:\n%s", log)
	}
	// The console says it was sent (report.space_sent), with today's date.
	cat, _ := catalog.Load("en")
	want := cat.Text("report.space_sent", map[string]string{"date": time.Now().In(time.Local).Format("2006-01-02")})
	if !strings.Contains(out.String(), want) {
		t.Errorf("console did not confirm the send.\ngot:  %q\nwant substring: %q", out.String(), want)
	}
}

func TestConnectSendRejectedChangesNothingElse(t *testing.T) {
	stPath, reportDir := bv176Setup(t)
	sends := 0
	out := bv176Stubs(t, stPath, 413, &sends) // 413: too large -> rejected (terminal, keeps the space)

	htmlPath := filepath.Join(reportDir, report.HTMLFileName)

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	before, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("no report.html: %v", err)
	}
	if sends != 1 {
		t.Fatalf("sendPackage called %d times, want 1", sends)
	}
	// The console says it could not be sent (report.space_rejected).
	cat, _ := catalog.Load("en")
	if want := cat.Text("report.space_rejected", nil); !strings.Contains(out.String(), want) {
		t.Errorf("console did not report the rejection.\ngot: %q\nwant substring: %q", out.String(), want)
	}
	// Nothing else changes: the already-open report is not rewritten after the
	// rejected send, and 413 keeps the space connected (next review retries).
	after, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the report was rewritten after the send")
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if !st.SpacePermission {
		t.Error("a 413 rejection dropped the space permission; it should keep it")
	}
}

// TestConnectNoSendWhenDeclined: the trust guarantee holds — with no connect
// (the person declines), nothing is sent.
func TestConnectNoSendWhenDeclined(t *testing.T) {
	stPath, _ := bv176Setup(t)
	sends := 0
	bv176Stubs(t, stPath, 201, &sends)
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { return connectDeclined }

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if sends != 0 {
		t.Fatalf("declined to connect but sent %d package(s)", sends)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.SpacePermission {
		t.Error("declining connected the space")
	}
}
