package profiles

import (
	"encoding/json"
	"strings"
)

// BV-249 §8 (CTO-60 §1, signed by Albert 16-11 and 17-11): «Try it just this
// once» on the P2 cards, macOS and Linux only. A session command turns the
// agent's sandbox on for that session only; it writes no settings file.

// GrokSessionCommand is Grok Build's, literal (BV-249 §8).
const GrokSessionCommand = "grok --sandbox strict --disable-web-search --deny WebFetch"

// claudeSession is CTO-60 §1's --settings JSON, with
// "autoAllowBashIfSandboxed": false (Albert 17-11) and P1's four allowed
// commands; the field order is the command's.
type claudeSession struct {
	Sandbox struct {
		Enabled                  bool `json:"enabled"`
		FailIfUnavailable        bool `json:"failIfUnavailable"`
		AllowUnsandboxedCommands bool `json:"allowUnsandboxedCommands"`
		AutoAllowBashIfSandboxed bool `json:"autoAllowBashIfSandboxed"`
		Network                  struct {
			AllowedDomains  []string `json:"allowedDomains"`
			StrictAllowlist bool     `json:"strictAllowlist"`
		} `json:"network"`
		Filesystem struct {
			DenyRead  []string `json:"denyRead"`
			AllowRead []string `json:"allowRead"`
		} `json:"filesystem"`
	} `json:"sandbox"`
	Permissions struct {
		BlockReadsOutsideWorkingDirectories bool     `json:"blockReadsOutsideWorkingDirectories"`
		Allow                               []string `json:"allow"`
		Deny                                []string `json:"deny"`
	} `json:"permissions"`
}

// ClaudeSessionSettings is the --settings JSON for a project at root (an
// absolute path, escaped by encoding/json).
func ClaudeSessionSettings(root string) string {
	var s claudeSession
	s.Sandbox.Enabled, s.Sandbox.FailIfUnavailable = true, true
	s.Sandbox.Network.AllowedDomains, s.Sandbox.Network.StrictAllowlist = []string{}, true
	s.Sandbox.Filesystem.DenyRead, s.Sandbox.Filesystem.AllowRead = []string{"~/"}, []string{root}
	s.Permissions.BlockReadsOutsideWorkingDirectories = true
	s.Permissions.Allow = []string{"Bash(npm run lint)", "Bash(npm run test *)", "Bash(git status)", "Bash(git diff)"}
	s.Permissions.Deny = []string{"WebFetch", "WebSearch"}
	b, _ := json.Marshal(s) // a fixed struct of strings and bools: cannot fail
	return string(b)
}

// ClaudeSessionCommand is the command a person pastes in a terminal: the JSON
// in single quotes, a single quote inside it written as '\”.
func ClaudeSessionCommand(root string) string {
	return "claude --settings '" + strings.ReplaceAll(ClaudeSessionSettings(root), "'", `'\''`) + "'"
}
