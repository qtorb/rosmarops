// Package knowledge holds the product knowledge base: documented behaviour of
// each supported agent product, dated and sourced per entry (decision
// 2026-10-23, Q2 and §3). It is knowledge, not judgement: it says what a
// product does by default, not whether that is dangerous.
//
// The base is embedded JSON, decoded strictly: an unknown field is an error.
package knowledge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Product labels of the bases (graph agent platform).
const (
	ClaudeCodeProduct = "claude-code"
	CursorProduct     = "cursor"
)

// Version is the version of the embedded Claude Code base. It must equal the
// "version" field of claude-code.json (checked by tests) and is recorded as
// provenance.knowledge_version in the graph.
const Version = "2026-10-24"

// CursorVersion is the version of the embedded Cursor base (cursor.json). In
// this cut the graph's knowledge_version stays Claude Code's Version
// (decision 2026-10-25, Cursor, annex 2 point 3).
const CursorVersion = "2026-10-25"

//go:embed claude-code.json
var claudeCodeJSON []byte

//go:embed cursor.json
var cursorJSON []byte

// Base is one product's knowledge base.
type Base struct {
	Product string  `json:"product"`
	Version string  `json:"version"`
	Entries []Entry `json:"entries"`

	// Mitigations are the documented settings that cut a way (decision
	// 2026-10-24, informe legible, E5).
	Mitigations []Mitigation `json:"mitigations"`

	// CodeLaunchers are the command forms that run code given in the rule
	// (decision 2026-10-24, informe legible, §4, E8). Cursor reuses Claude
	// Code's signed table (decision 2026-10-25, Cursor, §1.1).
	CodeLaunchers []CodeLauncher `json:"code_launchers"`

	// ReadCommands and NetworkCommands are RosmarOps's closed lists of
	// commands that read files and that send to the network, matched by
	// prefix in Cursor's terminalAllowlist (decision 2026-10-25, Cursor,
	// annex 2 point 2).
	ReadCommands    []CommandClass `json:"read_commands,omitempty"`
	NetworkCommands []CommandClass `json:"network_commands,omitempty"`
}

// CommandClass is one command of a closed list: its name, lowercase, without
// ".exe".
type CommandClass struct {
	Program   string `json:"program"`
	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

// Mitigation is one documented action and the ways it cuts. Ways are the
// codes of the rules layer: file_tools, bash_read_only, mode_auto,
// mode_bypass, mode_accept_edits, edit_allow, shell_allow, egress_auto,
// egress_bypass and any_reach.
type Mitigation struct {
	ID string `json:"id"`

	// Setting is the settings key it changes in the user settings file
	// ("permissions.ask" for a rule), or "credential_file" for an action on
	// the credential's own files.
	Setting string `json:"setting"`

	// RuleTool is the tool of the rule added to an ask list (Read, Edit).
	RuleTool string `json:"rule_tool,omitempty"`

	// File is the settings file the action changes, from "~/", when it is
	// not the agent's user settings file (Cursor's ~/.cursor/sandbox.json).
	File string `json:"file,omitempty"`

	// Value is the JSON literal the setting takes, for a plain setting.
	Value string `json:"value,omitempty"`

	// Completeness is full, partial, radius_only, or by_cuts (full when
	// it cuts a whole leg of the finding, partial otherwise).
	Completeness string `json:"completeness"`

	// Cuts are the ways it removes; Narrows the ways it restricts without
	// removing them.
	Cuts    []string `json:"cuts"`
	Narrows []string `json:"narrows"`

	// Magnitude is how large the change is: ask 1, deny 2, a one-line user
	// setting 2, a mode change 3, removing from disk 3.
	Magnitude int `json:"magnitude"`

	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

// CodeLauncher is one command form that runs code given in its arguments.
type CodeLauncher struct {
	// Family is second_shell, inline_code, npx, git or env_launcher.
	Family string `json:"family"`

	// Program is the command name, lowercase, without ".exe".
	Program string `json:"program"`

	// Form is the documented form, for reading only.
	Form string `json:"form"`

	// Options are the options that take the code or command (second_shell,
	// inline_code); Package the npx package (npx; none for "npx *");
	// Subcommand the subcommand of an env_launcher.
	Options    []string `json:"options,omitempty"`
	Package    string   `json:"package,omitempty"`
	Subcommand string   `json:"subcommand,omitempty"`

	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

// Entry is one documented default behaviour, expressed as a capability.
type Entry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Approval    string `json:"approval"`
	TargetScope string `json:"target_scope"`
	SourceURL   string `json:"source_url"`
	Verified    string `json:"verified"`

	// Paths and Exceptions are a documented path list, transcribed from
	// SourceURL (only the protected_paths entry has one). A trailing "/"
	// marks a folder, as the documentation writes ~/.claude/dev-mods/; a
	// name without it is a file. "~/.claude/" is Claude Code's configuration
	// folder. In Exceptions, "<id>" and "<project>" stand for one path
	// segment, and a final "*.md" for the Markdown files of that folder.
	// An entry with Paths is knowledge only: it is not a capability.
	Paths      []string `json:"paths,omitempty"`
	Exceptions []string `json:"exceptions,omitempty"`
}

// IsCapability reports whether the entry describes a product_default
// capability, as opposed to a path list.
func (e Entry) IsCapability() bool { return len(e.Paths) == 0 }

// Entry returns the entry with the given id.
func (b Base) Entry(id string) (Entry, bool) {
	for _, e := range b.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// ClaudeCode returns the embedded Claude Code knowledge base.
func ClaudeCode() (Base, error) {
	return decode(claudeCodeJSON)
}

// Cursor returns the embedded Cursor knowledge base, with Claude Code's
// code launcher table (decision 2026-10-25, Cursor, §1.1).
func Cursor() (Base, error) {
	b, err := decode(cursorJSON)
	if err != nil {
		return Base{}, err
	}
	if b.Version != CursorVersion {
		return Base{}, fmt.Errorf("knowledge base cursor: version %q, want %q", b.Version, CursorVersion)
	}
	cc, err := ClaudeCode()
	if err != nil {
		return Base{}, err
	}
	b.CodeLaunchers = cc.CodeLaunchers
	return b, nil
}

// All returns every embedded base, by product: an agent whose product has a
// base is evaluated.
func All() (map[string]Base, error) {
	out := map[string]Base{}
	for _, load := range []func() (Base, error){ClaudeCode, Cursor} {
		b, err := load()
		if err != nil {
			return nil, err
		}
		out[b.Product] = b
	}
	return out, nil
}

// decode parses a base strictly: unknown fields and trailing data are errors,
// and every field of every entry must be non-empty.
func decode(raw []byte) (Base, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var b Base
	if err := dec.Decode(&b); err != nil {
		return Base{}, fmt.Errorf("decode knowledge base: %w", err)
	}
	if dec.More() {
		return Base{}, fmt.Errorf("decode knowledge base: trailing data")
	}
	if b.Product == "" || b.Version == "" {
		return Base{}, fmt.Errorf("knowledge base: product and version are required")
	}
	seen := map[string]bool{}
	for i, e := range b.Entries {
		if e.ID == "" || e.Type == "" || e.Approval == "" || e.TargetScope == "" || e.SourceURL == "" || e.Verified == "" {
			return Base{}, fmt.Errorf("knowledge base %s: entry %d has an empty field", b.Product, i)
		}
		if err := checkPaths(e); err != nil {
			return Base{}, fmt.Errorf("knowledge base %s: entry %s: %w", b.Product, e.ID, err)
		}
		if seen[e.ID] {
			return Base{}, fmt.Errorf("knowledge base %s: duplicate entry id %q", b.Product, e.ID)
		}
		seen[e.ID] = true
	}
	if err := checkMitigations(b.Mitigations); err != nil {
		return Base{}, fmt.Errorf("knowledge base %s: %w", b.Product, err)
	}
	if err := checkLaunchers(b.CodeLaunchers); err != nil {
		return Base{}, fmt.Errorf("knowledge base %s: %w", b.Product, err)
	}
	for _, list := range [][]CommandClass{b.ReadCommands, b.NetworkCommands} {
		if err := checkCommands(list); err != nil {
			return Base{}, fmt.Errorf("knowledge base %s: %w", b.Product, err)
		}
	}
	return b, nil
}

// Mitigation returns the mitigation with the given id.
func (b Base) Mitigation(id string) (Mitigation, bool) {
	for _, m := range b.Mitigations {
		if m.ID == id {
			return m, true
		}
	}
	return Mitigation{}, false
}

// mitigationWays are the way codes a mitigation may cut or narrow.
var mitigationWays = map[string]bool{
	"file_tools": true, "bash_read_only": true, "mode_auto": true, "mode_bypass": true,
	"mode_accept_edits": true, "edit_allow": true, "shell_allow": true,
	"egress_auto": true, "egress_bypass": true, "any_reach": true,
	"mode_auto_review": true,
}

func checkCommands(cs []CommandClass) error {
	seen := map[string]bool{}
	for i, c := range cs {
		if c.Program == "" || c.SourceURL == "" || c.Verified == "" || c.Program != strings.ToLower(c.Program) || strings.ContainsAny(c.Program, " \t") {
			return fmt.Errorf("command %d has an empty or invalid field", i)
		}
		if seen[c.Program] {
			return fmt.Errorf("duplicate command %q", c.Program)
		}
		seen[c.Program] = true
	}
	return nil
}

func checkMitigations(ms []Mitigation) error {
	seen := map[string]bool{}
	for i, m := range ms {
		if m.ID == "" || m.Setting == "" || m.SourceURL == "" || m.Verified == "" || m.Magnitude <= 0 {
			return fmt.Errorf("mitigation %d has an empty field", i)
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate mitigation id %q", m.ID)
		}
		seen[m.ID] = true
		switch m.Completeness {
		case "full", "partial", "radius_only", "by_cuts":
		default:
			return fmt.Errorf("mitigation %s: completeness %q", m.ID, m.Completeness)
		}
		if (m.Setting == "permissions.ask") != (m.RuleTool != "") {
			return fmt.Errorf("mitigation %s: rule_tool goes with permissions.ask only", m.ID)
		}
		if m.File != "" && !strings.HasPrefix(m.File, "~/") {
			return fmt.Errorf("mitigation %s: file %q is not under ~/", m.ID, m.File)
		}
		if m.Value != "" && !json.Valid([]byte(m.Value)) {
			return fmt.Errorf("mitigation %s: value %q is not JSON", m.ID, m.Value)
		}
		for _, w := range append(append([]string{}, m.Cuts...), m.Narrows...) {
			if !mitigationWays[w] {
				return fmt.Errorf("mitigation %s: unknown way %q", m.ID, w)
			}
		}
	}
	return nil
}

func checkLaunchers(ls []CodeLauncher) error {
	for i, l := range ls {
		if l.Program == "" || l.Form == "" || l.SourceURL == "" || l.Verified == "" || l.Program != strings.ToLower(l.Program) {
			return fmt.Errorf("code launcher %d has an empty or invalid field", i)
		}
		var ok bool
		switch l.Family {
		case "second_shell", "inline_code":
			ok = len(l.Options) > 0 && l.Package == "" && l.Subcommand == ""
		case "npx":
			ok = len(l.Options) == 0 && l.Subcommand == ""
		case "git":
			ok = len(l.Options) == 0 && l.Package == "" && l.Subcommand == ""
		case "env_launcher":
			ok = len(l.Options) == 0 && l.Package == "" && l.Subcommand != ""
		}
		if !ok {
			return fmt.Errorf("code launcher %s: fields do not fit family %q", l.Form, l.Family)
		}
	}
	return nil
}

// checkPaths validates the path list of an entry: no empty, padded or
// duplicate path, and exceptions only alongside paths.
func checkPaths(e Entry) error {
	if len(e.Exceptions) > 0 && len(e.Paths) == 0 {
		return fmt.Errorf("exceptions without paths")
	}
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, e.Paths...), e.Exceptions...) {
		if p == "" || strings.TrimSpace(p) != p || strings.Contains(p, `\`) {
			return fmt.Errorf("invalid path %q", p)
		}
		if seen[p] {
			return fmt.Errorf("duplicate path %q", p)
		}
		seen[p] = true
	}
	return nil
}

// AgentExtensions are the VS Code extensions the knowledge base marks as AI
// agents (decision 2026-10-24, memoria, §2.1 and §5 reading 4), by
// extension id (lowercase), with their product name: a new one is a new
// agent in the report's memory; every other new extension stays in the
// technical report.
var AgentExtensions = map[string]string{
	"saoudrizwan.claude-dev": "Cline",
	"continue.continue":      "Continue",
	"github.copilot":         "GitHub Copilot",
	"github.copilot-chat":    "GitHub Copilot",
}
