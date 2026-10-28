// Package skills discovers the agent skills on disk (decision 2026-10-28,
// skills y herramientas de agentes, K1; lectura 98, §1): every
// <skill>/SKILL.md, which the agent loads as instructions, and every file
// under <skill>/scripts/, which the agent can run. It is portable: it
// receives the user's folder, the environment and the repository roots, and
// the platform adapter calls it. Existence only: every entry is checked with
// os.Lstat or listed from its folder and no skill file is opened. A link
// (symbolic link or junction) to a skill is read once, never walked: it
// counts only when it names a skill this pass already listed (BV-99b, item
// 1). The one file read is Hermes Agent's config.yaml, for its trusted
// project roots (BV-99b, item 2).
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

	// targets are the folders a link to a skill in this one may point into
	// and count (BV-99b, item 1), by pathKey.
	targets map[string]bool

	// hermesUnknown, when set, is the coverage reason of a project folder
	// Hermes Agent may load and that is not attributed to it: its trusted
	// roots could not be established.
	hermesUnknown string
	hermes        agent
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
		{path: filepath.Join(a.claude.configDir, "skills"), agents: []agent{a.claude}},
		{path: filepath.Join(home, ".cursor", "skills"), agents: []agent{a.cursor}},
		{path: filepath.Join(home, ".agents", "skills"), agents: []agent{a.cursor, a.codex, a.copilot}},
		{path: filepath.Join(home, ".gemini", "antigravity", "skills"), agents: []agent{a.antigravity}},
		{path: filepath.Join(home, ".gemini", "antigravity-cli", "skills"), agents: []agent{a.antigravity}},
		{path: filepath.Join(a.hermes.configDir, "skills"), agents: []agent{a.hermes}},
		{path: filepath.Join(home, ".cline", "skills"), agents: []agent{a.cline}},
		{path: filepath.Join(home, ".copilot", "skills"), agents: []agent{a.copilot}},
	}
}

// repoFolders are the skills folders of a repository root (lectura 98, §1).
// Hermes Agent loads .hermes\skills and .agents\skills only when the root is
// trusted (hermesTrust); otherwise they are not attributed to it, and when
// its trust is unknown the folders go to its coverage.
func repoFolders(root string, a agents, trust hermesTrust) []folder {
	agentsFolder := []agent{a.cursor, a.codex, a.antigravity, a.copilot, a.cline}
	var hermesFolder []agent
	if trust.trusts(root) {
		agentsFolder = append(agentsFolder, a.hermes)
		hermesFolder = []agent{a.hermes}
	}
	fs := []folder{
		{path: filepath.Join(root, ".claude", "skills"), agents: []agent{a.claude, a.cursor, a.copilot, a.cline}},
		{path: filepath.Join(root, ".cursor", "skills"), agents: []agent{a.cursor}},
		{path: filepath.Join(root, ".agents", "skills"), agents: agentsFolder},
		{path: filepath.Join(root, ".agent", "skills"), agents: []agent{a.antigravity}},
		{path: filepath.Join(root, ".hermes", "skills"), agents: hermesFolder},
		{path: filepath.Join(root, ".cline", "skills"), agents: []agent{a.cline}},
		{path: filepath.Join(root, ".clinerules", "skills"), agents: []agent{a.cline}},
		{path: filepath.Join(root, ".github", "skills"), agents: []agent{a.copilot}},
	}
	if trust.unknown != "" {
		fs[2].hermesUnknown, fs[2].hermes = trust.unknown, a.hermes
		fs[4].hermesUnknown, fs[4].hermes = trust.unknown, a.hermes
	}
	return fs
}

// Discover lists the skills of home's folders and of each repository root,
// one entry per agent that loads the folder. Each repository gets
// repos.PerRepoTimeout (invariant 6); a repository over it, or a run whose
// ctx ends, goes to coverage and the rest continues. An agent that is not
// present still gets its entries: the graph keeps only those of the agents
// it has.
//
// A skill that is a link counts when it points, in one step, to a skill
// listed in a user skills folder or in the .agents\skills of its own
// repository: the agents of the link's folder get the files of that skill,
// by their canonical paths, so each agent has one resource per file. Any
// other link goes to coverage.
func Discover(ctx context.Context, home string, env agentdirs.Env, repoRoots []string) platform.SkillScan {
	a := resolve(home, env)
	d := &discovery{seen: map[string]bool{}, skills: map[string]*skillFiles{}}
	users := map[string]bool{}
	var user []folder
	if home != "" {
		user = userFolders(home, a)
		for _, f := range user {
			users[pathKey(f.path)] = true
		}
		for _, f := range user {
			f.targets = users
			d.folder(ctx, f)
		}
	}
	var trust hermesTrust
	if len(repoRoots) > 0 {
		trust = readHermesTrust(home, env)
	}
	for _, root := range repoRoots {
		d.repo(ctx, root, a, trust, users)
	}
	d.resolveLinks()
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
func (d *discovery) repo(ctx context.Context, root string, a agents, trust hermesTrust, users map[string]bool) {
	ctx, cancel := context.WithTimeout(ctx, repos.PerRepoTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			d.fail(folder{path: root, agents: []agent{a.claude, a.cursor, a.codex, a.antigravity, a.hermes, a.cline, a.copilot}}, root, graph.ReasonReadError, fmt.Sprintf("panic: %v", r))
		}
	}()
	targets := map[string]bool{pathKey(filepath.Join(root, ".agents", "skills")): true}
	for k := range users {
		targets[k] = true
	}
	for _, f := range repoFolders(root, a, trust) {
		f.targets = targets
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

	// skills are the skill folders listed, by pathKey, with what was found
	// in them; links, the links to skills, resolved once every folder is
	// listed.
	skills map[string]*skillFiles
	links  []link
}

// skillFiles is what one skill folder holds: its SKILL.md, its scripts and
// the entries that could not be listed.
type skillFiles struct {
	instructions, executables []string
	failed                    []failure
}

type failure struct{ path, code, detail string }

// link is a skill that is a link: the folder it sits in and its target, read
// once (empty when it could not be read).
type link struct {
	f      folder
	path   string
	target string
	err    error
}

func (d *discovery) fail(f folder, path, code, detail string) {
	slog.Warn("skills: not listed", "path", path, "reason", code, "detail", detail)
	d.failFor(f.agents, path, graph.Reason(code, detail))
}

func (d *discovery) failFor(agents []agent, path, reason string) {
	for _, a := range agents {
		if key := "fail\x00" + a.product + "\x00" + a.configDir + "\x00" + path; !d.seen[key] {
			d.seen[key] = true
			d.scan.Failed = append(d.scan.Failed, platform.InstructionFailure{
				Product: a.product, ConfigDir: a.configDir, Path: path, Reason: reason,
			})
		}
	}
}

func (d *discovery) add(list *[]platform.InstructionFile, kind string, agents []agent, path string) {
	for _, a := range agents {
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

// notFollowed sends a link to coverage: it is not followed.
func (d *discovery) notFollowed(f folder, path string) {
	d.fail(f, path, graph.ReasonSourceUnsupported, "symbolic link not followed")
}

// isLink reports a symbolic link or another reparse point, such as a
// junction (ModeIrregular since Go 1.23 with winsymlink=1): neither is
// walked.
func isLink(m fs.FileMode) bool { return m&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// folder lists the skills of one skills folder: its subfolders, but those in
// the forced exclusion list (invariant 5), up to MaxSkillsPerFolder. A skill
// that is a link is kept for resolveLinks.
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
	case isLink(info.Mode()):
		d.notFollowed(f, f.path)
		return
	case !info.IsDir():
		return
	}
	if f.hermesUnknown != "" {
		slog.Warn("skills: project folder not attributed to Hermes", "path", f.path, "reason", f.hermesUnknown)
		d.failFor([]agent{f.hermes}, f.path, f.hermesUnknown)
	}
	if len(f.agents) == 0 {
		return
	}
	entries, err := os.ReadDir(f.path)
	if err != nil {
		d.fail(f, f.path, graph.ReasonReadError, err.Error())
		// ReadDir returns the entries read before the error.
	}
	n := 0
	for _, e := range entries {
		if repos.IsForcedExclusion(e.Name()) || (!e.IsDir() && !isLink(e.Type())) {
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
		if isLink(e.Type()) {
			target, err := os.Readlink(skill)
			d.links = append(d.links, link{f: f, path: skill, target: target, err: err})
			continue
		}
		sf := d.skill(ctx, f, skill)
		d.skills[pathKey(skill)] = sf
		d.emit(f.agents, sf)
	}
}

// emit lists sf's files and failures for agents.
func (d *discovery) emit(agents []agent, sf *skillFiles) {
	for _, p := range sf.instructions {
		d.add(&d.scan.Instructions, "instructions", agents, p)
	}
	for _, p := range sf.executables {
		d.add(&d.scan.Executables, "executable", agents, p)
	}
	for _, x := range sf.failed {
		d.failFor(agents, x.path, graph.Reason(x.code, x.detail))
	}
}

// resolveLinks attributes each link to a skill to the agents of its folder
// (BV-99b, item 1). The target, read once and relative to the link's
// folder, must be a skill this pass listed (a link to a link was not) in a
// folder the link's folder may point into; nothing is read again. Any
// other link goes to coverage.
func (d *discovery) resolveLinks() {
	for _, l := range d.links {
		if l.err != nil {
			d.fail(l.f, l.path, graph.ReasonSourceUnsupported, "symbolic link not followed: "+l.err.Error())
			continue
		}
		target := l.target
		if !filepath.IsAbs(target) && !isWindowsAbs(target) {
			target = filepath.Join(filepath.Dir(l.path), target)
		}
		sf, listed := d.skills[pathKey(target)]
		if !listed || !l.f.targets[pathKey(filepath.Dir(target))] {
			d.notFollowed(l.f, l.path)
			continue
		}
		slog.Info("skills: link to a listed skill", "path", l.path, "target", target)
		d.emit(l.f.agents, sf)
	}
}

// skill lists one skill: its SKILL.md and the files under scripts/. A folder
// with no SKILL.md is not a skill.
func (d *discovery) skill(ctx context.Context, f folder, dir string) *skillFiles {
	sf := &skillFiles{}
	failed := func(path, code, detail string) {
		slog.Warn("skills: not listed", "path", path, "reason", code, "detail", detail)
		sf.failed = append(sf.failed, failure{path, code, detail})
	}
	md := filepath.Join(dir, "SKILL.md")
	info, err := os.Lstat(md)
	switch {
	case os.IsNotExist(err):
		return sf
	case err != nil:
		failed(md, graph.ReasonReadError, err.Error())
	default:
		sf.listed(md, info, &sf.instructions, failed)
	}
	scripts := filepath.Join(dir, "scripts")
	info, err = os.Lstat(scripts)
	switch {
	case os.IsNotExist(err):
		return sf
	case err != nil:
		failed(scripts, graph.ReasonReadError, err.Error())
		return sf
	case isLink(info.Mode()):
		failed(scripts, graph.ReasonSourceUnsupported, "symbolic link not followed")
		return sf
	case !info.IsDir():
		return sf
	}
	n := 0
	err = filepath.WalkDir(scripts, func(path string, e fs.DirEntry, err error) error {
		if d.expired(ctx, f, scripts) {
			return fs.SkipAll
		}
		if err != nil {
			failed(path, graph.ReasonReadError, err.Error())
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
			failed(scripts, graph.ReasonFileCountLimitExceeded, fmt.Sprintf("more than %d files", MaxScriptFiles))
			return fs.SkipAll
		}
		n++
		info, err := e.Info()
		if err != nil {
			failed(path, graph.ReasonReadError, err.Error())
			return nil
		}
		sf.listed(path, info, &sf.executables, failed)
		return nil
	})
	if err != nil {
		failed(scripts, graph.ReasonReadError, err.Error())
	}
	return sf
}

// listed lists an existing entry: a regular file within the size limit
// (invariant 6), or a link, listed by its own path and not followed (a link
// inside a skill is the agent's file at that path). Folders and other
// entries are not listed.
func (sf *skillFiles) listed(path string, info fs.FileInfo, list *[]string, failed func(path, code, detail string)) {
	switch {
	case isLink(info.Mode()):
	case !info.Mode().IsRegular():
		return
	case info.Size() > repos.MaxFileBytes:
		failed(path, graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), repos.MaxFileBytes))
		return
	}
	*list = append(*list, path)
}
