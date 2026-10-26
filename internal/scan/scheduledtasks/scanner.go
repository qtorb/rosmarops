// Package scheduledtasks detects new Windows scheduled tasks since the last
// baseline. New tasks are YELLOW; new tasks with a logon trigger are RED —
// logon-triggered tasks are the most common Windows persistence mechanism.
package scheduledtasks

import (
	"time"

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

// SeenWithinDays is the window for "recent" tasks. Tasks registered more than
// this many days ago are ignored — they were presumably reviewed when they first
// appeared or are beyond the drift detection window.
// Hardcoded per CLAUDE.md invariant §5 (resource limits, not configurable).
const SeenWithinDays = 7

// ScanResult holds the output of ScanScheduledTasks.
type ScanResult struct {
	// Findings contains one finding per new task: YELLOW or RED.
	Findings []finding.Finding

	// NewBaseline is the full list of tasks from this scan (within the recency
	// window). The caller must persist this to state.Baselines.ScheduledTasks.
	NewBaseline []state.ScheduledTaskBase

	// AllowListedCount is the number of new tasks suppressed by the allow-list.
	AllowListedCount int
}

// ScanScheduledTasks compares the list of recently registered tasks against
// the baselined task set and emits findings for tasks that are:
//   - new (not in baseline by name) OR modified (sha256 changed), AND
//   - not allow-listed.
//
// Severity is RED for tasks with a logon trigger, YELLOW otherwise.
// Tasks older than SeenWithinDays are excluded from consideration.
//
// The returned NewBaseline contains all tasks within the recency window
// (replacing the old baseline entirely — old tasks age out naturally).
//
// RosmarOps's own weekly review (self.Owns, J6) is no finding.
func ScanScheduledTasks(
	tasks []platform.ScheduledTask,
	baseline []state.ScheduledTaskBase,
	allowList []config.AllowEntryID,
	self Self,
) ScanResult {
	cutoff := time.Now().UTC().Add(-SeenWithinDays * 24 * time.Hour)

	// Build baseline lookup: lowercase name → sha256.
	blByName := make(map[string]string, len(baseline))
	for _, b := range baseline {
		blByName[normName(b.Name)] = b.SHA256
	}

	// Build allow-list lookup: lowercase id → true.
	allowed := make(map[string]bool, len(allowList))
	for _, a := range allowList {
		allowed[normName(a.ID)] = true
	}

	var findings []finding.Finding
	var newBL []state.ScheduledTaskBase
	allowListedCount := 0

	for _, t := range tasks {
		// Date filter: skip tasks older than SeenWithinDays.
		// Tasks with an unknown date (zero) are included conservatively.
		if !t.Date.IsZero() && t.Date.Before(cutoff) {
			continue
		}

		// Update new baseline regardless of allow-list status.
		newBL = append(newBL, state.ScheduledTaskBase{
			Name:   t.Name,
			SHA256: t.SHA256,
		})

		// Check if the task is new or modified.
		baselinedSHA, inBaseline := blByName[normName(t.Name)]
		if inBaseline && baselinedSHA == t.SHA256 {
			continue // known, unchanged — no finding
		}

		// Its own weekly review (decision 2026-10-26, integridad y revisión
		// semanal, J6).
		if self.Owns(t) {
			continue
		}

		// New or modified task — check allow-list.
		if allowed[normName(t.Name)] {
			allowListedCount++
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
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
