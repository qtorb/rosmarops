package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-180: connecting after the space started again («Empezar de nuevo») must
// accept the space's new key instead of refusing. The kit this computer holds
// is replaced, and this run's package (sealed with the old key) is re-sealed
// with the new key so it is sent at once. Packages sealed with the old key are
// never sent.

// bv180Secret returns a fresh kit secret with a random salt (so two calls give
// two different keys).
func bv180Secret(t *testing.T) kit.Secret {
	t.Helper()
	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	return sec
}

// bv180KitID returns a valid kit_id (16 random bytes, 32 hex chars), as
// kit.Load requires.
func bv180KitID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// bv180Paths lays out a NOT-connected state (permission false, no credential in
// the kit, as a 409 key_mismatch leaves it) with a kit holding the old key.
func bv180Paths(t *testing.T, oldKitID string, oldKey kit.Secret) spacePaths {
	t.Helper()
	dir := t.TempDir()
	sp := spacePaths{
		store:   filepath.Join(dir, "kit.key"),
		state:   filepath.Join(dir, "state.json"),
		reports: filepath.Join(dir, "reports"),
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(sp.state, st); err != nil {
		t.Fatal(err)
	}
	if err := kit.Save(sp.store, kit.Stored{KitID: oldKitID, Secret: oldKey}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sp.reports, 0o700); err != nil {
		t.Fatal(err)
	}
	return sp
}

// bv180WritePackage seals content with key under hostID and date and writes the
// results package file, returning its path.
func bv180WritePackage(t *testing.T, reportsDir string, key []byte, kitID, hostID, date string, content []byte) string {
	t.Helper()
	h := results.Header{
		KitID: kitID, KeyID: kit.KeyID(key), HostID: hostID,
		CreatedAt: date + "T00:00:00Z", RosmaropsVersion: "0.7.8",
	}
	pkg, err := results.Seal(key, h, content)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := results.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	host := hostID
	if len(host) > 8 {
		host = host[:8]
	}
	path := filepath.Join(reportsDir, fmt.Sprintf("rosmarops-results-%s-%s.locked.json", date, host))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// bv180ConnectStub makes connectProtocol return the given material and a no-op
// ack, restoring the original on cleanup.
func bv180ConnectStub(t *testing.T, m spacenet.Material) {
	t.Helper()
	orig := connectProtocol
	t.Cleanup(func() { connectProtocol = orig })
	connectProtocol = func(context.Context, spacenet.Permission, string, func(int), func(string) error) (spacenet.Material, func(context.Context) error, error) {
		return m, func(context.Context) error { return nil }, nil
	}
}

// bv180SendStub captures the key_id of every package body sent and returns 201.
func bv180SendStub(t *testing.T, sentKeyIDs *[]string) {
	t.Helper()
	orig := sendPackage
	t.Cleanup(func() { sendPackage = orig })
	sendPackage = func(_ context.Context, _ spacenet.Permission, _ string, _ []byte, body []byte) (int, error) {
		p, err := results.Parse(body)
		if err != nil {
			t.Errorf("sent body is not a package: %v", err)
		}
		*sentKeyIDs = append(*sentKeyIDs, p.Header.KeyID)
		return 201, nil
	}
}

func TestConnectAfterResetTakesNewKeyAndSends(t *testing.T) {
	oldKey := bv180Secret(t)
	newKey := bv180Secret(t)
	oldKitID, newKitID := bv180KitID(t), bv180KitID(t)
	hostID := "hostbv180abc"

	sp := bv180Paths(t, oldKitID, oldKey)

	// This run's package (newest) and an older run's package, both sealed with
	// the old key.
	newContent := []byte(`{"schema_version":"space-view.v1","run":"today"}`)
	oldContent := []byte(`{"schema_version":"space-view.v1","run":"yesterday"}`)
	newestPkg := bv180WritePackage(t, sp.reports, oldKey.Key, oldKitID, hostID, "2026-11-04", newContent)
	olderPkg := bv180WritePackage(t, sp.reports, oldKey.Key, oldKitID, hostID, "2026-11-03", oldContent)

	cred := make([]byte, 32)
	rand.Read(cred)
	bv180ConnectStub(t, spacenet.Material{Secret: newKey, KitID: newKitID, Credential: cred})

	// Connect: the space hands out the new key.
	if err := spaceConnect(context.Background(), sp, func(int) {}, func(string) error { return nil }); err != nil {
		t.Fatalf("spaceConnect: %v", err)
	}

	// The kit now holds the new key and credential, and the computer is connected.
	stored, err := kit.Load(sp.store)
	if err != nil {
		t.Fatalf("kit.Load: %v", err)
	}
	if kit.KeyID(stored.Secret.Key) != kit.KeyID(newKey.Key) {
		t.Error("kit still holds the old key after connect")
	}
	if stored.KitID != newKitID || len(stored.SpaceCredential) != 32 {
		t.Errorf("kit not replaced with the space's: kit_id=%q cred=%d", stored.KitID, len(stored.SpaceCredential))
	}
	if !isSpaceConnected(sp) {
		t.Error("not connected after taking the new key")
	}

	// The newest package was re-sealed with the new key, same content.
	raw, err := os.ReadFile(newestPkg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := results.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Header.KeyID != kit.KeyID(newKey.Key) || p.Header.KitID != newKitID {
		t.Errorf("newest package not re-sealed: key_id=%q kit_id=%q", p.Header.KeyID, p.Header.KitID)
	}
	got, err := results.Open(newKey.Key, p)
	if err != nil {
		t.Fatalf("re-sealed package does not open with the new key: %v", err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("content changed on re-seal: got %q want %q", got, newContent)
	}

	// The older package is untouched (still the old key).
	rawOld, err := os.ReadFile(olderPkg)
	if err != nil {
		t.Fatal(err)
	}
	pOld, _ := results.Parse(rawOld)
	if pOld.Header.KeyID != kit.KeyID(oldKey.Key) {
		t.Error("older package was re-sealed; only this run's package should be")
	}

	// Send the moment the space connected: exactly the re-sealed package goes,
	// sealed with the new (current) key; the old-key package is never sent.
	var sent []string
	bv180SendStub(t, &sent)
	var out bytes.Buffer
	prevOut := kitOutput
	kitOutput = &out
	t.Cleanup(func() { kitOutput = prevOut })

	cat, _ := catalog.Load("en")
	now := time.Now()
	sendAfterConnect(sp, cat, now)

	if len(sent) != 1 {
		t.Fatalf("sent %d packages, want 1 (the re-sealed one)", len(sent))
	}
	if sent[0] != kit.KeyID(newKey.Key) {
		t.Errorf("sent a package with key_id %q, want the new key %q", sent[0], kit.KeyID(newKey.Key))
	}
	for _, id := range sent {
		if id == kit.KeyID(oldKey.Key) {
			t.Error("a package sealed with the old key was sent")
		}
	}
	// The console confirms the send.
	want := cat.Text("report.space_sent", map[string]string{"date": now.In(time.Local).Format("2006-01-02")})
	if !strings.Contains(out.String(), want) {
		t.Errorf("console did not confirm the send.\ngot: %q\nwant substring: %q", out.String(), want)
	}
	// The sends log shows the old-key package skipped and the new one sent.
	logRaw, err := os.ReadFile(filepath.Join(sp.reports, SendsLogName))
	if err != nil {
		t.Fatalf("no sends log: %v", err)
	}
	if !strings.Contains(string(logRaw), " "+sendSkipped+" ") {
		t.Errorf("old-key package not skipped in the log:\n%s", logRaw)
	}
	if !strings.Contains(string(logRaw), " "+sendSent+" ") {
		t.Errorf("re-sealed package not sent in the log:\n%s", logRaw)
	}
}

// TestConnectSameKeyUnchanged: when the space returns the same key_id this
// computer already holds, nothing is re-sealed — the behaviour is as it was
// before BV-180 — and the package still sends.
func TestConnectSameKeyUnchanged(t *testing.T) {
	key := bv180Secret(t)
	kitID := bv180KitID(t)
	hostID := "hostbv180abc"

	sp := bv180Paths(t, kitID, key)
	content := []byte(`{"schema_version":"space-view.v1","run":"today"}`)
	pkgPath := bv180WritePackage(t, sp.reports, key.Key, kitID, hostID, "2026-11-04", content)
	before, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatal(err)
	}

	cred := make([]byte, 32)
	rand.Read(cred)
	// Same key, same kit_id: a reconnect that just refreshes the credential.
	bv180ConnectStub(t, spacenet.Material{Secret: key, KitID: kitID, Credential: cred})

	if err := spaceConnect(context.Background(), sp, func(int) {}, func(string) error { return nil }); err != nil {
		t.Fatalf("spaceConnect: %v", err)
	}

	// The package file is byte-identical: it was not re-sealed.
	after, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the package was re-sealed although the key did not change")
	}

	// It still sends normally (its key is the current key).
	var sent []string
	bv180SendStub(t, &sent)
	var out bytes.Buffer
	prevOut := kitOutput
	kitOutput = &out
	t.Cleanup(func() { kitOutput = prevOut })
	cat, _ := catalog.Load("en")
	sendAfterConnect(sp, cat, time.Now())
	if len(sent) != 1 || sent[0] != kit.KeyID(key.Key) {
		t.Fatalf("send with the unchanged key: sent=%v", sent)
	}
}

// TestConnectFailedSaveRevertsPermission: a failed save leaves the computer not
// connected (unchanged by BV-180).
func TestConnectFailedSaveRevertsPermission(t *testing.T) {
	dir := t.TempDir()
	// Make the kit store path unwritable: its parent is a regular file, so
	// kit.Save's MkdirAll fails.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sp := spacePaths{
		store:   filepath.Join(blocker, "kit.key"),
		state:   filepath.Join(dir, "state.json"),
		reports: filepath.Join(dir, "reports"),
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(sp.state, st); err != nil {
		t.Fatal(err)
	}

	newKey := bv180Secret(t)
	kidRaw := make([]byte, 16)
	rand.Read(kidRaw)
	cred := make([]byte, 32)
	rand.Read(cred)
	bv180ConnectStub(t, spacenet.Material{Secret: newKey, KitID: hex.EncodeToString(kidRaw), Credential: cred})

	if err := spaceConnect(context.Background(), sp, func(int) {}, func(string) error { return nil }); err == nil {
		t.Fatal("spaceConnect returned nil although the save fails")
	}
	got, err := state.LoadState(sp.state)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpacePermission {
		t.Error("permission not reverted after a failed save")
	}
	if isSpaceConnected(sp) {
		t.Error("isSpaceConnected true after a failed save")
	}
}
