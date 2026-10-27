package windows

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// cursorProduct is Cursor's product label (graph.productOf).
const cursorProduct = "cursor"

// ListAgentInstructions implements platform.Platform. Existence only: every
// candidate is checked with os.Lstat, which does not follow a symbolic link,
// so a link to a network path is never resolved; no file is opened.
//
// Claude Code (https://code.claude.com/docs/en/memory, "Choose where to put
// CLAUDE.md files", "Organize rules with .claude/rules/", "User-level rules";
// https://code.claude.com/docs/en/claude-directory: CLAUDE_CONFIG_DIR moves
// every ~/.claude path):
//   - <configuration folder>/CLAUDE.md and <configuration folder>/rules/**/*.md;
//   - managed, on Windows only: C:\Program Files\ClaudeCode\CLAUDE.md;
//   - per repo root: CLAUDE.md, .claude/CLAUDE.md, CLAUDE.local.md and
//     .claude/rules/**/*.md (rules are discovered recursively).
//
// Cursor (https://docs.cursor.com/context/rules: "Project rules live in
// .cursor/rules"; "The .cursorrules file in your project root is still
// supported but will be deprecated"), per repo root: .cursorrules and every
// file under .cursor/rules/. The .cursor/rules folders that subfolders of a
// repository can hold ("Subdirectories can include their own .cursor/rules
// directory scoped to that folder") are not walked yet.
//
// <repo>/AGENTS.md, one entry per agent that loads it (decision BV-29, D4),
// marked Shared:
//   - Cursor: "AGENTS.md must be placed in your project root"
//     (https://docs.cursor.com/context/rules);
//   - Claude Code: "Claude Code can read AGENTS.md as your project
//     instructions", and "By default, Claude reads AGENTS.md only when you
//     have no CLAUDE.md in your working directory or above it", where a
//     CLAUDE.md, .claude/CLAUDE.md or CLAUDE.local.md counts
//     (https://code.claude.com/docs/en/memory, "AGENTS.md"). It is listed for
//     Claude Code only when none of those three exists in the repo root;
//     folders above the root and the "Project instructions" setting are not
//     read.
//
// Agents detected and not evaluated (decision 2026-10-27, agentes nuevos,
// N1), each under the configuration folder of its ai_client (agentdirs):
//   - Codex: CODEX_HOME\AGENTS.override.md when it exists, otherwise
//     CODEX_HOME\AGENTS.md (openai/codex, codex-rs/codex-home/src/instructions);
//     and <repo>\AGENTS.md, Shared;
//   - Antigravity: %USERPROFILE%\.gemini\GEMINI.md; per repo root GEMINI.md,
//     AGENTS.md (Shared) and every file under .agents\rules\;
//   - Hermes Agent: per repo root, the first that exists of .hermes.md,
//     HERMES.md, AGENTS.md, CLAUDE.md and .cursorrules (Hermes docs,
//     user-guide/which-file-does-what); the last three are other agents'
//     files, so Shared.
//
// Folders in the forced exclusion list (invariant 5) are not entered, and
// symbolic links to folders are not followed.
func (w *WindowsPlatform) ListAgentInstructions(_ context.Context, repoRoots []string) (platform.InstructionScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.InstructionScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	claudeDir := claudeConfigDir(home)
	cursorDir := filepath.Join(home, ".cursor")

	var scan platform.InstructionScan
	claude := instructionLister{scan: &scan, product: knowledge.ClaudeCodeProduct, configDir: claudeDir}
	cursor := instructionLister{scan: &scan, product: cursorProduct, configDir: cursorDir}
	// The agents detected and not evaluated: only when present.
	has := agentdirs.Products(agentdirs.Detect(home, nil))
	codexDir := agentdirs.CodexHome(home, nil)
	codex := instructionLister{scan: &scan, product: agentdirs.Codex, configDir: codexDir, off: !has[agentdirs.Codex]}
	geminiDir := agentdirs.AntigravityDir(home)
	antigravity := instructionLister{scan: &scan, product: agentdirs.Antigravity, configDir: geminiDir, off: !has[agentdirs.Antigravity]}
	hermes := instructionLister{scan: &scan, product: agentdirs.Hermes, configDir: agentdirs.HermesHome(home, nil), off: !has[agentdirs.Hermes]}

	if override := filepath.Join(codexDir, "AGENTS.override.md"); anyExists(override) {
		codex.file(override)
	} else {
		codex.file(filepath.Join(codexDir, "AGENTS.md"))
	}
	antigravity.file(filepath.Join(geminiDir, "GEMINI.md"))

	claude.file(filepath.Join(claudeDir, "CLAUDE.md"))
	claude.tree(filepath.Join(claudeDir, "rules"), isMarkdown)
	if w.managed != nil {
		claude.file(filepath.Join(w.managed.dir, "CLAUDE.md"))
	}
	for _, root := range repoRoots {
		claude.file(filepath.Join(root, "CLAUDE.md"))
		claude.file(filepath.Join(root, ".claude", "CLAUDE.md"))
		claude.file(filepath.Join(root, "CLAUDE.local.md"))
		claude.tree(filepath.Join(root, ".claude", "rules"), isMarkdown)

		cursor.file(filepath.Join(root, ".cursorrules"))
		cursor.tree(filepath.Join(root, ".cursor", "rules"), anyFile)

		agents := filepath.Join(root, "AGENTS.md")
		cursor.shared(agents)
		// AGENTS.md in subfolders, for Cursor ("Cursor supports AGENTS.md
		// in the project root and subdirectories", decision 2026-10-25,
		// Cursor, K10).
		sub := cursor
		sub.sharedFile = true
		sub.tree(root, func(name string) bool { return name == "AGENTS.md" }, agents)
		if !anyExists(filepath.Join(root, "CLAUDE.md"), filepath.Join(root, ".claude", "CLAUDE.md"), filepath.Join(root, "CLAUDE.local.md")) {
			claude.shared(agents)
		}

		codex.shared(agents)

		antigravity.file(filepath.Join(root, "GEMINI.md"))
		antigravity.shared(agents)
		antigravity.tree(filepath.Join(root, ".agents", "rules"), anyFile)

		for _, c := range []struct {
			name   string
			shared bool
		}{{".hermes.md", false}, {"HERMES.md", false}, {"AGENTS.md", true}, {"CLAUDE.md", true}, {".cursorrules", true}} {
			if p := filepath.Join(root, c.name); !hermes.off && anyExists(p) {
				h := hermes
				h.sharedFile = c.shared
				h.file(p)
				break
			}
		}
	}
	sort.SliceStable(scan.Files, func(i, j int) bool {
		a, b := scan.Files[i], scan.Files[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Product < b.Product
	})
	sort.Slice(scan.Failed, func(i, j int) bool { return scan.Failed[i].Path < scan.Failed[j].Path })
	return scan, nil
}

// anyExists reports whether any of paths exists (os.Lstat, not followed).
func anyExists(paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

func isMarkdown(name string) bool { return strings.EqualFold(filepath.Ext(name), ".md") }

func anyFile(string) bool { return true }

// instructionLister adds the instruction files of one agent to scan.
type instructionLister struct {
	scan      *platform.InstructionScan
	product   string
	configDir string

	// sharedFile marks the files listed as Shared.
	sharedFile bool

	// off lists nothing: the agent is not present.
	off bool
}

func (l instructionLister) fail(path, code, detail string) {
	slog.Warn("instructions: not listed", "path", path, "reason", code, "detail", detail)
	l.scan.Failed = append(l.scan.Failed, platform.InstructionFailure{
		Product: l.product, ConfigDir: l.configDir, Path: path, Reason: graph.Reason(code, detail),
	})
}

// shared lists path, a file several agents load, for this agent.
func (l instructionLister) shared(path string) {
	l.sharedFile = true
	l.file(path)
}

// file lists path if it exists as a file (or a symbolic link, not followed).
func (l instructionLister) file(path string) {
	if l.off {
		return
	}
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return
	case err != nil:
		l.fail(path, graph.ReasonReadError, err.Error())
		return
	}
	l.add(path, info)
}

// add lists an existing entry: a regular file within the size limit, or a
// symbolic link. Directories and other entries are not instruction files.
func (l instructionLister) add(path string, info fs.FileInfo) {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
	case !info.Mode().IsRegular():
		return
	case info.Size() > MaxConfigBytes:
		l.fail(path, graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), MaxConfigBytes))
		return
	}
	l.scan.Files = append(l.scan.Files, platform.InstructionFile{Product: l.product, ConfigDir: l.configDir, Path: path, Shared: l.sharedFile})
}

// tree lists every file under dir whose name matches, recursively, but the
// paths in skip.
func (l instructionLister) tree(dir string, match func(string) bool, skip ...string) {
	if l.off {
		return
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir && os.IsNotExist(err) {
				return nil
			}
			l.fail(path, graph.ReasonReadError, err.Error())
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == dir && !d.IsDir() {
			return nil // not a folder (or a link to one, not followed)
		}
		if d.IsDir() {
			if path != dir && repos.IsForcedExclusion(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !match(d.Name()) {
			return nil
		}
		for _, s := range skip {
			if path == s {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			l.fail(path, graph.ReasonReadError, err.Error())
			return nil
		}
		l.add(path, info)
		return nil
	})
	if err != nil {
		l.fail(dir, graph.ReasonReadError, err.Error())
	}
}
