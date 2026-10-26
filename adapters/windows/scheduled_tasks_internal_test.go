package windows

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// J5: the detail of a task that runs rosmarops.exe is read from the
// PowerShell JSON; any other task has none, and the canonical hash does not
// depend on it.
func TestScheduledTaskRosmarOpsDetail(t *testing.T) {
	raw := `[{"Name":"RosmarOps weekly review","Path":"\\","Triggers":"MSFT_TaskWeeklyTrigger","Actions":"C:\\Tools\\rosmarops.exe","Date":"",` +
		`"RosmarOps":{"Count":1,"Execute":"C:\\Tools\\rosmarops.exe","Expanded":"C:\\Tools\\rosmarops.exe","Arguments":"run --lang es --unattended",` +
		`"WorkingDirectory":"C:\\Tools","UserId":"PC\\ana","LogonType":"Interactive","RunLevel":"Limited",` +
		`"Triggers":[{"Class":"MSFT_TaskWeeklyTrigger","Days":"32","Start":"2026-10-30T09:00:00"}]}},` +
		`{"Name":"Backup","Path":"\\","Triggers":"","Actions":"C:\\b.exe","Date":"","RosmarOps":null}]`
	var records []psTaskRecord
	if err := unmarshalOneOrMany([]byte(raw), &records); err != nil {
		t.Fatal(err)
	}
	got := records[0].RosmarOps.task()
	want := &platform.RosmarOpsTask{
		Actions: 1, Execute: `C:\Tools\rosmarops.exe`, ExecuteExpanded: `C:\Tools\rosmarops.exe`, Arguments: "run --lang es --unattended",
		WorkingDirectory: `C:\Tools`, UserID: `PC\ana`, LogonType: "Interactive", RunLevel: "Limited",
		Triggers: []platform.TaskTrigger{{Class: "MSFT_TaskWeeklyTrigger", DaysOfWeek: 32, Start: "2026-10-30T09:00:00"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("detail %+v, want %+v", got, want)
	}
	if records[1].RosmarOps.task() != nil {
		t.Error("a task without rosmarops.exe has a detail")
	}
	r := records[0]
	if taskSHA256(r.Name, r.Path, r.Triggers, r.Actions) != hash.Bytes([]byte(r.Name+"\x00"+r.Path+"\x00"+r.Triggers+"\x00"+r.Actions)) {
		t.Error("the canonical hash changed")
	}
}
