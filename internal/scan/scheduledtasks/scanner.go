// Package scheduledtasks detects new Windows scheduled tasks since the last
// baseline. New tasks are YELLOW; new tasks with a logon trigger are RED —
// logon-triggered tasks are the most common Windows persistence mechanism.
package scheduledtasks

import (
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Finding text — approved by human owner (review required before commit).
// ---------------------------------------------------------------------------

const (
	whyScheduledTaskNew = "A Windows scheduled task was registered recently that was not present in your " +
		"baseline. Scheduled tasks persist across reboots and execute code automatically — legitimate " +
		"software registers them for auto-update, sync, and telemetry. Malware and compromised installers " +
		"use the same mechanism to achieve persistence without requiring administrative access."

	actionScheduledTaskNew = "Inspect the task in Task Scheduler (taskschd.msc) or run " +
		"`Get-ScheduledTask -TaskName '<name>' | Select-Object *`. Confirm the executable path in the " +
		"Actions tab belongs to software you installed. If legitimate, add an entry under " +
		"`allow_list.scheduled_tasks` in rosmarops.config.json with the task name as `id` and a specific " +
		"note; future runs will silently accept it. If you do not recognize the task, disable it " +
		"immediately with `Disable-ScheduledTask -TaskName '<name>'` and investigate."

	whyScheduledTaskLogonTrigger = "A recently registered Windows scheduled task is configured to run at " +
		"every user logon. Logon-triggered tasks are the most common Windows persistence mechanism: every " +
		"login silently executes the task with your user credentials. Legitimate per-session tools (update " +
		"checkers, sync clients) use this trigger, but so does virtually every piece of Windows malware."

	actionScheduledTaskLogonTrigger = "Open the task immediately in Task Scheduler (taskschd.msc) and " +
		"examine the Actions tab: confirm the executable and its path are from software you intentionally " +
		"installed. If legitimate, add an entry under `allow_list.scheduled_tasks` in rosmarops.config.json " +
		"with the task name as `id` and a note identifying the tool. If you do not recognize the task or " +
		"its executable, disable it immediately with `Disable-ScheduledTask -TaskName '<name>'`, run a " +
		"full Defender scan, and audit recent software installs."
)

// ScanResult holds the output of ScanScheduledTasks.
type ScanResult struct {
	// Findings contains one finding per new task: YELLOW or RED.
	Findings []finding.Finding

	// NewBaseline is the full list of tasks from this scan (within the recency
	// window). The caller must persist this to state.Baselines.ScheduledTasks.
	NewBaseline []state.ScheduledTaskBase

	// AllowListedCount is the number of new tasks suppressed by the allow-list.
	AllowListedCount int

	// Hits are the warnings the allow-list hid, one per warning, with the
	// entry that hid it (decision 2026-10-27, blindaje, B9).
	Hits []config.AllowHit
}

// ScanScheduledTasks compares the list of recently registered tasks against
// the baselined task set and emits findings for tasks that are:
//   - new (not in baseline by name) OR modified (sha256 changed), AND
//   - not allow-listed.
//
// Severity is RED for tasks with a logon trigger, YELLOW otherwise. Every
// task is considered: there is no date window (BV-93, B2/B8), because the
// registration date is chosen by whoever creates the task, so an old date
// must not hide a task.
//
// fresh marks a run with no comparable baseline: the first run ever (empty
// baseline), or the first run after the binary changed, when the task hash
// covers new fields and would otherwise flag every task (BV-93, B2). A fresh
// run records the baseline and emits no findings, exactly like the first time.
//
// The returned NewBaseline contains every task (replacing the old baseline).
//
// RosmarOps's own weekly review that still meets J6 (self.Owns) is no finding;
// one that runs this same exe but no longer meets J6 is own_task_changed.
func ScanScheduledTasks(
	tasks []platform.ScheduledTask,
	baseline []state.ScheduledTaskBase,
	allowList []config.AllowEntryID,
	self Self,
	fresh bool,
) ScanResult {
	// Build baseline lookup: lowercase name → sha256.
	blByName := make(map[string]string, len(baseline))
	for _, b := range baseline {
		blByName[normName(b.Name)] = b.SHA256
	}
	fresh = fresh || len(baseline) == 0

	// Build allow-list lookup: lowercase id → true.
	allowed := make(map[string]config.AllowEntryID, len(allowList))
	for _, a := range allowList {
		allowed[normName(a.ID)] = a
	}
	var hits []config.AllowHit

	var findings []finding.Finding
	var newBL []state.ScheduledTaskBase
	allowListedCount := 0

	for _, t := range tasks {
		// Update new baseline regardless of allow-list or fresh status.
		newBL = append(newBL, state.ScheduledTaskBase{
			Name:   t.Name,
			SHA256: t.SHA256,
		})

		// Its own weekly review that still meets J6 (decision 2026-10-26,
		// integridad y revisión semanal, J6): no finding.
		if self.Owns(t) {
			continue
		}

		// A fresh run has no comparable baseline: record it, warn about
		// nothing (BV-93, B2).
		if fresh {
			continue
		}

		// A task that runs this same rosmarops.exe but no longer meets J6:
		// its own weekly task was altered (BV-93, B2). Allow-list by name is
		// not honoured here: a tampered own task stays visible.
		if self.ownButChanged(t) {
			findings = append(findings, ownTaskChangedFinding(t))
			continue
		}

		// Check if the task is new or modified.
		baselinedSHA, inBaseline := blByName[normName(t.Name)]
		if inBaseline && baselinedSHA == t.SHA256 {
			continue // known, unchanged — no finding
		}

		// New or modified task — check allow-list.
		if a, ok := allowed[normName(t.Name)]; ok {
			allowListedCount++
			hits = append(hits, config.AllowHit{List: config.ListScheduledTasks, ID: a.ID, Note: a.Note})
			continue
		}

		// Emit finding.
		var f finding.Finding
		if t.HasLogonTrigger {
			id := taskFindingID("scheduled_task_logon_trigger", t.Name)
			f = finding.NewFinding(id, finding.SeverityRed, "scheduled_task_logon_trigger",
				"New scheduled task with logon trigger: "+t.Name)
			f.WhyItMatters = whyScheduledTaskLogonTrigger
			f.SuggestedAction = actionScheduledTaskLogonTrigger
			f.Confidence = "high"
		} else {
			id := taskFindingID("scheduled_task_new", t.Name)
			f = finding.NewFinding(id, finding.SeverityYellow, "scheduled_task_new",
				"New scheduled task: "+t.Name)
			f.WhyItMatters = whyScheduledTaskNew
			f.SuggestedAction = actionScheduledTaskNew
			f.Confidence = "medium"
		}
		f.Evidence = finding.Evidence{
			Extra: map[string]string{
				"task_name":         t.Name,
				"task_path":         t.Path,
				"has_logon_trigger": boolStr(t.HasLogonTrigger),
				"current_sha256":    t.SHA256[:16] + "…",
			},
		}
		findings = append(findings, f)
	}

	return ScanResult{
		Findings:         findings,
		NewBaseline:      newBL,
		AllowListedCount: allowListedCount,
		Hits:             hits,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ownTaskChangedFinding(t platform.ScheduledTask) finding.Finding {
	// The own weekly task no longer meets J6: texts from the catalog's
	// finding.own_task_changed.* (BV-93, B2; BV-94), in English, the v1
	// JSON's language.
	cat := catalog.Source()
	f := finding.NewFinding(taskFindingID("own_task_changed", t.Name), finding.SeverityYellow, "own_task_changed", cat.Text("finding.own_task_changed.title", nil))
	f.WhyItMatters = cat.Text("finding.own_task_changed.why", nil)
	f.SuggestedAction = cat.Text("finding.own_task_changed.action", nil)
	f.Confidence = "high"
	f.Evidence = finding.Evidence{Extra: map[string]string{
		"task_name":      t.Name,
		"task_path":      t.Path,
		"current_sha256": t.SHA256[:16] + "\u2026",
	}}
	return f
}

func taskFindingID(category, name string) string {
	h := hash.Bytes([]byte(name))
	return category + ":" + h[:16]
}

func normName(s string) string {
	// Task names are case-insensitive on Windows.
	result := ""
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			result += string(rune(c + 32))
		} else {
			result += string(c)
		}
	}
	return result
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
