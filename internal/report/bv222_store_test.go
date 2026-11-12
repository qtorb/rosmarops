package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// TestReportStoreUpdateAndOldCopy (BV-222): the report says the Microsoft
// Store updated RosmarOps; when the weekly review still runs another
// rosmarops.exe it says so and gives the command with the Store alias. With no
// Store update, neither text appears.
func TestReportStoreUpdateAndOldCopy(t *testing.T) {
	const alias = `C:\Users\ana\AppData\Local\Microsoft\WindowsApps\rosmarops.exe`
	upd := v17Report(t, func(in *report.V2Input) {
		in.StoreUpdateOld, in.StoreUpdateNew = "0.11.3", "0.11.4"
		in.ExePath = alias
		in.Weekly = scheduledtasks.Weekly{State: scheduledtasks.WeeklyOther, Other: `C:\Users\ana\Downloads\rosmarops.exe`}
	})
	plain := v17Report(t, func(in *report.V2Input) {
		in.ExePath = alias
		in.Weekly = scheduledtasks.Weekly{State: scheduledtasks.WeeklyOther, Other: `C:\Users\ana\Downloads\rosmarops.exe`}
	})
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		line := htmlEscape(cat.Text("report.integrity.binary_store", map[string]string{"old": "0.11.3", "new": "0.11.4"}))
		oldCopy := htmlEscape(strings.SplitN(cat.Text("report.weekly.points_to_old_exe", nil), "(", 2)[0])
		cmd := htmlEscape(report.WeeklyCommand(alias, lang))
		page := renderHTML(t, lang, upd)
		if !strings.Contains(page, line) {
			t.Errorf("%s: no store line %q", lang, line)
		}
		if !strings.Contains(page, oldCopy) || !strings.Contains(page, cmd) {
			t.Errorf("%s: no old-copy line or alias command", lang)
		}
		page = renderHTML(t, lang, plain)
		if strings.Contains(page, oldCopy) || strings.Contains(page, htmlEscape(strings.SplitN(cat.Text("report.integrity.binary_store", nil), "{", 2)[0])) {
			t.Errorf("%s: store texts without a store update", lang)
		}
	}
}
