package profiles

import (
	"path/filepath"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// The guided profiles of BV-238 (CTO-50 §2 Cursor, §3 Grok Build, with the
// adjustments of 14-11 22:11): the steps are shown and the text is copied,
// but the engine does not measure them.

// cursorBlock is the literal ~/.cursor/sandbox.json of Cursor's P0 and P1.
const cursorBlock = `{ "readBoundary": "workspace" }`

// grokP0 and grokP1 are Grok Build's ~/.grok/config.toml for P0 and P1, a
// single file: CTO-50 §3 with deny = ["WebSearch"] after rules, and P1 on the
// built-in "workspace" sandbox profile, without sandbox.toml. No
// requirements.toml: today's documentation honours its lock only from
// root-owned sources (Albert, 15-11).
const grokP0 = `[ui]
permission_mode = "ask"

[permission]
rules = [
  { action = "deny", tool = "edit" },
  { action = "deny", tool = "bash" },
  { action = "deny", tool = "webfetch" },
  { action = "deny", tool = "mcp" },
]
deny = ["WebSearch"]

[sandbox]
profile = "read-only"

[compat.claude]
hooks = false

[compat.cursor]
hooks = false`

const grokP1 = `[ui]
permission_mode = "ask"

[permission]
rules = [
  { action = "allow", tool = "bash", pattern = "npm run lint" },
  { action = "allow", tool = "bash", pattern = "npm run test *" },
  { action = "allow", tool = "bash", pattern = "git status" },
  { action = "allow", tool = "bash", pattern = "git diff" },
  { action = "deny",  tool = "webfetch" },
]
deny = ["WebSearch"]

[sandbox]
profile = "workspace"

[compat.claude]
hooks = false

[compat.cursor]
hooks = false`

// grokP3 is Grok Build's launch command of CTO-50 §3 P3, literal.
const grokP3 = `grok -p "<tarea>" --permission-mode dontAsk --sandbox strict --deny "WebFetch"`

// Guided agents.
const (
	GuidedCursor = "cursor"
	GuidedGrok   = "grok-build"
	GuidedBot    = "grok-bot"
)

// GuidedBlock is what a guided profile copies besides its sentence: Cursor's
// JSON (P0, P1), Grok Build's TOML blocks (P0, P1) or its command (P3).
// Empty when the profile copies nothing.
func GuidedBlock(agent, id string) string {
	switch agent + "/" + id {
	case GuidedCursor + "/" + P0, GuidedCursor + "/" + P1:
		return cursorBlock
	case GuidedGrok + "/" + P0:
		return grokP0
	case GuidedGrok + "/" + P1:
		return grokP1
	case GuidedGrok + "/" + P2:
		return grokP2
	case GuidedGrok + "/" + P3:
		return grokP3
	}
	return ""
}

// grokP2 is Grok Build's «Sandboxed development» (BV-249 §3.2, CTO-58 §1.3):
// P1's block with [sandbox] profile = "strict" in place of "workspace".
var grokP2 = strings.Replace(grokP1, `profile = "workspace"`, `profile = "strict"`, 1)

// GrokApply is s with a Grok Build template's block pasted in the user's
// ~/.grok/config.toml (BV-249 §3.2): its [ui] permission_mode, [sandbox]
// profile and [compat] cells set, its [permission] rules and deny added. A
// missing user layer is added in the configuration folder. ok is false for a
// template that is not measured (only P2) or with no folder to apply it to.
func GrokApply(s platform.GrokScan, id string) (platform.GrokScan, bool) {
	if id != P2 || s.ConfigDir == "" {
		return s, false
	}
	out := s
	out.Layers = append([]platform.GrokLayer(nil), s.Layers...)
	i := -1
	for j, l := range out.Layers {
		if l.Kind == platform.GrokLayerUser && l.Status == platform.MCPSourceRead {
			i = j
		}
	}
	if i < 0 {
		out.Layers = append(out.Layers, platform.GrokLayer{
			Kind: platform.GrokLayerUser, Path: filepath.Join(s.ConfigDir, "config.toml"), Status: platform.MCPSourceRead,
		})
		i = len(out.Layers) - 1
	}
	l := out.Layers[i]
	l.PermissionMode, l.HasPermissionMode, l.Yolo = "ask", true, false
	l.SandboxProfile = "strict"
	l.Rules = append(append([]platform.GrokRule(nil), l.Rules...), grokP1Rules...)
	l.Deny = append(append([]string(nil), l.Deny...), "WebSearch")
	compat := map[string]bool{}
	for k, v := range l.Compat {
		compat[k] = v
	}
	compat["claude.hooks"], compat["cursor.hooks"] = false, false
	l.Compat = compat
	out.Layers[i] = l
	out.Exists = true
	return out, true
}

// grokP1Rules are the [permission] rules of grokP1 and grokP2.
var grokP1Rules = []platform.GrokRule{
	{Action: "allow", Tool: "bash", Pattern: "npm run lint"},
	{Action: "allow", Tool: "bash", Pattern: "npm run test *"},
	{Action: "allow", Tool: "bash", Pattern: "git status"},
	{Action: "allow", Tool: "bash", Pattern: "git diff"},
	{Action: "deny", Tool: "webfetch"},
}
