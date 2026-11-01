package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// CTO-15 PR2: a connection that does not end stored reverts the permission, so
// the computer is left "not connected". A successful one stores the key and
// credential and records the permission.
func TestSpaceConnectRevertsOnFailure(t *testing.T) {
	newPaths := func(t *testing.T) spacePaths {
		dir := t.TempDir()
		sp := spacePaths{store: filepath.Join(dir, "kit.key"), state: filepath.Join(dir, "state.json"), reports: filepath.Join(dir, "reports")}
		st, err := state.DefaultState("1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if err := state.SaveState(sp.state, st); err != nil {
			t.Fatal(err)
		}
		return sp
	}

	t.Run("expired reverts permission", func(t *testing.T) {
		sp := newPaths(t)
		orig := connectProtocol
		t.Cleanup(func() { connectProtocol = orig })
		connectProtocol = func(context.Context, spacenet.Permission, string, func(int), func(string) error) (spacenet.Material, func(context.Context) error, error) {
			return spacenet.Material{}, nil, spacenet.ErrConnectExpired
		}
		err := spaceConnect(context.Background(), sp, func(int) {}, func(string) error { return nil })
		if err != spacenet.ErrConnectExpired {
			t.Fatalf("err = %v, want ErrConnectExpired", err)
		}
		st, _ := state.LoadState(sp.state)
		if st.SpacePermission {
			t.Error("permission not reverted after an expired connection")
		}
		if isSpaceConnected(sp) {
			t.Error("isSpaceConnected true after a failed connect")
		}
	})

	t.Run("success stores key and credential", func(t *testing.T) {
		sp := newPaths(t)
		salt := make([]byte, kit.SaltSize)
		rand.Read(salt)
		sec, err := kit.NewSecret(salt)
		if err != nil {
			t.Fatal(err)
		}
		kidRaw := make([]byte, 16)
		rand.Read(kidRaw)
		cred := make([]byte, 32)
		rand.Read(cred)
		orig := connectProtocol
		t.Cleanup(func() { connectProtocol = orig })
		acked := false
		connectProtocol = func(context.Context, spacenet.Permission, string, func(int), func(string) error) (spacenet.Material, func(context.Context) error, error) {
			return spacenet.Material{Secret: sec, KitID: hex.EncodeToString(kidRaw), Credential: cred},
				func(context.Context) error { acked = true; return nil }, nil
		}
		if err := spaceConnect(context.Background(), sp, func(int) {}, func(string) error { return nil }); err != nil {
			t.Fatalf("spaceConnect: %v", err)
		}
		if !acked {
			t.Error("ack not called on success")
		}
		if !isSpaceConnected(sp) {
			t.Error("isSpaceConnected false after a successful connect")
		}
		stored, err := kit.Load(sp.store)
		if err != nil || len(stored.SpaceCredential) != 32 {
			t.Errorf("credential not stored: %v", err)
		}
	})
}

// CTO-15 PR2 end-to-end: a full double-click run whose send gets a 401 leaves
// the computer "not connected" — no permission and no credential — so the next
// double click offers the window again. This is the regression for runWith
// re-writing the permission it loaded at the start.
func TestDoubleClick401ForgetsSpace(t *testing.T) {
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

	// Seed config, a connected state (permission true) and a kit with a
	// credential, so the double click skips first-use init and the run sends.
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
	prevSend := sendPackage
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
		sendPackage = prevSend
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0
	sendPackage = func(context.Context, spacenet.Permission, string, []byte, []byte) (int, error) { return 401, nil }

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}

	gotSt, err := state.LoadState(filepath.Join(rosmaDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if gotSt.SpacePermission {
		t.Error("permission survived a 401 during the run (runWith clobbered the wipe)")
	}
	stored, err := kit.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SpaceCredential) != 0 {
		t.Error("credential survived a 401 during the run")
	}
	sp := spacePaths{store: storePath, state: filepath.Join(rosmaDir, "state.json"), reports: filepath.Join(rosmaDir, "reports")}
	if isSpaceConnected(sp) {
		t.Error("still 'connected' after a 401; the next double click would not offer the window")
	}
}
