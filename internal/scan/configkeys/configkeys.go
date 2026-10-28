// Package configkeys turns the key conflicts the adapter found in the
// agents' JSON configuration files (platform.*.KeyConflicts) into red
// config_key_case_conflict findings (decision 2026-10-27, blindaje, B5): a
// key that matches a key RosmarOps reads only without regard to case. The
// agent may not read it, or may read another copy, so the report could be
// describing a configuration that is not the one in use.
package configkeys

import (
	"sort"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Category is the finding's category.
const Category = "config_key_case_conflict"

// conflict is one key of one file.
type conflict struct{ path, pointer string }

// Findings is one red finding per file and key, sorted by id, from every
// file the three scans read. Its texts are the catalog's
// finding.config_key_case_conflict.*, in English (the v1 JSON's language).
func Findings(mcp platform.MCPScan, settings platform.ClaudeSettingsScan, cursor platform.CursorScan) []finding.Finding {
	var all []conflict
	add := func(path string, pointers []string) {
		for _, p := range pointers {
			all = append(all, conflict{path, p})
		}
	}
	for _, s := range mcp.Sources {
		add(s.ConfigPath, s.KeyConflicts)
	}
	files := append([]platform.ClaudeSettingsFile{}, settings.Files...)
	for _, m := range settings.Managed {
		files = append(files, m.Files...)
	}
	if settings.ManagedMCP != nil {
		files = append(files, *settings.ManagedMCP)
	}
	for _, f := range files {
		add(f.ConfigPath, f.KeyConflicts)
	}
	if settings.VSCode != nil {
		add(settings.VSCode.Path, settings.VSCode.KeyConflicts)
	}
	for _, f := range cursor.Files {
		add(f.Path, f.KeyConflicts)
	}

	return build(all)
}

// FromCredentialFiles is one red finding per file and key of the JSON
// credential files read with exact keys (BV-96: Codex's .credentials.json,
// Hermes Agent's auth.json).
func FromCredentialFiles(scan platform.CredentialFileScan) []finding.Finding {
	all := make([]conflict, 0, len(scan.KeyConflicts))
	for _, c := range scan.KeyConflicts {
		all = append(all, conflict{c.Path, c.Pointer})
	}
	return build(all)
}

// build is one finding per file and key, sorted by id.
func build(all []conflict) []finding.Finding {
	cat := catalog.Source()
	seen := map[string]bool{}
	var out []finding.Finding
	for _, c := range all {
		id := Category + ":" + hash.Bytes([]byte(c.path + "\x00" + c.pointer))[:16]
		if seen[id] {
			continue
		}
		seen[id] = true
		f := finding.NewFinding(id, finding.SeverityRed, Category, cat.Text("finding."+Category+".title", nil))
		f.WhyItMatters = cat.Text("finding."+Category+".why", nil)
		f.SuggestedAction = cat.Text("finding."+Category+".action", nil)
		f.Confidence = "high"
		f.Evidence = finding.Evidence{File: c.path, Extra: map[string]string{"key": c.pointer}}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
