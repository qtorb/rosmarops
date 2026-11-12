// Package firstrun finds the projects of a first use with no questions
// (decision 2026-10-27, primer uso con un solo .exe, U3) and, when none is
// found, asks Windows's folder picker (U4). It only reads: JSON files the
// agents already keep (Claude Code's ~/.claude.json, Cursor's and VS Code's
// workspace.json) and folder entries; never state.vscdb (SQLite).
package firstrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/scan/projects"
)

// MaxRepos is the most repositories a search keeps (CTO-38 G7).
const MaxRepos = projects.MaxRepos

// maxJSONBytes bounds every JSON file read (invariant 6).
const maxJSONBytes = 2 * 1024 * 1024

// Sources, in U3's order.
const (
	SourceClaude    = "claude_projects"  // a) the keys of projects in ~/.claude.json
	SourceWorkspace = "editor_workspace" // b) Cursor's and VS Code's workspace.json
	SourceFolder    = "usual_folder"     // c) one level under the usual folders
	SourcePicked    = "picked_folder"    // U4: the folder chosen in the picker
)

// Rules: why a candidate entered or not.
const (
	RuleAccepted      = "accepted"
	RuleNotLocal      = "not_local_folder" // b) not a local file:// URI (remote, WSL, UNC)
	RuleNotFolder     = "not_a_folder"     // missing, or not a folder
	RuleNoGit         = "no_git"           // no .git entry
	RuleHomeOrRoot    = "home_or_drive_root"
	RuleSystemFolder  = "system_folder" // under AppData, LocalAppData, Program Files or the Windows folder
	RuleExcluded      = "forced_exclusion"
	RuleDuplicate     = "duplicate"
	RuleOverLimit     = "over_limit"
	RuleUnreadableSrc = "source_not_read" // a JSON file that could not be read or parsed
)

// Env is where a first use looks: the user's folder and the Windows folders
// a project never lives in.
type Env struct {
	Home, AppData, LocalAppData, ProgramFiles, ProgramFilesX86, SystemRoot string
}

// EnvFromOS reads Env from the environment.
func EnvFromOS() Env {
	home, _ := os.UserHomeDir()
	return Env{
		Home: home, AppData: os.Getenv("APPDATA"), LocalAppData: os.Getenv("LOCALAPPDATA"),
		ProgramFiles: os.Getenv("ProgramFiles"), ProgramFilesX86: os.Getenv("ProgramFiles(x86)"), SystemRoot: os.Getenv("SystemRoot"),
	}
}

// Decision is one candidate and the rule that kept it or left it out.
type Decision struct {
	Source, Path, Rule string
}

// Result is the repositories kept, in order, and every decision.
type Result struct {
	Repos     []string
	Decisions []Decision
	Over      int // repositories left out by MaxRepos
}

// Detect is the first use's search (CTO-38 §2.3): the project search over
// the user folder with the indicators, exactly as every review runs it. The
// first configuration only needs to know whether it found any.
func Detect(env Env) Result {
	found := projects.Search(env.Options(nil))
	res := Result{Repos: found.Repos()}
	for _, it := range found.Items {
		res.Decisions = append(res.Decisions, Decision{Source: string(it.Source), Path: it.Path, Rule: RuleAccepted})
	}
	for _, n := range found.Skipped {
		if n.Reason == projects.ReasonOverMax {
			res.Over = n.Count
		}
	}
	return res
}

// Options are the project search's inputs for env: the user folder, the
// Windows folders a project never lives in and the indicators (CTO-38 §1.1),
// with roots (repo_roots) to walk as well.
func (env Env) Options(roots []string) projects.Options {
	return projects.Options{
		Home: env.Home, SearchHome: true, Roots: roots, Indicators: Indicators(env),
		SystemDirs: []string{env.AppData, env.LocalAppData, env.ProgramFiles, env.ProgramFilesX86, env.SystemRoot},
	}
}

// Indicators are the repositories other tools already name: the keys of
// projects in ~/.claude.json (a) and the local folders of Cursor's and VS
// Code's workspace.json (b). They are never walked; the search accepts each
// one that passes its filters (CTO-38 §1.1, point 3).
func Indicators(env Env) []projects.Indicator {
	var res Result
	var out []projects.Indicator
	for _, p := range sorted(claudeProjects(env, &res)) {
		out = append(out, projects.Indicator{Path: p, Source: projects.SourceClaude})
	}
	for _, p := range workspaceFolders(env, &res) {
		out = append(out, projects.Indicator{Path: p, Source: projects.SourceWorkspace})
	}
	return out
}

// sorted orders paths alphabetically without regard to case, then in byte
// order, so that the order never depends on a map's (BV-79).
func sorted(paths []string) []string {
	sort.Slice(paths, func(i, j int) bool {
		if a, b := strings.ToLower(paths[i]), strings.ToLower(paths[j]); a != b {
			return a < b
		}
		return paths[i] < paths[j]
	})
	return paths
}

// readJSON reads a bounded JSON file into v; false when it is missing.
func readJSON(path string, v any) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return true, err
	}
	if info.Size() > maxJSONBytes {
		return true, fmt.Errorf("%d bytes > %d (invariant 6)", info.Size(), maxJSONBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	return true, json.Unmarshal(raw, v)
}

// claudeProjects is a): the keys of projects in ~/.claude.json, the only
// part read.
func claudeProjects(env Env, res *Result) []string {
	if env.Home == "" {
		return nil
	}
	path := filepath.Join(env.Home, ".claude.json")
	var f struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if found, err := readJSON(path, &f); found && err != nil {
		res.Decisions = append(res.Decisions, Decision{Source: SourceClaude, Path: path, Rule: RuleUnreadableSrc})
		return nil
	}
	paths := make([]string, 0, len(f.Projects))
	for p := range f.Projects {
		paths = append(paths, p)
	}
	return paths
}

// workspaceFolders is b): the folder of each workspace.json under Cursor's
// and VS Code's workspaceStorage, only when it is a local file:// URI.
func workspaceFolders(env Env, res *Result) []string {
	if env.AppData == "" {
		return nil
	}
	var paths []string
	for _, app := range []string{"Cursor", "Code"} {
		dirs, _ := filepath.Glob(filepath.Join(env.AppData, app, "User", "workspaceStorage", "*", "workspace.json"))
		sort.Strings(dirs)
		for _, ws := range dirs {
			var f struct {
				Folder string `json:"folder"`
			}
			if _, err := readJSON(ws, &f); err != nil {
				res.Decisions = append(res.Decisions, Decision{Source: SourceWorkspace, Path: ws, Rule: RuleUnreadableSrc})
				continue
			}
			if f.Folder == "" {
				continue // a multi-root workspace or an empty window: no folder
			}
			p, ok := LocalPath(f.Folder)
			if !ok {
				res.Decisions = append(res.Decisions, Decision{Source: SourceWorkspace, Path: f.Folder, Rule: RuleNotLocal})
				continue
			}
			paths = append(paths, p)
		}
	}
	return paths
}

// LocalPath is the path of a local file:// URI (file:///c%3A/Users/a/x →
// c:/Users/a/x, file:///home/a/x → /home/a/x). Any other scheme
// (vscode-remote://, WSL), a host (UNC, wsl.localhost) or a bad escape is
// not local.
func LocalPath(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok || !strings.HasPrefix(rest, "/") {
		return "", false // another scheme, or a host: file://server/share
	}
	p, ok := unescape(rest)
	if !ok || strings.HasPrefix(p, "//") || strings.Contains(p, `\\`) {
		return "", false
	}
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' && isLetter(p[1]) {
		p = p[1:] // /c:/Users → c:/Users
	}
	return filepath.FromSlash(p), true
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// unescape decodes %XX escapes (UTF-8); false on a malformed one. It is
// here so that no net/url enters the binary (invariant 1).
func unescape(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		hi, ok1 := unhex(s[i+1])
		lo, ok2 := unhex(s[i+2])
		if !ok1 || !ok2 {
			return "", false
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), true
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
