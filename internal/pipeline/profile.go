package pipeline

import "github.com/qtorb/rosmarops/internal/profiles"

// Profile is in with one measured profile of Claude Code (CTO-50 §1, BV-236
// §4) added to Claude Code's user settings, the way Claude Code merges a
// pasted block. Every other agent keeps its configuration. ok is false when
// there is no Claude Code configuration folder to apply it to.
func Profile(in Input, id string) (Input, bool) {
	s, ok := profiles.Apply(in.Settings, id)
	if !ok {
		return in, false
	}
	out := in
	out.Settings = s
	return out, true
}
