package main

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-172: a double click on a connected computer whose space was reset. The
// send reaches the key check and is refused (409 key_mismatch) — or the
// credential is gone (401 no_credential). Either way the program forgets the
// space before building the report, the report shows as not connected and says
// the check could not be sent, and the connect offer appears once in this same
// review, with no 7-day wait.
func TestBV172_RejectForgetsAndReoffers(t *testing.T) {
	for _, status := range []int{409, 401} {
		t.Run(map[int]string{409: "key_mismatch", 401: "no_credential"}[status], func(t *testing.T) {
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
			st, _ := state.DefaultState("1.0.0")
			st.SpacePermission = true
			// A recent «Not now» that, without the fix, would suppress the offer.
			st.ConnectOfferDeclinedAt = "2026-11-04T09:00:00Z"
			if err := state.SaveState(filepath.Join(rosmaDir, "state.json"), st); err != nil {
				t.Fatal(err)
			}
			salt, _ := st.Salt()
			sec, _ := kit.NewSecret(salt)
			kitID, _ := kit.NewKitID()
			cred := make([]byte, 32)
			rand.Read(cred)
			storePath, _ := kit.StorePath()
			if err := kit.Save(storePath, kit.Stored{KitID: kitID, Secret: sec, SpaceCredential: cred}); err != nil {
				t.Fatal(err)
			}

			prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
			prevSend, prevHook := sendPackage, connectOfferHook
			t.Cleanup(func() {
				systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
				sendPackage, connectOfferHook = prevSend, prevHook
			})
			systemLang = func() string { return "en" }
			detectProjects = func() firstrun.Result { return firstrun.Result{} }
			pickFolder = func(string) (string, error) { return "", nil }
			openReportFile = func(string) error { return nil }
			closeDelay = 0
			sendPackage = func(context.Context, spacenet.Permission, string, []byte, []byte) (int, error) { return status, nil }
			offers := 0
			connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { offers++; return connectDeclined }

			if code := runNoArgs(); code != 0 {
				t.Fatalf("review did not finish: exit %d", code)
			}

			// The credential and the permission are forgotten.
			stored, _ := kit.Load(storePath)
			if len(stored.SpaceCredential) != 0 {
				t.Errorf("credential not forgotten after %d", status)
			}
			st2, _ := state.LoadState(filepath.Join(rosmaDir, "state.json"))
			if st2.SpacePermission {
				t.Errorf("permission not forgotten after %d", status)
			}
			// The offer was shown once in this same review despite the recent «Not
			// now»: the reject cleared the 7-day decline before the offer ran.
			if offers != 1 {
				t.Errorf("connect offer shown %d times, want 1 (the 7-day wait was not bypassed)", offers)
			}
			// The report is not connected, says it could not be sent, and offers to
			// create/connect a space.
			raw, err := os.ReadFile(filepath.Join(rosmaDir, "reports", report.HTMLFileName))
			if err != nil {
				t.Fatal(err)
			}
			page := string(raw)
			if strings.Contains(page, "Open your space") {
				t.Error("the report still shows the connected «Open your space» button")
			}
			if !strings.Contains(page, "Create your space") {
				t.Error("the report does not show the unconnected «Create your space» button")
			}
			if !strings.Contains(page, "couldn&#39;t be sent to your space") {
				t.Error("the report does not say the check could not be sent (report.space_rejected)")
			}
		})
	}
}
