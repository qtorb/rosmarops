package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// The connected space, program side (BV-137 points 5, 6, 8). Connecting and
// disconnecting from the console (macOS has no window; the Windows window calls
// the same spaceConnect), and sending each run's locked results package after
// the check. All network goes through internal/spacenet under a Permission, so
// nothing here reaches the network without the recorded approval.

// SendsLogName is the local record of every send attempt, written in the
// reports directory (invariant 2: no new write target). One line per attempt;
// never content.
const SendsLogName = "rosmarops-sends.log"

// maxPending is how many of the most recent unsent packages a run tries to
// send (BV-137 point 6: "at most the last 5 packages").
const maxPending = 5

// Send results, as they appear in the sends log and as the dedupe reads them.
// A terminal result means the package is not tried again; "deferred" is.
const (
	sendSent         = "sent"
	sendUnauthorized = "unauthorized" // 401: forget permission and credential
	sendRejected     = "rejected"     // 409 key_mismatch / no_kit
	sendTooLarge     = "too_large"    // 413
	sendOtherDevice  = "other_device" // 403
	sendBadRequest   = "bad_request"  // 400
	sendError        = "error"        // unexpected status
	sendDeferred     = "deferred"     // no connection, 429 or 5xx: try again next run
	sendSkipped      = "skipped"      // not a closed package with our key_id, or over 2 MB
)

// terminalResults are the results that mean "do not send this package again".
var terminalResults = map[string]bool{
	sendSent: true, sendUnauthorized: true, sendRejected: true,
	sendTooLarge: true, sendOtherDevice: true, sendBadRequest: true, sendError: true,
	sendSkipped: true,
}

// rejectedResults are the terminal results, other than a 401, that mean the
// area refused this run's package; the report says so (report.space_rejected).
var rejectedResults = map[string]bool{
	sendRejected: true, sendTooLarge: true, sendOtherDevice: true, sendBadRequest: true,
}

// maxPackageBytes is the largest file spaceSend will send (CTO-15 PR5): a
// closed results package is a few KB; anything over 2 MB is skipped.
const maxPackageBytes = 2 << 20

// spacePaths are where the space work reads and writes: the kit key file, the
// state, and the reports directory.
type spacePaths struct {
	store, state, reports string
}

func resolveSpacePaths() (spacePaths, error) {
	p, err := resolveKitPaths()
	if err != nil {
		return spacePaths{}, err
	}
	return spacePaths(p), nil
}

// spaceConnect runs the connect protocol for the console command and the
// Windows window. It records the permission in the state BEFORE any network
// (BV-137 point 5), connects, and on success stores the space key and the send
// credential in the kit blob before acking. onNumber shows the number; onOpen
// opens the browser. It returns ErrConnectCancelled / ErrConnectExpired / a
// network error, which the caller turns into connect.expired / connect.failed.
func spaceConnect(ctx context.Context, paths spacePaths, onNumber func(int), onOpen func(string) error) error {
	st, err := state.LoadState(paths.state)
	if err != nil {
		return fmt.Errorf("load state (run rosmarops init first): %w", err)
	}
	salt, err := st.Salt()
	if err != nil {
		return err
	}
	hostID := graph.HostID(salt)

	// The permission is saved before any connection (point 5). Once it is in
	// the state, a Permission can be obtained and the network may happen.
	if !st.SpacePermission {
		st.SpacePermission = true
		if err := state.SaveState(paths.state, st); err != nil {
			return err
		}
	}
	perm, err := spacenet.FromState(paths.state)
	if err != nil {
		revertSpacePermission(paths.state)
		return err
	}
	// Any connection that does not end stored reverts the permission (CTO-15
	// PR2): a cancel, an expiry, a network error or a failed save must leave
	// this computer "not connected", so the next double click offers the
	// window again and the report does not say "connected" forever.
	m, ack, err := connectProtocol(ctx, perm, hostID, onNumber, onOpen)
	if err != nil {
		revertSpacePermission(paths.state)
		return err
	}
	// Never replace a different local key (CTO-15 PR5): if this computer
	// already holds a kit with another key_id, refuse — revert the permission
	// so the window shows connect.failed — rather than silently overwrite it.
	if existing, _ := loadKitFrom(paths.store); existing != nil && kit.KeyID(existing.Secret.Key) != kit.KeyID(m.Secret.Key) {
		revertSpacePermission(paths.state)
		return fmt.Errorf("spacenet: this computer already holds a different space key")
	}
	// Store the space key and credential BEFORE acking (BV-137 point 4, CTO-14
	// P5): the area deletes the connection only on ack, so a failed save never
	// loses the credential.
	if err := kit.Save(paths.store, kit.Stored{KitID: m.KitID, Secret: m.Secret, SpaceCredential: m.Credential}); err != nil {
		revertSpacePermission(paths.state)
		return fmt.Errorf("store the space key and credential: %w", err)
	}
	if err := ack(ctx); err != nil {
		// The credential is already stored and usable; the area expires the
		// connect record in 10 minutes anyway. Not fatal.
		slog.Warn("connect: ack failed; the connection is stored locally", "err", err)
	}
	return nil
}

// connectProtocol is the program-side connect run. It is a seam: production is
// spacenet.Connect; tests replace it to exercise spaceConnect's revert without
// a real connection.
var connectProtocol = spacenet.Connect

// revertSpacePermission puts the permission back to not-connected after a
// connection that did not end stored (CTO-15 PR2). No credential is ever
// written on a failed connect, so only the permission needs undoing.
func revertSpacePermission(statePath string) {
	st, err := state.LoadState(statePath)
	if err != nil || !st.SpacePermission {
		return
	}
	st.SpacePermission = false
	if err := state.SaveState(statePath, st); err != nil {
		slog.Warn("revert space permission", "err", err)
	}
}

// isSpaceConnected reports whether this computer is connected to the space:
// the permission is recorded AND a send credential is stored (CTO-15 PR2). A
// permission with no credential (e.g. a connection that failed to finish) is
// not "connected".
func isSpaceConnected(sp spacePaths) bool {
	st, err := state.LoadState(sp.state)
	if err != nil || !st.SpacePermission {
		return false
	}
	stored, err := loadKitFrom(sp.store)
	return err == nil && stored != nil && len(stored.SpaceCredential) > 0
}

// spaceDisconnect revokes this computer at the area (best effort) and forgets
// the credential and permission locally regardless of the network (BV-137
// point 8).
func spaceDisconnect(ctx context.Context, paths spacePaths) {
	st, err := state.LoadState(paths.state)
	stored, _ := loadKitFrom(paths.store)
	if err == nil && st.SpacePermission && stored != nil && len(stored.SpaceCredential) > 0 {
		if perm, perr := spacenet.FromState(paths.state); perr == nil {
			if status, derr := spacenet.Disconnect(ctx, perm, stored.SpaceCredential); derr != nil {
				slog.Warn("disconnect: the area could not be reached; forgetting locally anyway", "err", derr)
			} else {
				slog.Info("disconnect: area responded", "status", status)
			}
		}
	}
	forgetSpace(paths, stored)
}

// sendPackage POSTs one package to the space. It is a seam: production is
// spacenet.Send (the only network path); tests replace it to exercise the send
// orchestration (queue, log, 401 wipe) without the hardened client, which by
// design cannot reach a local server.
var sendPackage = spacenet.Send

// spaceSend sends this computer's pending results packages to the space — the
// most recent up to maxPending, oldest first, the current run's package
// included — and records each in the sends log (BV-137 point 6). It runs only
// with the permission and a credential. A 401 forgets both and stops; a network
// error, 429 or 5xx leaves the package for the next run. It never fails the
// run: every outcome is logged, not returned.
func spaceSend(ctx context.Context, paths spacePaths, currentPkg string, now time.Time) {
	st, err := state.LoadState(paths.state)
	if err != nil {
		slog.Warn("space send skipped: state", "err", err)
		return
	}
	if !st.SpacePermission {
		return // not connected: nothing to do, and no network
	}
	perm, err := spacenet.FromState(paths.state)
	if err != nil {
		return
	}
	stored, err := loadKitFrom(paths.store)
	if err != nil {
		slog.Warn("space send skipped: kit", "err", err)
		return
	}
	if stored == nil || len(stored.SpaceCredential) == 0 {
		return // connected but no credential: cannot send
	}
	salt, err := st.Salt()
	if err != nil {
		slog.Warn("space send skipped: salt", "err", err)
		return
	}
	hostID := graph.HostID(salt)
	keyID := kit.KeyID(stored.Secret.Key)
	logPath := filepath.Join(paths.reports, SendsLogName)

	for _, pkg := range packagesToSend(paths.reports, currentPkg, maxPending) {
		body, err := os.ReadFile(pkg)
		if err != nil {
			slog.Warn("space send: read package", "path", pkg, "err", err)
			continue
		}
		// Only a closed results package no larger than 2 MB, sealed with this
		// kit's key, ever leaves (CTO-15 PR5); anything else is logged skipped
		// and never sent.
		if !sealedPackageForKey(body, keyID) {
			appendSendLog(logPath, now, filepath.Base(pkg), len(body), sendSkipped)
			continue
		}
		status, sendErr := sendPackage(ctx, perm, hostID, stored.SpaceCredential, body)
		result := sendResultFor(status, sendErr)
		appendSendLog(logPath, now, filepath.Base(pkg), len(body), result)
		switch result {
		case sendUnauthorized:
			slog.Info("space send: credential rejected; forgetting the space")
			forgetSpace(paths, stored)
			return
		case sendDeferred:
			slog.Info("space send: deferred (offline or rate-limited); will retry next run")
			return
		}
	}
}

// sendResultFor maps an HTTP status (0 on a network error) to a log result.
func sendResultFor(status int, err error) string {
	if err != nil {
		return sendDeferred // a network error: keep for next run
	}
	switch status {
	case 201:
		return sendSent
	case 401:
		return sendUnauthorized
	case 403:
		return sendOtherDevice
	case 409:
		return sendRejected
	case 413:
		return sendTooLarge
	case 400:
		return sendBadRequest
	case 429:
		return sendDeferred
	default:
		if status >= 500 {
			return sendDeferred
		}
		return sendError
	}
}

// sealedPackageForKey reports whether body is a closed rosmarops-results
// package no larger than 2 MB, sealed with the given key_id (CTO-15 PR5). Only
// such a file is ever sent; anything else (a stray file matching the name, a
// package for another key, or an oversized one) is skipped.
func sealedPackageForKey(body []byte, keyID string) bool {
	if len(body) > maxPackageBytes {
		return false
	}
	var p struct {
		Header struct {
			KeyID string `json:"key_id"`
		} `json:"header"`
		WrappedKey string `json:"wrapped_key"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return false
	}
	return p.Header.KeyID == keyID && p.WrappedKey != "" && p.Ciphertext != ""
}

// currentPackageStatus reads the sends log for this run's package and reports
// the local date it was sent (empty if not sent this run) and whether the area
// refused it (CTO-15 PR5). The last matching line wins.
func currentPackageStatus(reportsDir, currentPkg string, loc *time.Location) (sentDate string, rejected bool) {
	if currentPkg == "" {
		return "", false
	}
	base := filepath.Base(currentPkg)
	raw, err := os.ReadFile(filepath.Join(reportsDir, SendsLogName))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[4] != base {
			continue
		}
		switch {
		case f[3] == sendSent:
			if t, perr := time.Parse(time.RFC3339, f[0]); perr == nil {
				sentDate, rejected = t.In(loc).Format("2006-01-02"), false
			}
		case rejectedResults[f[3]]:
			sentDate, rejected = "", true
		}
	}
	return sentDate, rejected
}

// packagesToSend returns the package paths to send this run, oldest first: the
// most recent maxPending unsent packages, plus the current run's package, which
// is always included even if an earlier same-day run already sent a file of the
// same name (its content changed).
func packagesToSend(reportsDir, currentPkg string, max int) []string {
	terminal := terminalFilenames(reportsDir)
	matches, _ := filepath.Glob(filepath.Join(reportsDir, "rosmarops-results-*.locked.json"))
	currentBase := ""
	if currentPkg != "" {
		currentBase = filepath.Base(currentPkg)
	}
	var cands []string
	for _, m := range matches {
		base := filepath.Base(m)
		if base == currentBase {
			continue // added explicitly below, always
		}
		if terminal[base] {
			continue
		}
		cands = append(cands, m)
	}
	if currentPkg != "" {
		cands = append(cands, currentPkg)
	}
	sort.Strings(cands) // the filename carries the date: ascending is oldest first
	if len(cands) > max {
		cands = cands[len(cands)-max:]
	}
	return cands
}

// terminalFilenames reads the sends log and returns the set of package
// filenames whose last recorded result is terminal (so not to be sent again).
func terminalFilenames(reportsDir string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(reportsDir, SendsLogName))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		// <ts> <size> <destination> <result> <filename>
		if len(f) < 5 {
			continue
		}
		result, name := f[3], f[4]
		if terminalResults[result] {
			out[name] = true
		} else if out[name] {
			// a later non-terminal result (e.g. a re-send deferred) reopens it
			delete(out, name)
		}
	}
	return out
}

// appendSendLog writes one line to the sends log: the UTC time, the size in
// bytes, the destination host, the result, and the package filename. Never
// content (BV-137 point 6).
func appendSendLog(path string, now time.Time, filename string, size int, result string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Warn("sends log: create dir", "err", err)
		return
	}
	line := fmt.Sprintf("%s %d %s %s %s\n", now.UTC().Format(time.RFC3339), size, spacenet.Host, result, filename)
	// Keep the log to its last maxSendLogLines (CTO-15 PR5). It grows a few
	// lines a week, so a read-trim-rewrite on each append is cheap; the rename
	// stays inside the reports directory (invariant 2).
	if prev, err := os.ReadFile(path); err == nil {
		lines := strings.Split(strings.TrimRight(string(prev), "\n"), "\n")
		if len(lines) >= maxSendLogLines {
			kept := lines[len(lines)-(maxSendLogLines-1):]
			rewriteSendLog(path, strings.Join(kept, "\n")+"\n"+line)
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Warn("sends log: open", "err", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		slog.Warn("sends log: write", "err", err)
	}
}

// maxSendLogLines caps rosmarops-sends.log (CTO-15 PR5).
const maxSendLogLines = 1000

// rewriteSendLog replaces the log atomically with content (temp file + rename,
// both inside the reports directory).
func rewriteSendLog(path, content string) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sends-*.tmp")
	if err != nil {
		slog.Warn("sends log: temp", "err", err)
		return
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		slog.Warn("sends log: write temp", "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		slog.Warn("sends log: close temp", "err", err)
		return
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		slog.Warn("sends log: rename", "err", err)
	}
}

// forgetSpace clears the permission from the state and the credential from the
// kit blob. Used on a 401 and on disconnect. Best effort: each failure is
// logged, not returned.
func forgetSpace(paths spacePaths, stored *kit.Stored) {
	if st, err := state.LoadState(paths.state); err == nil && st.SpacePermission {
		st.SpacePermission = false
		if err := state.SaveState(paths.state, st); err != nil {
			slog.Warn("forget space: save state", "err", err)
		}
	}
	if stored == nil {
		var err error
		stored, err = loadKitFrom(paths.store)
		if err != nil || stored == nil {
			return
		}
	}
	if len(stored.SpaceCredential) > 0 {
		cleared := *stored
		cleared.SpaceCredential = nil
		if err := kit.Save(paths.store, cleared); err != nil {
			slog.Warn("forget space: save kit without credential", "err", err)
		}
	}
}

// newConnectCmd is `rosmarops connect`: the console path to connect this
// computer (macOS has no window; on Windows the double click shows the window).
func newConnectCmd() *cli.Command {
	lang := cli.Flag{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"}
	return &cli.Command{
		Name: "connect", Short: "cli.cmd.connect.short", Flags: []cli.Flag{lang},
		Args: 0, Before: integrityCheck, Run: runConnect,
	}
}

func runConnect(inv cli.Invocation) error {
	cat, err := catalog.Load(inv.Flags["lang"])
	if err != nil {
		return fmt.Errorf("lang_unsupported: %q (en, es)", inv.Flags["lang"])
	}
	paths, err := resolveSpacePaths()
	if err != nil {
		return err
	}
	if stored, err := loadKitFrom(paths.store); err == nil && stored != nil && len(stored.SpaceCredential) > 0 {
		fmt.Fprintln(kitOutput, cat.Text("connect.done", nil))
		return nil
	}
	fmt.Fprintln(kitOutput, cat.Text("connect.q", nil))
	fmt.Fprintln(kitOutput, cat.Text("connect.text", nil))
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	onNumber := func(n int) {
		fmt.Fprintln(kitOutput, cat.Text("connect.number", nil))
		fmt.Fprintln(kitOutput, n)
		fmt.Fprintln(kitOutput, cat.Text("connect.wait", nil))
	}
	onOpen := func(u string) error {
		fmt.Fprintln(kitOutput, u)
		openSpaceURL(u)
		return nil
	}
	switch err := spaceConnect(ctx, paths, onNumber, onOpen); {
	case err == nil:
		fmt.Fprintln(kitOutput, cat.Text("connect.done", nil))
		return nil
	case errors.Is(err, spacenet.ErrConnectExpired), errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintln(kitOutput, cat.Text("connect.expired", nil))
		return nil
	case errors.Is(err, spacenet.ErrConnectCancelled):
		fmt.Fprintln(kitOutput, cat.Text("connect.expired", nil))
		return nil
	default:
		slog.Warn("connect failed", "err", err)
		fmt.Fprintln(kitOutput, cat.Text("connect.failed", nil))
		return nil
	}
}

// newDisconnectCmd is `rosmarops disconnect` (BV-137 point 8).
func newDisconnectCmd() *cli.Command {
	lang := cli.Flag{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"}
	return &cli.Command{
		Name: "disconnect", Short: "cli.cmd.disconnect.short", Flags: []cli.Flag{lang},
		Args: 0, Before: integrityCheck, Run: runDisconnect,
	}
}

func runDisconnect(inv cli.Invocation) error {
	cat, err := catalog.Load(inv.Flags["lang"])
	if err != nil {
		return fmt.Errorf("lang_unsupported: %q (en, es)", inv.Flags["lang"])
	}
	paths, err := resolveSpacePaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), spacenet.RequestTimeout+5*time.Second)
	defer cancel()
	spaceDisconnect(ctx, paths)
	fmt.Fprintln(kitOutput, cat.Text("disconnect.done", nil))
	return nil
}

// openSpaceURL opens a URL in the browser, best effort (macOS uses `open`;
// elsewhere the command printed it). Reuses the report opener, which on macOS
// opens URLs too.
func openSpaceURL(u string) {
	if err := openReportFile(u); err != nil {
		slog.Info("connect: browser not opened; the URL was printed", "err", err)
	}
}
