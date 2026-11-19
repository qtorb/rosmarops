package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// The integrity of a run (decision 2026-10-26, integridad y revisión
// semanal, J1–J3): the references init records, checked on every run. A
// change is never accepted in silence: with a console the person answers;
// without one, or in a scheduled review, the run stops with a red report.

// asker asks the person in the console. terminal is false without a
// console and always in a scheduled review: then nothing is read and every
// question counts as «no».
type asker struct {
	in       io.Reader
	out      io.Writer
	terminal bool
	cat      catalog.Catalog
}

// ask prints key and reads the answer: only «y» or «s» (any case) is yes.
func (a asker) ask(key string) bool {
	if !a.terminal {
		return false
	}
	ui.question() // BV-151: the window and taskbar show a pending question
	defer ui.running()
	fmt.Fprint(a.out, a.cat.Text(key, nil))
	answer, _ := bufio.NewReader(a.in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "s":
		return true
	}
	return false
}

func isZeroHash(h string) bool { return h == "" || h == zeroHash }

// The catalog keys of an integrity abort's text: each has a .why and an
// .action. None sends the person to baseline: accepting a new version is the
// question every command asks in a console.
const (
	abortBinary       = "integrity.abort.binary"
	abortConfig       = "integrity.abort.config"
	abortState        = "integrity.abort.state"
	abortUnverifiable = "integrity.abort.unverifiable" // .why takes {error}
)

// integrityRun is what the run's integrity check reads and may update.
type integrityRun struct {
	ask        asker
	st         *state.State
	stErr      error // the state could not be read: a lost state
	stPath     string
	cfgPath    string
	reportDir  string
	unattended bool
	window     bool // the double-click window (U5)
	lang       string
	selfHash   func() (string, error)
	// abort is abortWithIntegrityFailure, which never returns; tests
	// replace it.
	abort func(category, text string, o abortOptions)
	// store reports whether a changed binary is a Microsoft Store update the
	// check accepts without asking (storeAccepts with this process's package
	// identity); nil: never (tests that do not exercise it).
	store func(stateVersion string) bool
	// accepted is set when the check accepted a Microsoft Store update, for
	// the report; nil: not recorded.
	accepted *storeUpdate
	// adminList reports whether the administrator's fingerprint list,
	// trusted, holds this binary's fingerprint with its version; nil: never
	// (tests that do not exercise it). session-check and statusline never
	// get here: they never accept and never write.
	adminList func(self string) bool
	// company is the review's company configuration: its source decides what
	// the configuration fingerprint means, and the check records there how
	// the binary was accepted. nil: no company configuration.
	company *companyRun
}

// source is the configuration in use: user or company.
func (r integrityRun) source() string {
	if r.company != nil && r.company.source == configSourceCompany {
		return configSourceCompany
	}
	return configSourceUser
}

// setBinary records how this review accepted its binary.
func (r integrityRun) setBinary(how string) {
	if r.company != nil {
		r.company.binary = how
	}
}

// realStoreCheck is integrityRun.store for a real run: this process's package
// identity and real executable (CTO-41 §1.2 B).
func realStoreCheck(stateVersion string) bool {
	exe, err := currentExe()
	if err != nil {
		return false
	}
	return storeAccepts(currentPackage(), exe, os.Getenv("LOCALAPPDATA"), version, stateVersion)
}

// errAborted is check's error after an abort that returned (tests only).
var errAborted = errors.New("integrity: aborted")

// recordReferences sets both references to this binary and this
// configuration, writes the accepted copy and saves the state; via is how the
// binary was accepted.
func (r integrityRun) recordReferences(self, via string) error {
	raw, err := os.ReadFile(r.cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	r.st.Integrity.VigilBinarySHA256 = self
	r.st.Integrity.BinaryAcceptedVia = via
	r.st.Integrity.ConfigSource = configSourceIn(r.source())
	r.st.Integrity.ConfigSHA256 = hash.Bytes(raw)
	r.st.EngineVersion = version
	if err := writeAcceptedCopy(r.reportDir, raw); err != nil {
		return err
	}
	if err := state.SaveState(r.stPath, *r.st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// check runs the run's integrity check, in this order: the state (lost:
// missing, unreadable, or both references at zero after a run), the binary,
// the configuration. A lost state is a change, never a fresh start (J1):
// accepted in the console, the references are recorded again; and when the
// state could not be read, notChecked says the report must tell that this
// run's integrity was not checked. Every rejection aborts with a red report
// and never returns.
func (r integrityRun) check() (notChecked bool, err error) {
	abort := func(category, text string, o abortOptions) {
		do := r.abort
		if do == nil {
			do = abortWithIntegrityFailure
		}
		o.lang, o.unattended, o.window = r.lang, r.unattended, r.window
		// The company reads that this review stopped, before the abort that
		// never returns.
		code := category
		if o.stateLost {
			code = "state_lost"
		}
		r.company.writeAbortSummary(report.SummaryIntegrityAbort, code)
		do(category, text, o)
	}
	self, err := r.selfHash()
	if err != nil {
		slog.Error("integrity: could not hash binary", "err", err)
		abort("binary_hash_changed", abortUnverifiable, abortOptions{detail: err.Error()})
		return false, errAborted
	}
	t := func(key string, p map[string]string) string { return r.ask.cat.Text(key, p) }
	zero := isZeroHash(r.st.Integrity.VigilBinarySHA256) || isZeroHash(r.st.Integrity.ConfigSHA256)
	switch {
	case r.stErr != nil || zero && r.st.HasRun():
		// J1: the state is gone, unreadable, or lost its references after a
		// run: a change, never a fresh start.
		if r.stErr != nil {
			slog.Warn("integrity: state unavailable", "err", r.stErr)
		}
		// With the company configuration in use (no config.json of the
		// person's), a lost or missing state is a first run only when the
		// binary passes the administrator's list or the Store check: deleting
		// the state is never enough to accept any binary.
		if r.source() == configSourceCompany {
			via := ""
			switch {
			case r.adminList != nil && r.adminList(self):
				via = acceptedViaAdminList
			case r.store != nil && r.store("0.0.0"):
				via = acceptedViaStore
			}
			if via != "" {
				slog.Info("integrity: first run with the company configuration", "accepted_via", via)
				r.setBinary(binaryFirstRun)
				return false, r.recordReferences(self, via)
			}
		}
		fmt.Fprintln(r.ask.out, t("console.state_lost", nil))
		// Starting from scratch gives this computer a new identity: say so
		// before the question when it was connected to a space, or when the
		// state cannot be read and so that cannot be known.
		if r.ask.terminal && (r.stErr != nil || r.st.SpacePermission) {
			fmt.Fprintln(r.ask.out, t("console.state_new_identity", nil))
		}
		if !r.ask.ask("console.state_confirm") {
			abort("binary_hash_changed", abortState, abortOptions{stateLost: true})
			return false, errAborted
		}
		r.setBinary(acceptedViaConsole)
		return r.stErr != nil, r.recordReferences(self, acceptedViaConsole)
	case zero:
		// A state written by an earlier init that recorded no reference and
		// never ran: nothing to compare with yet.
		slog.Warn("integrity: no reference recorded yet")
		return false, nil
	}
	if self != r.st.Integrity.VigilBinarySHA256 && r.adminList != nil && r.st.EngineVersion != "" &&
		semverGreater(version, r.st.EngineVersion) && r.adminList(self) {
		// Invariant 8: the administrator's fingerprint list holds this binary
		// with its version, newer than the recorded one (never an older
		// version, never the same version with another fingerprint); checked
		// before the person is asked. The report says so.
		oldVersion := r.st.EngineVersion
		slog.Info("integrity: binary accepted by the administrator's list", "old_version", oldVersion, "new_version", version)
		r.st.Integrity.VigilBinarySHA256 = self
		r.st.Integrity.BinaryAcceptedVia = acceptedViaAdminList
		r.st.EngineVersion = version
		if err := state.SaveState(r.stPath, *r.st); err != nil {
			return false, fmt.Errorf("save state: %w", err)
		}
		r.setBinary(acceptedViaAdminList)
		if r.company != nil {
			r.company.adminOld, r.company.adminNew = oldVersion, version
		}
	}
	if self != r.st.Integrity.VigilBinarySHA256 && r.store != nil && r.st.EngineVersion != "" && r.store(r.st.EngineVersion) {
		// Invariant 8 (BV-222): a Microsoft Store update of RosmarOps itself,
		// newer than the recorded version, is accepted without asking; the
		// report says so.
		oldVersion := r.st.EngineVersion
		slog.Info("integrity: binary accepted as a Microsoft Store update", "old_version", oldVersion, "new_version", version)
		r.st.Integrity.VigilBinarySHA256 = self
		r.st.Integrity.BinaryAcceptedVia = acceptedViaStore
		r.st.EngineVersion = version
		if err := state.SaveState(r.stPath, *r.st); err != nil {
			return false, fmt.Errorf("save state: %w", err)
		}
		if r.accepted != nil {
			*r.accepted = storeUpdate{Old: oldVersion, New: version}
		}
		r.setBinary(acceptedViaStore)
	}
	if self != r.st.Integrity.VigilBinarySHA256 {
		// J2: the old and the new version and fingerprint, and the question.
		old := r.st.Integrity.VigilBinarySHA256
		fmt.Fprintln(r.ask.out, t("console.binary_changed", map[string]string{
			"old_version": r.st.EngineVersion, "old_hash": old[:16], "new_version": version, "new_hash": self[:16],
		}))
		if !r.ask.ask("console.binary_confirm") {
			slog.Error("integrity: binary hash mismatch", "stored", old[:16]+"…", "computed", self[:16]+"…")
			abort("binary_hash_changed", abortBinary, abortOptions{})
			return false, errAborted
		}
		r.st.Integrity.VigilBinarySHA256 = self
		r.st.Integrity.BinaryAcceptedVia = acceptedViaConsole
		r.st.EngineVersion = version
		if err := state.SaveState(r.stPath, *r.st); err != nil {
			return false, fmt.Errorf("save state: %w", err)
		}
		r.setBinary(acceptedViaConsole)
	}
	// The company configuration in use is an administrator's file, trusted
	// by its ownership check: a change to it is recorded and reported,
	// never an abort.
	if r.source() == configSourceCompany {
		cur, hashErr := hash.File(r.cfgPath)
		if hashErr != nil {
			return false, fmt.Errorf("hash company configuration: %w", hashErr)
		}
		if cur != r.st.Integrity.ConfigSHA256 || r.st.Integrity.ConfigSource != configSourceCompany {
			if r.company != nil && r.st.Integrity.ConfigSource == configSourceCompany {
				r.company.changed = true
			}
			r.st.Integrity.ConfigSHA256, r.st.Integrity.ConfigSource = cur, configSourceCompany
			if err := state.SaveState(r.stPath, *r.st); err != nil {
				return false, fmt.Errorf("save state: %w", err)
			}
		}
		return false, nil
	}
	// The configuration (adjustment A): what changed, and the question.
	if cur, hashErr := hash.File(r.cfgPath); hashErr == nil && cur != r.st.Integrity.ConfigSHA256 {
		ok, confirmErr := confirmConfig(confirmation{in: r.ask.in, out: r.ask.out, terminal: r.ask.terminal}, r.ask.cat, r.st, r.stPath, r.cfgPath, r.reportDir)
		if confirmErr != nil {
			return false, confirmErr
		}
		if !ok {
			abort("config_hash_changed", abortConfig, abortOptions{configRejected: true})
			return false, errAborted
		}
	}
	// The person's config.json is in use again: its fingerprint is a user one.
	if r.st.Integrity.ConfigSource != "" {
		r.st.Integrity.ConfigSource = ""
		if err := state.SaveState(r.stPath, *r.st); err != nil {
			return false, fmt.Errorf("save state: %w", err)
		}
	}
	return false, nil
}

// configSourceIn is state.integrity.config_source for a source: absent for
// the person's config.json, as in a state from before it.
func configSourceIn(source string) string {
	if source == configSourceCompany {
		return configSourceCompany
	}
	return ""
}

// ScheduledLogName is the log of a scheduled review, in the reports
// directory (J3); RunLogName, the log of a double click (U5).
const (
	ScheduledLogName = "last-scheduled-run-log.txt"
	RunLogName       = "last-run-log.txt"
)

// deferredLog keeps a run's log in memory until the reports directory is
// known, then writes it, and what follows, to its file name there.
type deferredLog struct {
	name   string
	mu     sync.Mutex
	buf    []byte
	file   *os.File
	closed bool
}

func (d *deferredLog) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return len(p), nil
	}
	if d.file != nil {
		return d.file.Write(p)
	}
	d.buf = append(d.buf, p...)
	return len(p), nil
}

// attach opens dir/name (replacing the last one) and writes what was kept.
// Only the first call opens a file.
func (d *deferredLog) attach(dir string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil || d.closed {
		return nil
	}
	// BV-183: the run logs live in data/ inside the reports directory.
	dir = report.DataDir(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create report dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, d.name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	if _, err := f.Write(d.buf); err != nil {
		return fmt.Errorf("write run log: %w", err)
	}
	d.file, d.buf = f, nil
	return nil
}

// Close closes the log's file, once; later writes and attaches are dropped.
// A file left open cannot be removed on Windows.
func (d *deferredLog) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed, d.buf = true, nil
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	if err != nil {
		return fmt.Errorf("close run log: %w", err)
	}
	return nil
}
