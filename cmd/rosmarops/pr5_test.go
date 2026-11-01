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
)

// CTO-15 PR5: spaceSend sends only a closed package sealed with this kit's key
// and no larger than 2 MB. A stray file matching the name, a package for
// another key, and an oversized one are logged "skipped" and never sent.
func TestSpaceSendSkipsNonSendable(t *testing.T) {
	paths, sec := connectedSpace(t)

	// 1) not a sealed package.
	stray := filepath.Join(paths.reports, "rosmarops-results-2026-11-01-host_abc.locked.json")
	if err := os.WriteFile(stray, []byte(`{"not":"a package"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// 2) a package sealed with a different key.
	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	other, _ := kit.NewSecret(salt)
	foreign, err := results.Seal(other.Key, results.Header{KeyID: kit.KeyID(other.Key)}, []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := results.Marshal(foreign)
	foreignPath := filepath.Join(paths.reports, "rosmarops-results-2026-11-02-host_abc.locked.json")
	if err := os.WriteFile(foreignPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	called := 0
	orig := sendPackage
	t.Cleanup(func() { sendPackage = orig })
	sendPackage = func(context.Context, spacenet.Permission, string, []byte, []byte) (int, error) {
		called++
		return 201, nil
	}
	// The current package is sendable (sealed with our key); the two above are not.
	cur := writePkg(t, paths.reports, "2026-11-03", sec)
	spaceSend(context.Background(), paths, cur, time.Now())

	if called != 1 {
		t.Errorf("sent %d packages; only the current sendable one should be sent", called)
	}
	log := readLog(t, paths.reports)
	for _, name := range []string{filepath.Base(stray), filepath.Base(foreignPath)} {
		if !strings.Contains(log, name+"\n") && !strings.Contains(log, " "+sendSkipped+" "+name) {
			t.Errorf("%s not logged as skipped:\n%s", name, log)
		}
	}
	if strings.Count(log, " "+sendSkipped+" ") != 2 {
		t.Errorf("want 2 skipped lines:\n%s", log)
	}
}

// CTO-15 PR5: currentPackageStatus reports this run's package as sent (with its
// date) or rejected, from the sends log.
func TestCurrentPackageStatus(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, SendsLogName)
	cur := filepath.Join(dir, "rosmarops-results-2026-11-01-host_abc.locked.json")
	base := filepath.Base(cur)

	if d, rej := currentPackageStatus(dir, cur, time.UTC); d != "" || rej {
		t.Errorf("no log yet: got %q, %v", d, rej)
	}
	appendSendLog(logPath, time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), base, 10, sendSent)
	if d, rej := currentPackageStatus(dir, cur, time.UTC); d != "2026-11-01" || rej {
		t.Errorf("after sent: got %q, %v", d, rej)
	}
	// A later rejection supersedes.
	appendSendLog(logPath, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), base, 10, sendTooLarge)
	if d, rej := currentPackageStatus(dir, cur, time.UTC); d != "" || !rej {
		t.Errorf("after rejection: got %q, %v", d, rej)
	}
}

// CTO-15 PR5: rosmarops-sends.log keeps at most its last 1000 lines.
func TestSendLogCapped(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, SendsLogName)
	for i := 0; i < 1200; i++ {
		appendSendLog(logPath, time.Now(), "rosmarops-results-x.locked.json", i, sendSent)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != maxSendLogLines {
		t.Errorf("log has %d lines, want %d", len(lines), maxSendLogLines)
	}
	// The newest line survived.
	if !strings.Contains(lines[len(lines)-1], " 1199 ") {
		t.Errorf("newest line not kept: %q", lines[len(lines)-1])
	}
}
