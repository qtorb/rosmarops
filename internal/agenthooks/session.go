// Package agenthooks is RosmarOps inside Claude Code (BV-226, CTO-44 §4a-§4d):
// the session check a SessionStart hook runs, the status line, and what the
// report needs to offer them. It only reads Claude Code's files, never the
// network, never another process; the only files it writes are the
// project snapshots in <reports>/data/session/ and, from a run,
// <reports>/data/statusline.txt (invariant 2).
package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// Kinds of what a session check reports.
const (
	KindAllow      = "allow"
	KindMode       = "mode"
	KindHook       = "hook"
	KindMCP        = "mcp"
	KindMCPChanged = "mcp_changed"
	KindSkill      = "skill"
	KindAgent      = "agent"
)

// kindOrder is the order items are told in.
var kindOrder = map[string]int{KindAllow: 0, KindMode: 1, KindHook: 2, KindMCP: 3, KindMCPChanged: 4, KindSkill: 5, KindAgent: 6}

// MaxItems is how many items a message lists (BV-226 §1); the rest is
// session.item.more.
const MaxItems = 5

// MaxShown is the longest a name or rule from the configuration is shown, in
// characters, «…» included when it is cut.
const MaxShown = 60

// MaxSnapshots is how many project snapshots are kept; past it the oldest is
// removed (CTO-44 §4a).
const MaxSnapshots = 200

// SessionDir is the snapshots' folder inside the reports' data folder.
const SessionDir = "session"

// Item is one thing a Claude Code configuration brings: its comparison key,
// a fingerprint of its value (empty when the key says it all), whether it
// comes from the project's own files, and the text to show, read from the
// file now (only fingerprints and names are ever stored).
type Item struct {
	Key     string
	Hash    string
	Project bool
	Kind    string
	Shown   string
}

// Input is the part of a hook's standard input the check uses.
type Input struct {
	Cwd string
}

// cursorKeys are fields every Cursor hook receives and Claude Code's input
// never has (cursor.com/docs/hooks, read by the advisor on 13-11): with
// either, the input is Cursor's, whatever its event name says (BV-226 §1,
// the advisor's rule of 13-11 09:15).
var cursorKeys = []string{"cursor_version", "workspace_roots"}

// ParseInput recognises the input session-check acts on (BV-226 §1): none of
// Cursor's cursor_version or workspace_roots, hook_event_name exactly
// "SessionStart" and source "startup" or "resume"
// (https://code.claude.com/docs/en/hooks#sessionstart-input), and an absolute
// cwd, the project to check. Cursor's sessionStart carries neither source
// nor cwd, so it is out both ways, however Cursor translates the event. Any
// other input — a clear, compact or fork, or anything else — is not ok, and
// the check says and writes nothing.
func ParseInput(raw []byte) (Input, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return Input{}, false
	}
	for _, k := range cursorKeys {
		if _, ok := m[k]; ok {
			return Input{}, false
		}
	}
	str := func(k string) string {
		var s string
		if v, ok := m[k]; ok {
			_ = json.Unmarshal(v, &s)
		}
		return s
	}
	if str("hook_event_name") != "SessionStart" {
		return Input{}, false
	}
	if src := str("source"); src != "startup" && src != "resume" {
		return Input{}, false
	}
	cwd := str("cwd")
	if cwd == "" || !filepath.IsAbs(cwd) {
		return Input{}, false
	}
	return Input{Cwd: filepath.Clean(cwd)}, true
}

// Paths are the files a session check reads (CTO-44 §4a point 2).
type Paths struct {
	UserSettings  string // <config dir>/settings.json
	Managed       string // managed-settings.json, "" where there is none
	ClaudeJSON    string // ~/.claude.json
	UserSkills    string // <config dir>/skills
	Project       string // the project folder (the hook's cwd)
	ProjectConfig string // <project>/.claude
}

// DefaultPaths are the paths for this computer: CLAUDE_CONFIG_DIR or
// ~/.claude, the managed settings file of this system, and the project.
func DefaultPaths(home, project string) Paths {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	return Paths{
		UserSettings:  filepath.Join(dir, "settings.json"),
		Managed:       managedSettings(),
		ClaudeJSON:    filepath.Join(home, ".claude.json"),
		UserSkills:    filepath.Join(dir, "skills"),
		Project:       project,
		ProjectConfig: filepath.Join(project, ".claude"),
	}
}

// managedSettings is Claude Code's managed settings file
// (https://code.claude.com/docs/en/managed-settings); the registry sources of
// Windows would need another process, so the check reads only the file.
func managedSettings() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		return "/etc/claude-code/managed-settings.json"
	}
	return ""
}

// maxFile is the largest settings file the check opens (invariant 6).
const maxFile = 2 << 20

func readJSON(path string) map[string]any {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFile {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// Collect reads the files of p and returns every item they bring, sorted.
func Collect(p Paths) []Item {
	var items []Item
	settings := func(path, scope string, project bool) {
		v := readJSON(path)
		if v == nil {
			return
		}
		if perm, ok := v["permissions"].(map[string]any); ok {
			if allow, ok := perm["allow"].([]any); ok {
				for _, r := range allow {
					if s, ok := r.(string); ok && s != "" {
						items = append(items, Item{Key: "allow|" + scope + "|" + fingerprint(s), Project: project, Kind: KindAllow, Shown: s})
					}
				}
			}
			if m, ok := perm["defaultMode"].(string); ok && m != "" {
				items = append(items, Item{Key: "mode|" + scope, Hash: fingerprint(m), Project: project, Kind: KindMode, Shown: m})
			}
		}
		if hooks, ok := v["hooks"].(map[string]any); ok {
			for event, groups := range hooks {
				gs, _ := groups.([]any)
				for _, g := range gs {
					gm, _ := g.(map[string]any)
					hs, _ := gm["hooks"].([]any)
					for _, h := range hs {
						hm, _ := h.(map[string]any)
						cmd, _ := hm["command"].(string)
						if t, _ := hm["type"].(string); t == "command" && strings.TrimSpace(cmd) != "" {
							items = append(items, Item{Key: "hook|" + scope + "|" + event + "|" + fingerprint(cmd), Project: project, Kind: KindHook, Shown: event})
						}
					}
				}
			}
		}
		if mcp, ok := v["mcpServers"].(map[string]any); ok {
			items = append(items, mcpItems(mcp, scope, project)...)
		}
	}
	settings(p.Managed, "managed", false)
	settings(p.UserSettings, "user", false)
	settings(filepath.Join(p.ProjectConfig, "settings.json"), "project", true)
	settings(filepath.Join(p.ProjectConfig, "settings.local.json"), "local", true)
	if v := readJSON(filepath.Join(p.Project, ".mcp.json")); v != nil {
		if mcp, ok := v["mcpServers"].(map[string]any); ok {
			items = append(items, mcpItems(mcp, "mcpjson", true)...)
		}
	}
	// ~/.claude.json: the user's MCP servers and this project's entry (its
	// local MCP servers); trust is read by the run, not told here.
	if v := readJSON(p.ClaudeJSON); v != nil {
		if mcp, ok := v["mcpServers"].(map[string]any); ok {
			items = append(items, mcpItems(mcp, "claudejson", false)...)
		}
		if projects, ok := v["projects"].(map[string]any); ok {
			for path, entry := range projects {
				if filepath.Clean(path) != p.Project {
					continue
				}
				if em, ok := entry.(map[string]any); ok {
					if mcp, ok := em["mcpServers"].(map[string]any); ok {
						items = append(items, mcpItems(mcp, "claudejson-project", false)...)
					}
				}
			}
		}
	}
	names := func(dir, scope, kind string, project bool) {
		es, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range es {
			n := e.Name()
			if kind == KindAgent {
				if e.IsDir() || !strings.HasSuffix(n, ".md") {
					continue
				}
				n = strings.TrimSuffix(n, ".md")
			} else if !e.IsDir() {
				continue
			}
			items = append(items, Item{Key: kind + "|" + scope + "|" + n, Project: project, Kind: kind, Shown: n})
		}
	}
	names(p.UserSkills, "user", KindSkill, false)
	names(filepath.Join(p.ProjectConfig, "skills"), "project", KindSkill, true)
	names(filepath.Join(p.ProjectConfig, "agents"), "project", KindAgent, true)
	sortItems(items)
	return items
}

// mcpItems are the MCP servers of one mcpServers object. Their fingerprint
// is where they point: the command and its arguments, or the URL; never env
// nor headers (BV-226 §1).
func mcpItems(mcp map[string]any, scope string, project bool) []Item {
	var out []Item
	for name, s := range mcp {
		sm, _ := s.(map[string]any)
		dest := map[string]any{"command": sm["command"], "args": sm["args"], "url": sm["url"]}
		b, _ := json.Marshal(dest)
		out = append(out, Item{Key: "mcp|" + scope + "|" + name, Hash: fingerprint(string(b)), Project: project, Kind: KindMCP, Shown: name})
	}
	return out
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if kindOrder[items[i].Kind] != kindOrder[items[j].Kind] {
			return kindOrder[items[i].Kind] < kindOrder[items[j].Kind]
		}
		return items[i].Key < items[j].Key
	})
}

// Snapshot is what is stored per project: each item's key and fingerprint.
type Snapshot struct {
	V     int               `json:"v"`
	Items map[string]string `json:"items"`
}

// SnapshotOf is the snapshot of items.
func SnapshotOf(items []Item) Snapshot {
	s := Snapshot{V: 1, Items: map[string]string{}}
	for _, it := range items {
		s.Items[it.Key] = it.Hash
	}
	return s
}

// SnapshotPath is the project's snapshot in the reports' data folder: 16 hex
// of the SHA-256 of the project's real path.
func SnapshotPath(dataDir, project string) string {
	real, err := filepath.EvalSymlinks(project)
	if err != nil {
		real = project
	}
	sum := sha256.Sum256([]byte(filepath.Clean(real)))
	return filepath.Join(dataDir, SessionDir, hex.EncodeToString(sum[:8])+".json")
}

// ReadSnapshot reads a project's snapshot; ok is false when there is none
// (the first time this project is opened) or it cannot be read.
func ReadSnapshot(path string) (Snapshot, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, false
	}
	var s Snapshot
	if json.Unmarshal(b, &s) != nil || s.V != 1 || s.Items == nil {
		return Snapshot{}, false
	}
	return s, true
}

// Tell is what the check says: on the first opening, what the project's own
// files bring; afterwards, what is new or changed since the snapshot, of the
// user and of the project. An MCP server whose destination changed is told
// as mcp_changed; a permission mode that changed is told with its new value.
func Tell(items []Item, prev Snapshot, first bool) []Item {
	var out []Item
	for _, it := range items {
		if first {
			if it.Project {
				out = append(out, it)
			}
			continue
		}
		old, seen := prev.Items[it.Key]
		switch {
		case !seen:
			out = append(out, it)
		case old != it.Hash:
			if it.Kind == KindMCP {
				it.Kind = KindMCPChanged
			}
			out = append(out, it)
		}
	}
	sortItems(out)
	return out
}

// Shown is a name or rule from the configuration as the person sees it
// (BV-226 §1): no line breaks nor control or format characters, at most 60
// characters with «…» when cut, between «».
func Shown(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	t := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(t) > MaxShown {
		t = strings.TrimSpace(string([]rune(t)[:MaxShown-1])) + "…"
	}
	return "«" + t + "»"
}

// Message is the systemMessage for items, or "" when there is nothing to say.
func Message(cat catalog.Catalog, items []Item, first bool) string {
	if len(items) == 0 {
		return ""
	}
	title := "session.changes.title"
	if first {
		title = "session.first.title"
	}
	lines := []string{cat.Text(title, nil)}
	for i, it := range items {
		if i == MaxItems {
			lines = append(lines, "- "+cat.Text("session.item.more", map[string]string{"count": strconv.Itoa(len(items) - MaxItems)}))
			break
		}
		lines = append(lines, "- "+itemText(cat, it))
	}
	lines = append(lines, cat.Text("session.more", nil))
	return strings.Join(lines, "\n")
}

func itemText(cat catalog.Catalog, it Item) string {
	v := Shown(it.Shown)
	switch it.Kind {
	case KindAllow:
		return cat.Text("session.item.allow_rule", map[string]string{"rule": v})
	case KindMode:
		return cat.Text("session.item.default_mode", map[string]string{"mode": v})
	case KindHook:
		return cat.Text("session.item.hook", map[string]string{"event": v})
	case KindMCPChanged:
		return cat.Text("session.item.mcp_changed", map[string]string{"name": v})
	case KindMCP:
		return cat.Text("session.item.mcp", map[string]string{"name": v})
	case KindSkill:
		return cat.Text("session.item.skill", map[string]string{"name": v})
	default:
		return cat.Text("session.item.agent", map[string]string{"name": v})
	}
}

// Output is the hook's whole standard output: one JSON object whose only
// field is systemMessage, the text Claude Code shows the person and not the
// model (https://code.claude.com/docs/en/hooks#json-output).
func Output(w io.Writer, message string) error {
	b, err := json.Marshal(struct {
		SystemMessage string `json:"systemMessage"`
	}{message})
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// WriteSnapshot stores s at path when it differs from what is there:
// atomically (a .tmp beside it, then a rename), and then keeps at most
// MaxSnapshots in the folder, removing the oldest. It returns whether it
// wrote.
func WriteSnapshot(path string, s Snapshot) (bool, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == string(b) {
		return false, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, prune(dir, path)
}

// prune removes the oldest snapshots past MaxSnapshots, never keep.
func prune(dir, keep string) error {
	es, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type snap struct {
		path string
		mod  int64
	}
	var all []snap
	for _, e := range es {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, snap{filepath.Join(dir, e.Name()), fi.ModTime().UnixNano()})
	}
	if len(all) <= MaxSnapshots {
		return nil
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod < all[j].mod })
	var errs []error
	for _, s := range all[:len(all)-MaxSnapshots] {
		if s.path != keep {
			if err := os.Remove(s.path); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
