package windows

import (
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// OwnBinary reports whether a command's program path leads to RosmarOps's own
// binary: an executable whose SHA-256 is the state's current reference
// (CTO-44 §4d, condition 3; the Store alias resolved inside our own package).
// The program sets it before reading the agents; nil recognises nothing, so
// every hook is treated as any other.
var OwnBinary func(path string) bool

// ownHooks finds RosmarOps's own hooks in a user or managed settings
// document (CTO-44 §4d) and returns them with exec minus their pointers. A
// hook is ours only when all of these hold at once:
//  1. it is in user or managed settings (the caller checks the scope);
//  2. its command is exactly «<path> session-check» or «<path> statusline»
//     (the path may be in double quotes), with no other argument, shell
//     operator or variable;
//  3. <path> leads to our own binary (OwnBinary);
//  4. the handler's type is command and its event is SessionStart (or it is
//     the statusLine);
//  5. no matcher and no if makes it run on other events.
//
// Anything else is a hook like any other.
func ownHooks(s settingsFile, exec []string) (own, rest []string) {
	drop := map[string]bool{}
	if groups, ok := s.Hooks["SessionStart"]; ok {
		for g, group := range groups {
			if group.Matcher != "" {
				continue
			}
			for h, handler := range group.Hooks {
				if handler.Type == "command" && handler.If == "" && ownCommand(handler.Command, platform.OwnHookSessionCheck) {
					drop["/hooks/SessionStart/"+strconv.Itoa(g)+"/hooks/"+strconv.Itoa(h)+"/command"] = true
					if !contains(own, platform.OwnHookSessionCheck) {
						own = append(own, platform.OwnHookSessionCheck)
					}
				}
			}
		}
	}
	if sl := s.StatusLine; sl != nil && sl.Type == "command" && ownCommand(sl.Command, platform.OwnHookStatusline) {
		drop["/statusLine/command"] = true
		own = append(own, platform.OwnHookStatusline)
	}
	for _, p := range exec {
		if !drop[p] {
			rest = append(rest, p)
		}
	}
	return own, rest
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// unsafeChars are what would make a command more than «<path> <command>»: shell
// operators, redirections, substitutions, variables, quotes inside the
// path, and line breaks.
const unsafeChars = "&|;<>`$%!^\n\r\t\x00'"

// ownCommand reports whether cmd is exactly «<path> want» or «"<path>" want»
// with <path> leading to our binary.
func ownCommand(cmd, want string) bool {
	var path string
	switch {
	case strings.HasPrefix(cmd, `"`):
		end := strings.Index(cmd[1:], `"`)
		if end < 0 {
			return false
		}
		path = cmd[1 : 1+end]
		if cmd[2+end:] != " "+want {
			return false
		}
	default:
		p, w, ok := strings.Cut(cmd, " ")
		if !ok || w != want || strings.Contains(p, `"`) {
			return false
		}
		path = p
	}
	if path == "" || strings.ContainsAny(path, unsafeChars) || strings.ContainsAny(want, unsafeChars) {
		return false
	}
	return OwnBinary != nil && OwnBinary(path)
}
