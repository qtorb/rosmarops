// Package problems is the local problem notebook (BV-210, telemetría pasiva).
// It records, on the machine and never leaving it on its own, a closed list of
// anomaly codes RosmarOps ran into during a review — never paths, file or
// folder names, the user or computer name, file contents, keys, config values,
// the IP, or the free text of error messages (decision 2026-11-10, §1). The
// notebook is the only thing the send path reads; sending happens only with the
// person's permission (the «Send» button or the saved checkbox), in
// internal/spacenet.
//
// Everything here is local and does no network and no I/O outside the reports
// data directory (invariant 2): the notebook and the run mark both live next to
// rosmarops-sends.log.
package problems

import "github.com/qtorb/rosmarops/internal/knowledge"

// Code is one anomaly code. The list is closed: these seven values and no
// others (§1). Each maps to a {summary} line in the catalog for the window.
type Code string

const (
	// RunUnfinished: at startup, the previous review had started and did not
	// finish — it was killed, hung, or closed abruptly. Detected by the run
	// mark (StartReview / EndReview); there is no other signal for it.
	RunUnfinished Code = "run_unfinished"
	// StepFailed: one part of the review failed internally and its results were
	// discarded. Detected by RecoverStep (internal/scan/runner.go).
	StepFailed Code = "step_failed"
	// TimeRanOut: the review ran out of time, whole or in one project. Detected
	// by the global timeout and per-repo scan_timeout.
	TimeRanOut Code = "time_ran_out"
	// OutputInvalid: a result did not pass its own format check before leaving.
	// Detected by results.v1 validation at the send gate (since 0.10.3).
	OutputInvalid Code = "output_invalid"
	// SpaceRejected: the space refused a send or a connection (401, 409, 4xx,
	// 5xx). Detected from the send result.
	SpaceRejected Code = "space_rejected"
	// ConfigUnreadable: an agent config file in a format RosmarOps knows could
	// not be parsed. Detected from the graph coverage parse_error reason; only
	// the code is recorded, never the read text (S2 of BV-202).
	ConfigUnreadable Code = "config_unreadable"
	// ReportNotOpened: the report could not be opened nor the notice shown.
	// Detected in internal/notify / the notice finish.
	ReportNotOpened Code = "report_not_opened"
)

// Codes is every valid code, in the order of §1.
var Codes = []Code{
	RunUnfinished, StepFailed, TimeRanOut, OutputInvalid,
	SpaceRejected, ConfigUnreadable, ReportNotOpened,
}

var validCode = func() map[Code]bool {
	m := make(map[Code]bool, len(Codes))
	for _, c := range Codes {
		m[c] = true
	}
	return m
}()

// ValidCode reports whether c is one of the seven codes.
func ValidCode(c Code) bool { return validCode[c] }

// Step values name the part of the review a problem belongs to. The list is
// closed (§1): the scanner names, plus the logical steps for detections that do
// not come from a single scanner. All match problems.v1's step pattern
// ^[a-z0-9_]{1,40}$.
const (
	StepReview    = "review"     // the whole review (run_unfinished, global timeout)
	StepAgents    = "agents"     // reading an agent's configuration (config_unreadable)
	StepResults   = "results"    // building/validating the results package (output_invalid)
	StepSpaceSend = "space_send" // sending to the space (space_rejected)
	StepNotify    = "notify"     // opening the report / showing the notice (report_not_opened)
)

// scannerSteps are the scanner names used verbatim as the step for step_failed
// and per-repo time_ran_out (they are the Step.Name values in cmd/rosmarops,
// plus "graph" for the out-of-Run graph build).
var scannerSteps = []string{
	"vscode", "mcp", "credentials", "graph", "shellprofile",
	"scheduledtasks", "avexclusions", "globals", "repos",
}

var validStep = func() map[string]bool {
	m := map[string]bool{
		StepReview: true, StepAgents: true, StepResults: true,
		StepSpaceSend: true, StepNotify: true,
	}
	for _, s := range scannerSteps {
		m[s] = true
	}
	return m
}()

// ValidStep reports whether s is one of the closed-list step tokens.
func ValidStep(s string) bool { return validStep[s] }

// knownAgents is the closed set of product ids that may appear as a problem's
// agent (BV-210 §1/§7). The agent of a config_unreadable is always one of these
// — a scored AI client's product id, never a path, a user or computer name, or
// anything typed. Restricting the sent agent to this set (stricter than the
// problems.v1 pattern, which BA accepts the same) means a tampered notebook
// cannot smuggle user data through the agent field.
var knownAgents = map[string]bool{
	knowledge.ClaudeCodeProduct:     true,
	knowledge.CursorProduct:         true,
	knowledge.HermesProduct:         true,
	knowledge.CodexProduct:          true,
	knowledge.GrokBuildProduct:      true,
	knowledge.GitHubCopilotProduct:  true,
	knowledge.AntigravityCLIProduct: true,
	knowledge.GeminiCLIProduct:      true,
	knowledge.ClineProduct:          true,
}

// ValidAgent reports whether s is one of the known product ids that may be sent
// as a problem's agent (§7). It is deliberately stricter than the problems.v1
// agent pattern: only a closed list of product names, never free text.
func ValidAgent(s string) bool { return knownAgents[s] }

// KnownAgents returns the product ids ValidAgent accepts, for tests.
func KnownAgents() []string {
	out := make([]string, 0, len(knownAgents))
	for a := range knownAgents {
		out = append(out, a)
	}
	return out
}

// agentNames is the display name shown for each known product id in the
// window and the terminal question (CTO-32 N3). It mirrors the report's own
// client names. Only the display changes: the value recorded and sent is
// always the product id (ValidAgent's closed list), never this name.
var agentNames = map[string]string{
	knowledge.ClaudeCodeProduct:     "Claude Code",
	knowledge.CursorProduct:         "Cursor",
	knowledge.HermesProduct:         "Hermes Agent",
	knowledge.CodexProduct:          "Codex",
	knowledge.GrokBuildProduct:      "Grok Build",
	knowledge.GitHubCopilotProduct:  "GitHub Copilot",
	knowledge.AntigravityCLIProduct: "Antigravity CLI",
	knowledge.GeminiCLIProduct:      "Gemini CLI",
	knowledge.ClineProduct:          "Cline",
}

// AgentName is the product's display name for a known agent id, for the
// {agent} slot of the prompt (CTO-32 N3). An id with no name (should not
// happen for a valid agent) falls back to the id itself.
func AgentName(id string) string {
	if name, ok := agentNames[id]; ok {
		return name
	}
	return id
}
