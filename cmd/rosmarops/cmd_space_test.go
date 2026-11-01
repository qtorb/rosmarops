package main

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// sendMarker is seeded in each test package's sealed content; it must never
// appear in the sends log.
const sendMarker = "LEAK-MARKER-9z"

// connectedSpace lays out a temp state (SpacePermission true) and a kit with a
// 32-byte send credential, and returns the paths and the kit secret so a test
// can seal real packages that pass the send filter.
func connectedSpace(t *testing.T) (spacePaths, kit.Secret) {
	t.Helper()
	dir := t.TempDir()
	paths := spacePaths{
		store:   filepath.Join(dir, "kit.key"),
		state:   filepath.Join(dir, "state.json"),
		reports: filepath.Join(dir, "reports"),
	}
	if err := os.MkdirAll(paths.reports, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SpacePermission = true
	if err := state.SaveState(paths.state, st); err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kitID, err := kit.NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	cred := make([]byte, 32)
	rand.Read(cred)
	if err := kit.Save(paths.store, kit.Stored{KitID: kitID, Secret: sec, SpaceCredential: cred}); err != nil {
		t.Fatal(err)
	}
	return paths, sec
}

// writePkg writes a real closed results package for the given date, sealed with
// sec (so it passes the send filter), its content carrying sendMarker.
func writePkg(t *testing.T, reports, date string, sec kit.Secret) string {
	t.Helper()
	kitID, err := kit.NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := results.Seal(sec.Key, results.Header{
		Format: "rosmarops-results", Version: 1, KitID: kitID, KeyID: kit.KeyID(sec.Key),
		HostID: "host_abc0000000000", CreatedAt: "2026-11-01T00:00:00Z", RosmaropsVersion: "1.0.0",
	}, []byte(`{"content":"`+sendMarker+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := results.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(reports, "rosmarops-results-"+date+"-host_abc.locked.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func stubSend(t *testing.T, status int, bodies *[][]byte) {
	t.Helper()
	orig := sendPackage
	t.Cleanup(func() { sendPackage = orig })
	sendPackage = func(_ context.Context, _ spacenet.Permission, _ string, _, body []byte) (int, error) {
		if bodies != nil {
			*bodies = append(*bodies, append([]byte(nil), body...))
		}
		return status, nil
	}
}

// Point 6: a successful send logs one "sent" line with the filename, size and
// host, sends the package body verbatim, and the log never holds the content.
func TestSpaceSend_Sent(t *testing.T) {
	paths, sec := connectedSpace(t)
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	want, _ := os.ReadFile(cur)
	var bodies [][]byte
	stubSend(t, 201, &bodies)

	spaceSend(context.Background(), paths, cur, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))

	if len(bodies) != 1 || string(bodies[0]) != string(want) {
		t.Fatalf("sent %d bodies; want 1 equal to the package", len(bodies))
	}
	log := readLog(t, paths.reports)
	if !strings.Contains(log, filepath.Base(cur)) || !strings.Contains(log, sendSent) ||
		!strings.Contains(log, spacenet.Host) || !strings.Contains(log, "2026-11-01T10:00:00Z") {
		t.Errorf("sends log line missing fields:\n%s", log)
	}
	if strings.Contains(log, sendMarker) {
		t.Errorf("sends log leaked package content:\n%s", log)
	}
}

// Point 6: a 401 forgets the permission and the credential and stops.
func TestSpaceSend_Unauthorized_Forgets(t *testing.T) {
	paths, sec := connectedSpace(t)
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	stubSend(t, 401, nil)

	spaceSend(context.Background(), paths, cur, time.Now())

	st, err := state.LoadState(paths.state)
	if err != nil {
		t.Fatal(err)
	}
	if st.SpacePermission {
		t.Error("permission not forgotten after 401")
	}
	stored, err := kit.Load(paths.store)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SpaceCredential) != 0 {
		t.Error("credential not forgotten after 401")
	}
	if !strings.Contains(readLog(t, paths.reports), sendUnauthorized) {
		t.Error("401 not logged")
	}
}

// Point 6: an already-sent package is not sent again, but the current run's
// package always is.
func TestSpaceSend_DedupeKeepsCurrent(t *testing.T) {
	paths, sec := connectedSpace(t)
	old := writePkg(t, paths.reports, "2026-10-30", sec)
	// Pre-log the old package as sent.
	appendSendLog(filepath.Join(paths.reports, SendsLogName), time.Now(), filepath.Base(old), 3, sendSent)
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	want, _ := os.ReadFile(cur)
	var bodies [][]byte
	stubSend(t, 201, &bodies)

	spaceSend(context.Background(), paths, cur, time.Now())

	if len(bodies) != 1 || string(bodies[0]) != string(want) {
		t.Fatalf("expected only the current package sent; got %d bodies", len(bodies))
	}
}

// Point 6: offline (a network error) leaves the package for next time — logged
// deferred, permission kept.
func TestSpaceSend_DeferredKeepsPermission(t *testing.T) {
	paths, sec := connectedSpace(t)
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	orig := sendPackage
	t.Cleanup(func() { sendPackage = orig })
	sendPackage = func(_ context.Context, _ spacenet.Permission, _ string, _, _ []byte) (int, error) {
		return 0, context.DeadlineExceeded
	}
	spaceSend(context.Background(), paths, cur, time.Now())
	st, _ := state.LoadState(paths.state)
	if !st.SpacePermission {
		t.Error("permission wrongly forgotten on a transient error")
	}
	if !strings.Contains(readLog(t, paths.reports), sendDeferred) {
		t.Error("deferred not logged")
	}
}

// Without permission, spaceSend does nothing and makes no send.
func TestSpaceSend_NoPermissionNoSend(t *testing.T) {
	paths, sec := connectedSpace(t)
	st, _ := state.LoadState(paths.state)
	st.SpacePermission = false
	if err := state.SaveState(paths.state, st); err != nil {
		t.Fatal(err)
	}
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	called := false
	orig := sendPackage
	t.Cleanup(func() { sendPackage = orig })
	sendPackage = func(_ context.Context, _ spacenet.Permission, _ string, _, _ []byte) (int, error) {
		called = true
		return 201, nil
	}
	spaceSend(context.Background(), paths, cur, time.Now())
	if called {
		t.Error("spaceSend reached the network without permission")
	}
}

func readLog(t *testing.T, reports string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(reports, SendsLogName))
	if err != nil {
		return ""
	}
	return string(b)
}
