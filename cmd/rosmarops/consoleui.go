package main

import (
	"fmt"
	"io"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// Console branding for a manual run (BV-151): the window title, the taskbar
// progress button and the brand header. A scheduled review (--unattended,
// which calls freeConsole) and any run whose output is not a console get no
// branding. ui is the run's instance; it and every method are nil-safe, so
// the abort paths can call it even on a run that set up none.
var ui *consoleUI

// consoleUI holds a run's console branding. title is nil when titles are off;
// bar is nil when the taskbar sequence is off.
type consoleUI struct {
	cat   catalog.Catalog
	title func(string)
	bar   *taskbar
}

// newConsoleUI builds the branding. active is «a console and not a scheduled
// review»; taskbarOn additionally needs Windows Terminal (WT_SESSION set) and
// virtual-terminal processing. w is where the taskbar sequences are written
// (os.Stdout in production; a buffer in tests).
func newConsoleUI(cat catalog.Catalog, active, taskbarOn bool, w io.Writer) *consoleUI {
	u := &consoleUI{cat: cat}
	if active {
		u.title = setConsoleTitle
	}
	if active && taskbarOn {
		u.bar = &taskbar{w: w, enabled: true, lastState: tbNormal}
	}
	return u
}

func (u *consoleUI) setTitle(key string) {
	if u == nil || u.title == nil {
		return
	}
	u.title(u.cat.Text(key, nil))
}

// taskbar returns the run's taskbar (nil when off), for the progress printer.
func (u *consoleUI) taskbar() *taskbar {
	if u == nil {
		return nil
	}
	return u.bar
}

// running, question, done and failed are the title + taskbar transitions.
func (u *consoleUI) running() {
	if u == nil {
		return
	}
	u.setTitle("console.title.running")
	u.bar.resume()
}

func (u *consoleUI) question() {
	if u == nil {
		return
	}
	u.setTitle("console.title.question")
	u.bar.pause()
}

func (u *consoleUI) done() {
	if u == nil {
		return
	}
	u.setTitle("console.title.done")
	u.bar.clear()
}

func (u *consoleUI) failed() {
	if u == nil {
		return
	}
	u.setTitle("console.title.failed")
	u.bar.failed()
}

// OSC 9;4 taskbar progress states (ConEmu / Windows Terminal).
const (
	tbClear         = 0 // no indicator
	tbNormal        = 1 // a value, 0–100
	tbError         = 2 // red
	tbIndeterminate = 3 // moving, no value
	tbPaused        = 4 // yellow, a value
)

// taskbar emits the OSC 9;4 sequence that colours the taskbar button
// (BV-151). Every method is nil-safe and a no-op when disabled.
type taskbar struct {
	w         io.Writer
	enabled   bool
	lastState int // the state to return to after a pause (resume)
	lastPct   int
}

// emit writes one sequence: ESC ] 9 ; 4 ; state ; value BEL.
func (t *taskbar) emit(state, pct int) {
	if t == nil || !t.enabled {
		return
	}
	fmt.Fprintf(t.w, "\x1b]9;4;%d;%d\a", state, pct)
}

// set records the state as the one a later resume returns to, then emits it.
func (t *taskbar) set(state, pct int) {
	if t == nil {
		return
	}
	t.lastState, t.lastPct = state, pct
	t.emit(state, pct)
}

func (t *taskbar) agents()        { t.set(tbIndeterminate, 0) }
func (t *taskbar) reposPct(p int) { t.set(tbNormal, p) }
func (t *taskbar) report()        { t.set(tbNormal, 100) }
func (t *taskbar) clear()         { t.set(tbClear, 0) }
func (t *taskbar) failed()        { t.set(tbError, 100) }

// pause shows the paused (yellow) colour at the last value without forgetting
// the state to resume to; resume re-emits that state.
func (t *taskbar) pause() {
	if t == nil {
		return
	}
	t.emit(tbPaused, t.lastPct)
}

func (t *taskbar) resume() {
	if t == nil {
		return
	}
	t.emit(t.lastState, t.lastPct)
}

// writeBrandHeader prints the brand header (BV-151): a blank line, two spaces
// and «RosmarOps» (bold rosemary-green #6F8F78 when color) + « v» + the
// version in the default colour, then a blank line. The name and the number
// are brand, not translatable.
func writeBrandHeader(w io.Writer, version string, color bool) {
	fmt.Fprintln(w)
	if color {
		fmt.Fprintf(w, "  \x1b[1;38;2;111;143;120mRosmarOps\x1b[0m v%s\n", version)
	} else {
		fmt.Fprintf(w, "  RosmarOps v%s\n", version)
	}
	fmt.Fprintln(w)
}
