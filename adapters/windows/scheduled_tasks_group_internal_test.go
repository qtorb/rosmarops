package windows

import "testing"

// A task registered for a group (a company deployment) carries the
// principal's GroupId through to the platform detail; a user task has none.
func TestScheduledTaskGroupID(t *testing.T) {
	raw := `[{"Name":"RosmarOps weekly review (company)","Path":"\\","Triggers":"MSFT_TaskWeeklyTrigger","Actions":"C:\\Tools\\rosmarops.exe","Date":"",` +
		`"RosmarOps":{"Count":1,"Execute":"C:\\Tools\\rosmarops.exe","Expanded":"C:\\Tools\\rosmarops.exe","Arguments":"run --unattended",` +
		`"WorkingDirectory":"","UserId":"","GroupId":"S-1-5-32-545","LogonType":"Group","RunLevel":"Limited",` +
		`"Triggers":[{"Class":"MSFT_TaskWeeklyTrigger","Days":"8","Start":"2026-10-28T13:30:00"}]}},` +
		`{"Name":"RosmarOps weekly review","Path":"\\","Triggers":"","Actions":"C:\\Tools\\rosmarops.exe","Date":"",` +
		`"RosmarOps":{"Count":1,"Execute":"C:\\Tools\\rosmarops.exe","UserId":"PC\\ana","GroupId":""}}]`
	var records []psTaskRecord
	if err := unmarshalOneOrMany([]byte(raw), &records); err != nil {
		t.Fatal(err)
	}
	if got := records[0].RosmarOps.task(); got.GroupID != "S-1-5-32-545" || got.UserID != "" {
		t.Errorf("group task: GroupID=%q UserID=%q", got.GroupID, got.UserID)
	}
	if got := records[1].RosmarOps.task(); got.GroupID != "" || got.UserID != `PC\ana` {
		t.Errorf("user task: GroupID=%q UserID=%q", got.GroupID, got.UserID)
	}
}
