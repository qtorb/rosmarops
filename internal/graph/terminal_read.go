package graph

import "strings"

// TerminalReadRulePrefix marks the capability «read through the terminal on
// the whole computer» (BV-245, CTO-55 option b-i): when an agent's commands run
// without a person, or a model decides them, on the whole computer, a command
// can read any file the user can read, so the terminal reaches every
// credential. It is a read_files capability of its own with target_scope
// machine (so its weight is the «any project» one), never the agent's
// project read capability; its reaches edges are inferred and carry the mode's
// premise assumption, if the mode has one.
const TerminalReadRulePrefix = "terminal: "

// IsTerminalRead reports whether c is a terminal-read capability.
func IsTerminalRead(c Node) bool {
	return c.Type == CapReadFiles && strings.HasPrefix(c.Rule, TerminalReadRulePrefix)
}

// terminalReadScope is the terminal-read capability's target_scope.
const terminalReadScope = "machine"
