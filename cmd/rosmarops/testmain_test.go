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
	connectWindowHook = func(spacePaths, catalog.Catalog) {}
	os.Exit(m.Run())
}
