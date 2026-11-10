package main

// Console input-mode flags for QuickEdit (BV-211 §2). ENABLE_QUICK_EDIT_MODE
// lets a click put the console in «Select» mode, which pauses the program when
// the person then types; ENABLE_EXTENDED_FLAGS must be set when changing these
// bits. https://learn.microsoft.com/windows/console/setconsolemode
const (
	enableExtendedFlags = 0x0080
	enableQuickEditMode = 0x0040
)

// quickEditOff is the console input mode to write to turn QuickEdit off: the
// QuickEdit bit cleared, with ENABLE_EXTENDED_FLAGS set.
func quickEditOff(mode uint32) uint32 {
	return (mode &^ enableQuickEditMode) | enableExtendedFlags
}

// disableQuickEditWith turns QuickEdit off through the given seams and returns
// a function that restores the original mode (to be deferred). It does nothing,
// with a no-op restore, unless this process owns its console and the mode can
// be read and then written. The real seams are in console_windows.go; tests
// pass fakes. This holds the BV-211 §2 behaviour in one place, testable on
// every platform.
func disableQuickEditWith(own func() bool, get func() (uint32, bool), set func(uint32) bool) func() {
	noop := func() {}
	if own == nil || !own() {
		return noop
	}
	mode, ok := get()
	if !ok {
		return noop
	}
	if !set(quickEditOff(mode)) {
		return noop
	}
	return func() { _ = set(mode) }
}
