package report_test

import (
	"os"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// TestMain: the package's reports are Windows runs wherever the tests run
// (on macOS, Build would make every one a macOS report, BV-102); the macOS
// tests set Platform.OS themselves.
func TestMain(m *testing.M) {
	report.SetGOOS("windows")
	os.Exit(m.Run())
}
