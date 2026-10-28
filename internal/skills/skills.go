// Package skills discovers the agent skills on disk (decision 2026-10-28,
// skills y herramientas de agentes, K1; lectura 98, §1): every
// <skill>/SKILL.md, which the agent loads as instructions, and every file
// under <skill>/scripts/, which the agent can run. It is portable: it
// receives the user's folder, the environment and the repository roots, and
// the platform adapter calls it. Existence only: every entry is checked with
// os.Lstat or listed from its folder, symbolic links are never followed and
// no file is opened.
package skills

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// Product codes of the agents that load skills and have no other constant
// (graph.clientDisplayNames). Copilot has no ai_client yet: its skills are
// listed and the graph leaves them out.
const (
	cursorProduct  = "cursor"
	clineProduct   = "cline"
	copilotProduct = "copilot"
)

// Limits (decision K1, item 4): skills listed per skills folder and files
// listed per scripts folder. The rest are not listed and the folder goes to
// coverage.
const (
	MaxSkillsPerFolder = 200
	MaxScriptFiles     = 200
)

// agent identifies an ai_client: product and configuration folder.
type agent struct{ product, configDir string }

// folder is a skills folder and the agents that load it.
type folder struct {
	path   string
	agents []agent
}

// agents are the configuration folders of every agent that loads skills,
// resolved as their ai_clients are.
type agents struct {
	claude, cursor, codex, antigravity, hermes, cline, copilot agent
}

func resolve(home string, env agentdirs.Env) agents {
	if env == nil {
		env = os.LookupEnv
	}
	claudeDir := filepath.Join(home, ".claude")
	if v, ok := env("CLAUDE_CONFIG_DIR"); ok && v != "" {
		claudeDir = v
	}
	appData := filepath.Join(home, "AppData", "Roaming")
	if v, ok := env("APPDATA"); ok && v != "" {
		appData = v
	}
	return agents{
		claude:      agent{knowledge.ClaudeCodeProduct, claudeDir},
		cursor:      agent{cursorProduct, filepath.Join(home, ".cursor")},
		codex:       agent{agentdirs.Codex, agentdirs.CodexHome(home, env)},
		antigravity: agent{agentdirs.Antigravity, agentdirs.AntigravityDir(home)},
		hermes:      agent{agentdirs.Hermes, agentdirs.HermesHome(home, env)},
		cline:       agent{clineProduct, filepath.Join(appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings")},
		copilot:     agent{copilotProduct, filepath.Join(home, ".copilot")},
	}
}

// userFolders are the user's skills folders (lectura 98, §1):
//   - Claude Code: <configuration folder>\skills (CLAUDE_CONFIG_DIR moves it);
//   - Cursor: .cursor\skills and .agents\skills;
//   - Codex: .agents\skills;
//   - Antigravity: .gemini\antigravity\skills and .gemini\antigravity-cli\skills;
//   - Hermes Agent: <HERMES_HOME>\skills (hermes_constants.get_skills_dir);
//   - Cline: .cline\skills;
//   - Copilot: .copilot\skills and .agents\skills.
//
// Continue and Claude Desktop load no skills.
func userFolders(home string, a agents) []folder {
	return []folder{
		{filepath.Join(a.claude.configDir, "skills"), []agent{a.claude}},
		{filepath.Join(home, ".cursor", "skills"), []agent{a.cursor}},
		{filepath.Join(home, ".agents", "skills"), []agent{a.cursor, a.codex, a.copilot}},
		{filepath.Join(home, ".gemini", "antigravity", "skills"), []agent{a.antigravity}},
		{filepath.Join(home, ".gemini", "antigravity-cli", "skills"), []agent{a.antigravity}},
		{filepath.Join(a.hermes.configDir, "skills"), []agent{a.hermes}},
		{filepath.Join(home, ".cline", "skills"), []agent{a.cline}},
		{filepath.Join(home, ".copilot", "skills"), []agent{a.copilot}},
	}
}

// repoFolders are the skills folders of a repository root (lectura 98, §1).
// Hermes Agent loads .hermes\skills and .agents\skills only from the roots
// in skills.trusted_project_dirs (agent/skill_utils.py), which is not read:
// they are listed for it as for the others.
func repoFolders(root string, a agents) []folder {
	return []folder{
		{filepath.Join(root, ".claude", "skills"), []agent{a.claude, a.cursor, a.copilot, a.cline}},
		{filepath.Join(root, ".cursor", "skills"), []agent{a.cursor}},
		{filepath.Join(root, ".agents", "skills"), []agent{a.cursor, a.codex, a.antigravity, a.hermes, a.copilot, a.cline}},
		{filepath.Join(root, ".agent", "skills"), []agent{a.antigravity}},
		{filepath.Join(root, ".hermes", "skills"), []agent{a.hermes}},
		{filepath.Join(root, ".cline", "skills"), []agent{a.cline}},
		{filepath.Join(root, ".clinerules", "skills"), []agent{a.cline}},
		{filepath.Join(root, ".github", "skills"), []agent{a.copilot}},
	}
}

// Discover lists the skills of home's folders and of each repository root,
// one entry per agent that loads the folder. Each repository gets
// repos.PerRepoTimeout (invariant 6); a repository over it, or a run whose
// ctx ends, goes to coverage and the rest continues. An agent that is not
// present still gets its entries: the graph keeps only those of the agents
// it has.
func Discover(ctx context.Context, home string, env agentdirs.Env, repoRoots []string) platform.SkillScan {
	a := resolve(home, env)
	d := &discovery{seen: map[string]bool{}}
	if home != "" {
		for _, f := range userFolders(home, a) {
			d.folder(ctx, f)
		}
	}
	for _, root := range repoRoots {
		d.repo(ctx, root, a)
	}
	sortFiles(d.scan.Instructions)
	sortFiles(d.scan.Executables)
	sort.SliceStable(d.scan.Failed, func(i, j int) bool {
		x, y := d.scan.Failed[i], d.scan.Failed[j]
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Product < y.Product
	})
	return d.scan
}

// repo lists the skills folders of one root under its own deadline; a panic
// sends the root to coverage and the next one continues (invariant 7).
func (d *discovery) repo(ctx context.Context, root string, a agents) {
	ctx, cancel := context.WithTimeout(ctx, repos.PerRepoTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			d.fail(folder{root, []agent{a.claude, a.cursor, a.codex, a.antigravity, a.hermes, a.cline, a.copilot}}, root, graph.ReasonReadError, fmt.Sprintf("panic: %v", r))
		}
	}()
	for _, f := range repoFolders(root, a) {
		d.folder(ctx, f)
	}
}

func sortFiles(files []platform.InstructionFile) {
	sort.SliceStable(files, func(i, j int) bool {
		x, y := files[i], files[j]
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Product < y.Product
	})
}

type discovery struct {
	scan platform.SkillScan
	seen map[string]bool // product, configDir, kind and path already listed
}

func (d *discovery) fail(f folder, path, code, detail string) {
	slog.Warn("skills: not listed", "path", path, "reason", code, "detail", detail)
	for _, a := range f.agents {
		if key := "fail\x00" + a.product + "\x00" + a.configDir + "\x00" + path; !d.seen[key] {
			d.seen[key] = true
			d.scan.Failed = append(d.scan.Failed, platform.InstructionFailure{
				Product: a.product, ConfigDir: a.configDir, Path: path, Reason: graph.Reason(code, detail),
			})
		}
	}
}

func (d *discovery) add(list *[]platform.InstructionFile, kind string, f folder, path string) {
	for _, a := range f.agents {
		if key := kind + "\x00" + a.product + "\x00" + a.configDir + "\x00" + path; !d.seen[key] {
			d.seen[key] = true
			*list = append(*list, platform.InstructionFile{Product: a.product, ConfigDir: a.configDir, Path: path, Shared: true})
		}
	}
}

// expired sends path to coverage when ctx has ended: the repository's
// deadline or the run's.
func (d *discovery) expired(ctx context.Context, f folder, path string) bool {
	if err := ctx.Err(); err != nil {
		d.fail(f, path, graph.ReasonReadError, fmt.Sprintf("not listed: %v (per-repo limit %s, invariant 6)", context.Cause(ctx), repos.PerRepoTimeout))
		return true
	}
	return false
}

// notFollowed sends a symbolic link to coverage: it is never followed.
func (d *discovery) notFollowed(f folder, path string) {
	d.fail(f, path, graph.ReasonSourceUnsupported, "symbolic link not followed")
}

// folder lists the skills of one skills folder: its subfolders, but those in
// the forced exclusion list (invariant 5), up to MaxSkillsPerFolder.
func (d *discovery) folder(ctx context.Context, f folder) {
	info, err := os.Lstat(f.path)
	switch {
	case os.IsNotExist(err):
		return
	case d.expired(ctx, f, f.path):
		return
	case err != nil:
		d.fail(f, f.path, graph.ReasonReadError, err.Error())
		return
	case info.Mode()&fs.ModeSymlink != 0:
		d.notFollowed(f, f.path)
		return
	case !info.IsDir():
		return
	}
	entries, err := os.ReadDir(f.path)
	if err != nil {
		d.fail(f, f.path, graph.ReasonReadError, err.Error())
		// ReadDir returns the entries read before the error.
	}
	n := 0
	for _, e := range entries {
		if repos.IsForcedExclusion(e.Name()) || (!e.IsDir() && e.Type()&fs.ModeSymlink == 0) {
			continue
		}
		if n == MaxSkillsPerFolder {
			d.fail(f, f.path, graph.ReasonFileCountLimitExceeded, fmt.Sprintf("more than %d skills", MaxSkillsPerFolder))
			return
		}
		n++
		if d.expired(ctx, f, f.path) {
			return
		}
		skill := filepath.Join(f.path, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			d.notFollowed(f, skill)
			continue
		}
		d.skill(ctx, f, skill)
	}
}

// skill lists one skill: its SKILL.md and the files under scripts/. A folder
// with no SKILL.md is not a skill.
func (d *discovery) skill(ctx context.Context, f folder, dir string) {
	md := filepath.Join(dir, "SKILL.md")
	if !d.entry(f, md, &d.scan.Instructions, "instructions") {
		return
	}
	scripts := filepath.Join(dir, "scripts")
	info, err := os.Lstat(scripts)
	switch {
	case os.IsNotExist(err):
		return
	case err != nil:
		d.fail(f, scripts, graph.ReasonReadError, err.Error())
		return
	case info.Mode()&fs.ModeSymlink != 0:
		d.notFollowed(f, scripts)
		return
	case !info.IsDir():
		return
	}
	n := 0
	err = filepath.WalkDir(scripts, func(path string, e fs.DirEntry, err error) error {
		if d.expired(ctx, f, scripts) {
			return fs.SkipAll
		}
		if err != nil {
			d.fail(f, path, graph.ReasonReadError, err.Error())
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			if path != scripts && repos.IsForcedExclusion(e.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if n == MaxScriptFiles {
			d.fail(f, scripts, graph.ReasonFileCountLimitExceeded, fmt.Sprintf("more than %d files", MaxScriptFiles))
			return fs.SkipAll
		}
		n++
		info, err := e.Info()
		if err != nil {
			d.fail(f, path, graph.ReasonReadError, err.Error())
			return nil
		}
		d.listed(f, path, info, &d.scan.Executables, "executable")
		return nil
	})
	if err != nil {
		d.fail(f, scripts, graph.ReasonReadError, err.Error())
	}
}

// entry lists path if it exists, and reports whether it does.
func (d *discovery) entry(f folder, path string, list *[]platform.InstructionFile, kind string) bool {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return false
	case err != nil:
		d.fail(f, path, graph.ReasonReadError, err.Error())
		return true
	}
	d.listed(f, path, info, list, kind)
	return true
}

// listed lists an existing entry: a regular file within the size limit
// (invariant 6), or a symbolic link, listed by its own path and not
// followed. Folders and other entries are not listed.
func (d *discovery) listed(f folder, path string, info fs.FileInfo, list *[]platform.InstructionFile, kind string) {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
	case !info.Mode().IsRegular():
		return
	case info.Size() > repos.MaxFileBytes:
		d.fail(f, path, graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), repos.MaxFileBytes))
		return
	}
	d.add(list, kind, f, path)
}
