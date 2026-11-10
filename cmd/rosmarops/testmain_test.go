package main

import (
	"os"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// TestMain disables the connect window for every test in this package. Several
// tests drive the double click (runNoArgs), which on Windows would launch the
// real WinForms window; with nobody to answer it, a headless test would hang
// until the timeout (seen in CI as a 10-minute `verify-windows` failure). The
// window's own driver is tested directly in connectwindow_test.go.
func TestMain(m *testing.M) {
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { return connectNotOffered }
	// BV-210: likewise disable the «send a problem?» window and its outcome
	// message box, so a double-click test that recorded a problem does not
	// launch a real WinForms window and hang. The prompt and send logic are
	// tested directly through these seams.
	problemUIHook = func(problemAsk) (problemAnswer, bool) { return problemAnswer{}, false }
	showProblemOutcomeFn = func(catalog.Catalog, bool, string) {}
	problemUnmarkHook = func(catalog.Catalog, int) bool { return false }
	os.Exit(m.Run())
}
