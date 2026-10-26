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
	fmt.Fprint(a.out, a.cat.Text(key, nil))
	answer, _ := bufio.NewReader(a.in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "s":
		return true
	}
	return false
}

func isZeroHash(h string) bool { return h == "" || h == zeroHash }

// integrityRun is what the run's integrity check reads and may update.
type integrityRun struct {
	ask        asker
	st         *state.State
	stErr      error // the state could not be read: a lost state
	stPath     string
	cfgPath    string
	reportDir  string
	unattended bool
	lang       string
	selfHash   func() (string, error)
	// abort is abortWithIntegrityFailure, which never returns; tests
	// replace it.
	abort func(category, why, action string, o abortOptions)
}

// errAborted is check's error after an abort that returned (tests only).
var errAborted = errors.New("integrity: aborted")

// recordReferences sets both references to this binary and this
// configuration, writes the accepted copy and saves the state.
func (r integrityRun) recordReferences(self string) error {
	raw, err := os.ReadFile(r.cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	r.st.Integrity.VigilBinarySHA256 = self
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
	abort := func(category, why, action string, o abortOptions) {
		do := r.abort
		if do == nil {
			do = abortWithIntegrityFailure
		}
		o.lang, o.unattended = r.lang, r.unattended
		do(category, why, action, o)
	}
	self, err := r.selfHash()
	if err != nil {
		slog.Error("integrity: could not hash binary", "err", err)
		abort("binary_hash_changed", "Could not verify binary integrity: "+err.Error(),
			"Run 'rosmarops baseline' after verifying the binary is trustworthy.", abortOptions{})
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
		fmt.Fprintln(r.ask.out, t("console.state_lost", nil))
		if !r.ask.ask("console.state_confirm") {
			abort("binary_hash_changed", "RosmarOps lost its integrity reference: the state was lost or reset.",
				"If you reset or reinstalled RosmarOps yourself, run it by hand and confirm. If not, inspect ~/.rosmarops.", abortOptions{stateLost: true})
			return false, errAborted
		}
		return r.stErr != nil, r.recordReferences(self)
	case zero:
		// A state written by an earlier init that recorded no reference and
		// never ran: nothing to compare with yet.
		slog.Warn("integrity: no reference recorded yet — run 'rosmarops baseline' to establish it")
		return false, nil
	}
	if self != r.st.Integrity.VigilBinarySHA256 {
		// J2: the old and the new version and fingerprint, and the question.
		old := r.st.Integrity.VigilBinarySHA256
		fmt.Fprintln(r.ask.out, t("console.binary_changed", map[string]string{
			"old_version": r.st.EngineVersion, "old_hash": old[:16], "new_version": version, "new_hash": self[:16],
		}))
		if !r.ask.ask("console.binary_confirm") {
			slog.Error("integrity: binary hash mismatch", "stored", old[:16]+"…", "computed", self[:16]+"…")
			abort("binary_hash_changed", "RosmarOps binary hash does not match the last baseline.",
				"If you updated RosmarOps intentionally, run it by hand and confirm. "+
					"If not, do not run RosmarOps again — treat as potential compromise.", abortOptions{})
			return false, errAborted
		}
		r.st.Integrity.VigilBinarySHA256 = self
		r.st.EngineVersion = version
		if err := state.SaveState(r.stPath, *r.st); err != nil {
			return false, fmt.Errorf("save state: %w", err)
		}
	}
	// The configuration (adjustment A): what changed, and the question.
	if cur, hashErr := hash.File(r.cfgPath); hashErr == nil && cur != r.st.Integrity.ConfigSHA256 {
		ok, confirmErr := confirmConfig(confirmation{in: r.ask.in, out: r.ask.out, terminal: r.ask.terminal}, r.ask.cat, r.st, r.stPath, r.cfgPath, r.reportDir)
		if confirmErr != nil {
			return false, confirmErr
		}
		if !ok {
			abort("config_hash_changed", "Config file hash does not match the last baseline.",
				"If you edited the configuration intentionally, run RosmarOps by hand and confirm the change. "+
					"If not, inspect the file for unexpected changes.", abortOptions{configRejected: true})
			return false, errAborted
		}
	}
	return false, nil
}

// ScheduledLogName is the log of a scheduled review, in the reports
// directory (J3).
const ScheduledLogName = "last-scheduled-run-log.txt"

// deferredLog keeps a scheduled review's log in memory until the reports
// directory is known, then writes it, and what follows, to its file.
type deferredLog struct {
	mu   sync.Mutex
	buf  []byte
	file *os.File
}

func (d *deferredLog) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil {
		return d.file.Write(p)
	}
	d.buf = append(d.buf, p...)
	return len(p), nil
}

// attach opens dir/ScheduledLogName (replacing the last one) and writes what
// was kept. Only the first call opens a file.
func (d *deferredLog) attach(dir string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create report dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ScheduledLogName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open scheduled run log: %w", err)
	}
	if _, err := f.Write(d.buf); err != nil {
		return fmt.Errorf("write scheduled run log: %w", err)
	}
	d.file, d.buf = f, nil
	return nil
}
