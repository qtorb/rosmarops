package windows

import (
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// BV-93, B4: the scheduled-task and Defender queries load their module by the
// absolute system path (Import-Module prefix). Off Windows the prefix is empty
// (runPS returns before running anything there).
func TestImportModulePrefix(t *testing.T) {
	for _, name := range []string{"ScheduledTasks", "Defender"} {
		got := importModule(name)
		if runtime.GOOS != "windows" {
			if got != "" {
				t.Errorf("%s off Windows = %q, want empty", name, got)
			}
			continue
		}
		want := "Import-Module '" + winpath.Module(name) + "';"
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
		if !strings.HasPrefix(got, "Import-Module '") {
			t.Errorf("%s is not an absolute-path import: %q", name, got)
		}
	}
}
