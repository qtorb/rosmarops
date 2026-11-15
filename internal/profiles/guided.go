package profiles

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
	case GuidedGrok + "/" + P3:
		return grokP3
	}
	return ""
}
