package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// psTaskRecord mirrors one element of the JSON array emitted by the PowerShell
// scheduled-task enumeration command.
type psTaskRecord struct {
	Name      string       `json:"Name"`
	Path      string       `json:"Path"`
	Triggers  string       `json:"Triggers"` // comma-separated CIM class names
	Actions   string       `json:"Actions"`  // pipe-separated executable paths
	Date      string       `json:"Date"`     // ISO-8601 registration date, may be empty
	RosmarOps *psRosmarOps `json:"RosmarOps"`
}

// psRosmarOps is the detail of a task that runs rosmarops.exe (J5).
type psRosmarOps struct {
	Count            int         `json:"Count"`
	Execute          string      `json:"Execute"`
	Expanded         string      `json:"Expanded"`
	Arguments        string      `json:"Arguments"`
	WorkingDirectory string      `json:"WorkingDirectory"`
	UserID           string      `json:"UserId"`
	LogonType        string      `json:"LogonType"`
	RunLevel         string      `json:"RunLevel"`
	Triggers         []psTrigger `json:"Triggers"`
}

type psTrigger struct {
	Class string `json:"Class"`
	Days  string `json:"Days"`
	Start string `json:"Start"`
}

// ListScheduledTasks implements platform.Platform.
//
// Enumerates all Task Scheduler tasks via PowerShell and returns them as
// platform.ScheduledTask values. The caller is responsible for any date-based
// filtering (e.g., "only tasks registered in the last N days").
//
// Tasks with an MSFT_TaskLogonTrigger trigger have HasLogonTrigger = true.
// Tasks with an unknown or missing Date have Date = zero time.Time.
//
// Returns nil, nil if PowerShell or Task Scheduler is unavailable.
func (w *WindowsPlatform) ListScheduledTasks(ctx context.Context) ([]platform.ScheduledTask, error) {
	// The PowerShell pipeline:
	//   - Iterates all registered tasks.
	//   - Collects trigger class names (to detect logon triggers) and action
	//     executables (for the canonical hash).
	//   - Outputs a JSON array; @() ensures an array even for a single task.
	const psCmd = `$t = @(Get-ScheduledTask | ForEach-Object {` +
		`$task=$_;` +
		`$triggers=(($task.Triggers | ForEach-Object {$_.CimClass.CimClassName}) -join ',');` +
		`$actions=(($task.Actions | ForEach-Object {if($_.Execute){"$($_.Execute)"}else{''}}) -join '|');` +
		`[PSCustomObject]@{` +
		`Name=$task.TaskName;` +
		`Path=$task.TaskPath;` +
		`Triggers=$triggers;` +
		`Actions=$actions;` +
		`Date=if($task.Date){"$($task.Date)"}else{''};` +
		// J5: for a task with a rosmarops.exe action only, its detail.
		`RosmarOps=$(` +
		`$ra=@($task.Actions);` +
		`$m=@($ra | Where-Object {$_.Execute -and ((Split-Path -Leaf ([Environment]::ExpandEnvironmentVariables("$($_.Execute)").Trim('"'))) -ieq 'rosmarops.exe')});` +
		`if($m.Count -gt 0){$a0=$m[0];` +
		`[PSCustomObject]@{` +
		`Count=$ra.Count;` +
		`Execute="$($a0.Execute)";` +
		`Expanded=[Environment]::ExpandEnvironmentVariables("$($a0.Execute)").Trim('"');` +
		`Arguments="$($a0.Arguments)";` +
		`WorkingDirectory="$($a0.WorkingDirectory)";` +
		`UserId="$($task.Principal.UserId)";` +
		`LogonType="$($task.Principal.LogonType)";` +
		`RunLevel="$($task.Principal.RunLevel)";` +
		`Triggers=@($task.Triggers | ForEach-Object {[PSCustomObject]@{Class=$_.CimClass.CimClassName;Days="$($_.DaysOfWeek)";Start="$($_.StartBoundary)"}})` +
		`}}else{$null})` +
		`}` +
		`});` +
		`ConvertTo-Json -InputObject $t -Depth 4 -Compress`

	raw, err := runPS(ctx, psCmd)
	if err != nil {
		return nil, fmt.Errorf("scheduled_tasks: query: %w", err)
	}
	if raw == nil {
		return nil, nil
	}

	// ConvertTo-Json with a single element may return an object, not an array.
	// Handle both cases.
	var records []psTaskRecord
	if err := unmarshalOneOrMany(raw, &records); err != nil {
		return nil, fmt.Errorf("scheduled_tasks: parse JSON: %w", err)
	}

	tasks := make([]platform.ScheduledTask, 0, len(records))
	for _, r := range records {
		if r.Name == "" {
			continue
		}
		tasks = append(tasks, platform.ScheduledTask{
			Name:            r.Name,
			Path:            r.Path,
			SHA256:          taskSHA256(r.Name, r.Path, r.Triggers, r.Actions),
			HasLogonTrigger: hasLogonTrigger(r.Triggers),
			Date:            parseTaskDate(r.Date),
			RosmarOps:       r.RosmarOps.task(),
		})
	}
	return tasks, nil
}

// task is the platform form of the detail; nil stays nil.
func (r *psRosmarOps) task() *platform.RosmarOpsTask {
	if r == nil {
		return nil
	}
	t := &platform.RosmarOpsTask{
		Actions: r.Count, Execute: r.Execute, ExecuteExpanded: r.Expanded, Arguments: r.Arguments,
		WorkingDirectory: r.WorkingDirectory, UserID: r.UserID, LogonType: r.LogonType, RunLevel: r.RunLevel,
	}
	for _, tr := range r.Triggers {
		days, _ := strconv.Atoi(strings.TrimSpace(tr.Days))
		t.Triggers = append(t.Triggers, platform.TaskTrigger{Class: tr.Class, DaysOfWeek: days, Start: tr.Start})
	}
	return t
}

// taskSHA256 computes the canonical SHA-256 for a scheduled task.
// The canonical form is: name + NUL + path + NUL + triggerTypes + NUL + actions.
func taskSHA256(name, path, triggers, actions string) string {
	canonical := name + "\x00" + path + "\x00" + triggers + "\x00" + actions
	return hash.Bytes([]byte(canonical))
}

// hasLogonTrigger returns true if the comma-separated trigger class names
// include MSFT_TaskLogonTrigger.
func hasLogonTrigger(triggers string) bool {
	for _, t := range strings.Split(triggers, ",") {
		t = strings.TrimSpace(t)
		if strings.EqualFold(t, "MSFT_TaskLogonTrigger") {
			return true
		}
	}
	return false
}

// parseTaskDate parses the registration date string returned by PowerShell.
// Returns zero time.Time on parse failure or empty input.
func parseTaskDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// PowerShell may return the date in various ISO-8601-like formats.
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"1/2/2006 3:04:05 PM",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// unmarshalOneOrMany handles PowerShell's ConvertTo-Json quirk: when there is
// only one element, it returns a JSON object instead of a one-element array.
func unmarshalOneOrMany(data []byte, dst *[]psTaskRecord) error {
	// Try array first.
	if err := json.Unmarshal(data, dst); err == nil {
		return nil
	}
	// Fall back to single object.
	var single psTaskRecord
	if err := json.Unmarshal(data, &single); err != nil {
		return fmt.Errorf("unmarshal task JSON (tried array and object): %w", err)
	}
	*dst = []psTaskRecord{single}
	return nil
}
