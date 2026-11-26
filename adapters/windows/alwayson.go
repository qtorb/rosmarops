package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// ListAlwaysOn implements platform.Platform (BV-251, CTO-62 §1, §4).
// claude is what ListClaudeSettings read: its auto memory keys, in every
// scope, say where Claude Code's memory is and whether it is on.
func (w *WindowsPlatform) ListAlwaysOn(_ context.Context, repoRoots []string, claude platform.ClaudeSettingsScan) (platform.AlwaysOnScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.AlwaysOnScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return readAlwaysOn(w.layout, home, claudeConfigDir(home), repoRoots, nil, claude), nil
}

// ReadAlwaysOn is ListAlwaysOn on layout l with the Claude Code configuration
// folder claudeDir and the settings claude. env is nil outside tests.
func ReadAlwaysOn(l agentdirs.Layout, home, claudeDir string, repoRoots []string, env agentdirs.Env, claude platform.ClaudeSettingsScan) platform.AlwaysOnScan {
	return readAlwaysOn(l, home, claudeDir, repoRoots, env, claude)
}

func readAlwaysOn(l agentdirs.Layout, home, claudeDir string, repoRoots []string, env agentdirs.Env, claude platform.ClaudeSettingsScan) platform.AlwaysOnScan {
	var scan platform.AlwaysOnScan
	claudeAlwaysOn(&scan, home, claudeDir, repoRoots, claude)
	clineAlwaysOn(&scan, home, agentdirs.ClineDataDir(home, env), repoRoots)
	hermes := l.HermesHome(home, env)
	dirs := []string{hermes}
	if entries, err := os.ReadDir(filepath.Join(hermes, "profiles")); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				dirs = append(dirs, filepath.Join(hermes, "profiles", e.Name()))
			}
		}
	}
	for _, dir := range dirs {
		hermesAlwaysOn(&scan, dir)
	}
	codex := agentdirs.CodexHome(home, env)
	for _, d := range []string{"memories_v2", "memories"} {
		addMemory(&scan, knowledge.CodexProduct, codex, filepath.Join(codex, d), false)
	}
	vscode := l.VSCodeUserDir(home, env)
	for _, root := range repoRoots {
		addMemory(&scan, knowledge.GitHubCopilotProduct, vscode, filepath.Join(root, ".github", "copilot-memories.jsonl"), true)
	}
	return scan
}

// claudeAlwaysOn reads Claude Code's channels and scheduled tasks
// (CTO-53 §1.2, §2, §3; CTO-62 §1) and its memory folders (CTO-62 §4).
// A channel runs only when Claude Code starts with --channels: at rest it is
// launch_flag_required. Its .env is never opened; of access.json only
// dmPolicy and how many entries allowFrom has are kept.
func claudeAlwaysOn(scan *platform.AlwaysOnScan, home, dir string, repoRoots []string, claude platform.ClaudeSettingsScan) {
	product := knowledge.ClaudeCodeProduct
	for _, name := range subdirs(filepath.Join(dir, "channels")) {
		chDir := filepath.Join(dir, "channels", name)
		ch := platform.InboundChannel{
			Product: product, ConfigDir: dir, Path: chDir, Platform: platformName(name),
			Status: platform.ChannelLaunchFlagRequired, Origin: platform.ChannelOriginConfig,
			LaunchFlag: "--channels", Approvers: platform.ApproversUnknown,
		}
		claudeAccess(scan, &ch, filepath.Join(chDir, "access.json"))
		scan.Channels = append(scan.Channels, ch)
	}
	// Scheduled tasks: <repo>/.claude/scheduled_tasks.json and the desktop
	// app's ~/.claude/scheduled-tasks/<name>/ (one folder per task; their
	// SKILL.md is never opened).
	for _, root := range repoRoots {
		path := filepath.Join(root, ".claude", "scheduled_tasks.json")
		if n, ok := countTasks(scan, path); ok {
			scan.Schedules = append(scan.Schedules, platform.AgentSchedule{Product: product, ConfigDir: dir, Path: path, Tasks: &n, Active: intPtr(n)})
		}
	}
	if tasks := subdirs(filepath.Join(dir, "scheduled-tasks")); len(tasks) > 0 {
		n := len(tasks)
		scan.Schedules = append(scan.Schedules, platform.AgentSchedule{Product: product, ConfigDir: dir, Path: filepath.Join(dir, "scheduled-tasks"), Tasks: &n, Active: intPtr(n)})
	}
	claudeMemory(scan, home, dir, repoRoots, claude)
}

// claudeMemory adds Claude Code's auto memory folders
// (code.claude.com/docs/en/memory): <config>/projects/<project>/memory, and
// the folder autoMemoryDirectory names in any settings scope read (user,
// project, local, managed). Each is marked AutoMemoryOff when
// autoMemoryEnabled is false in the settings that apply to it.
func claudeMemory(scan *platform.AlwaysOnScan, home, dir string, repoRoots []string, claude platform.ClaudeSettingsScan) {
	product := knowledge.ClaudeCodeProduct
	// enabled is autoMemoryEnabled for a project root: the managed settings
	// first, then the project's local and project settings, then the user's
	// (code.claude.com/docs/en/settings, settings precedence). An empty root
	// is a memory of no known project: only the managed and user settings
	// apply, and a project that turns it on keeps it on.
	var managed, user *bool
	byRoot := map[string]*bool{}
	anyOn := false
	for _, src := range claude.Managed {
		for _, f := range src.Files {
			if managed == nil && f.Status == platform.MCPSourceRead && f.AutoMemoryEnabled != nil {
				managed = f.AutoMemoryEnabled
			}
		}
	}
	// Local is read before project for the same root: it takes precedence.
	files := append([]platform.ClaudeSettingsFile{}, claude.Files...)
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Scope == platform.MCPScopeLocal && files[j].Scope == platform.MCPScopeProject
	})
	for _, f := range files {
		if f.Status != platform.MCPSourceRead || f.AutoMemoryEnabled == nil {
			continue
		}
		switch f.Scope {
		case platform.MCPScopeUser:
			user = f.AutoMemoryEnabled
		case platform.MCPScopeProject, platform.MCPScopeLocal:
			if f.HomeLocalOnly {
				continue
			}
			if _, set := byRoot[f.ScopePath]; !set {
				byRoot[f.ScopePath] = f.AutoMemoryEnabled
			}
			anyOn = anyOn || *f.AutoMemoryEnabled
		}
	}
	off := func(root string) bool {
		switch {
		case managed != nil:
			return !*managed
		case root != "" && byRoot[root] != nil:
			return !*byRoot[root]
		case user != nil:
			return !*user && (root != "" || !anyOn)
		}
		return false
	}
	add := func(path, root string) {
		for _, m := range scan.Memory {
			if m.Product == product && sameDir(m.Path, path) {
				return
			}
		}
		n := len(scan.Memory)
		addMemory(scan, product, dir, path, false)
		if len(scan.Memory) > n {
			scan.Memory[n].AutoMemoryOff = off(root)
		}
	}
	// <config>/projects/<project>/memory: the folder name is the project's
	// path with every character that is not a letter or a digit turned into
	// "-" (e.g. /home/user becomes -home-user).
	roots := map[string]string{}
	for _, root := range repoRoots {
		roots[claudeProjectDirName(root)] = root
	}
	for _, p := range subdirs(filepath.Join(dir, "projects")) {
		add(filepath.Join(dir, "projects", p, "memory"), roots[p])
	}
	// autoMemoryDirectory moves the memory. Only that key is kept.
	moved := func(f platform.ClaudeSettingsFile, root string) {
		p := f.AutoMemoryDirectory
		if f.Status != platform.MCPSourceRead || p == "" {
			return
		}
		if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
			p = filepath.Join(home, p[2:])
		}
		if filepath.IsAbs(p) {
			add(p, root)
		}
	}
	for _, f := range claude.Files {
		root := ""
		if (f.Scope == platform.MCPScopeProject || f.Scope == platform.MCPScopeLocal) && !f.HomeLocalOnly {
			root = f.ScopePath
		}
		moved(f, root)
	}
	for _, src := range claude.Managed {
		for _, f := range src.Files {
			moved(f, "")
		}
	}
}

// claudeProjectDirName is the name Claude Code gives a project's folder under
// <config>/projects: its path with every character that is not an ASCII
// letter or digit replaced by "-".
func claudeProjectDirName(root string) string { return ClaudeProjectDirName(root) }

// ClaudeProjectDirName is claudeProjectDirName, for tests.
func ClaudeProjectDirName(root string) string {
	b := []byte(root)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// claudeAccess reads a channel's access.json (external_plugins/telegram/
// skills/access/SKILL.md:41-60): dmPolicy, and how many allowFrom entries —
// never who, never pending. Without the file the policy is pairing with an
// empty list.
func claudeAccess(scan *platform.AlwaysOnScan, ch *platform.InboundChannel, path string) {
	raw, status, reason := readBounded(path)
	switch status {
	case platform.MCPSourceMissing:
		ch.Approvers, ch.AllowlistCount = platform.ApproversListed, intPtr(0)
		return
	case platform.MCPSourceFailed:
		scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: reason})
		return
	}
	var access struct {
		DMPolicy  string            `json:"dmPolicy"`
		AllowFrom []json.RawMessage `json:"allowFrom"`
	}
	if err := json.Unmarshal(raw, &access); err != nil {
		scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: "parse_error"})
		return
	}
	switch strings.ToLower(strings.TrimSpace(access.DMPolicy)) {
	case "", "pairing", "allowlist":
		ch.Approvers, ch.AllowlistCount = platform.ApproversListed, intPtr(len(access.AllowFrom))
	case "disabled":
		ch.Approvers = platform.ApproversHostOnly
	default:
		ch.Approvers = platform.ApproversUnknown
	}
}

// clineAlwaysOn reads Cline's connectors and schedules (CTO-53 §1.3, §2;
// CTO-62 §1): a connector's state file <data>/connectors/<platform>/<bot>.json
// by its name only (never opened: Slack's options can hold tokens), and
// whether ~/.cline/schedules exists (schedules.db is not read, signed). Who may
// give orders (--allowed-user-id) is a launch argument: not seen at rest.
func clineAlwaysOn(scan *platform.AlwaysOnScan, home, data string, repoRoots []string) {
	product := knowledge.ClineProduct
	for _, name := range subdirs(filepath.Join(data, "connectors")) {
		pdir := filepath.Join(data, "connectors", name)
		bots := 0
		entries, _ := os.ReadDir(pdir)
		for _, e := range entries {
			if e.Type().IsRegular() && strings.EqualFold(filepath.Ext(e.Name()), ".json") {
				bots++
			}
		}
		if bots == 0 && !strings.EqualFold(name, "slack") {
			continue
		}
		scan.Channels = append(scan.Channels, platform.InboundChannel{
			Product: product, ConfigDir: data, Path: pdir, Platform: platformName(name),
			Status: platform.ChannelConfigured, Origin: platform.ChannelOriginConfig, Approvers: platform.ApproversUnknown,
		})
	}
	if p := filepath.Join(home, ".cline", "schedules"); pathExists(p) {
		scan.Schedules = append(scan.Schedules, platform.AgentSchedule{Product: product, ConfigDir: data, Path: p})
	}
	for _, root := range repoRoots {
		addMemory(scan, product, data, filepath.Join(root, "memory-bank"), true)
	}
}

// hermesJob is the only part of a cron job RosmarOps keeps (cron/jobs.py:
// enabled :540, schedule.kind :576, deliver): never the prompt or the script.
type hermesJob struct {
	Enabled  *bool `json:"enabled"`
	Schedule *struct {
		Kind string `json:"kind"`
	} `json:"schedule"`
	Deliver json.RawMessage `json:"deliver"`
}

// hermesAlwaysOn reads one Hermes folder's cron/jobs.json (CTO-53 §2) and its
// memories folder (CTO-62 §4). Its gateway platforms and approvers come with
// ListHermes.
func hermesAlwaysOn(scan *platform.AlwaysOnScan, dir string) {
	product := knowledge.HermesProduct
	path := filepath.Join(dir, "cron", "jobs.json")
	raw, status, reason := readBounded(path)
	switch status {
	case platform.MCPSourceFailed:
		scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: reason})
	case platform.MCPSourceRead:
		jobs, ok := decodeJobs(raw)
		if !ok {
			scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: "parse_error"})
			break
		}
		total, active := len(jobs), 0
		dest := map[string]bool{}
		for _, j := range jobs {
			if j.Enabled == nil || *j.Enabled {
				active++
			}
			if d := deliverPlatform(j.Deliver); d != "" {
				dest[d] = true
			}
		}
		scan.Schedules = append(scan.Schedules, platform.AgentSchedule{
			Product: product, ConfigDir: dir, Path: path, Tasks: &total, Active: &active, Destinations: sortedSet(dest),
		})
	}
	addMemory(scan, product, dir, filepath.Join(dir, "memories"), false)
}

// decodeJobs reads jobs.json as a list of jobs or an object with a "jobs"
// list.
func decodeJobs(raw []byte) ([]hermesJob, bool) {
	var list []hermesJob
	if json.Unmarshal(raw, &list) == nil {
		return list, true
	}
	var obj struct {
		Jobs []hermesJob `json:"jobs"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Jobs != nil {
		return obj.Jobs, true
	}
	return nil, false
}

// deliverPlatform is the platform part of a cron job's deliver value
// ("telegram", "telegram:<chat>", "local"…): its name only, never the chat.
func deliverPlatform(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimSpace(s), ":")
	return platformName(name)
}

// countTasks counts the entries of a scheduled tasks file (a list, or an
// object with a "tasks" list); ok false when it does not exist or is not one
// of these.
func countTasks(scan *platform.AlwaysOnScan, path string) (int, bool) {
	raw, status, reason := readBounded(path)
	switch status {
	case platform.MCPSourceMissing:
		return 0, false
	case platform.MCPSourceFailed:
		scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: reason})
		return 0, false
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		return len(list), true
	}
	var obj struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Tasks != nil {
		return len(obj.Tasks), true
	}
	scan.Failed = append(scan.Failed, platform.SourceFailure{Path: path, Reason: "parse_error"})
	return 0, false
}

// Memory limits: how deep and how many entries the metadata walk goes.
const (
	memoryMaxDepth   = 6
	memoryMaxEntries = 10000
)

// addMemory adds the memory at path when it exists, from its metadata only
// (BV-251 decision 2): a file, or the regular files under a folder. Nothing
// is opened.
func addMemory(scan *platform.AlwaysOnScan, product, configDir, path string, inRepo bool) {
	files, size, ok := MemoryStat(path)
	if !ok {
		return
	}
	scan.Memory = append(scan.Memory, platform.AgentMemory{
		Product: product, ConfigDir: configDir, Path: path, Files: files, Bytes: size, InRepository: inRepo,
	})
}

// MemoryStat is how many regular files path holds and their total size, from
// their metadata only (os.Lstat and directory listings; no file is opened).
// A symbolic link is not followed. ok false when path does not exist.
func MemoryStat(path string) (files int, size int64, ok bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, 0, false
	}
	if info.Mode().IsRegular() {
		return 1, info.Size(), true
	}
	if !info.IsDir() {
		return 0, 0, false
	}
	files, size = MemoryWalk(os.DirFS(path))
	return files, size, true
}

// MemoryWalk counts the regular files of fsys and their sizes with
// fs.ReadDir and DirEntry.Info only: no file is opened (tested with a file
// system that fails on any file open).
func MemoryWalk(fsys fs.FS) (files int, size int64) {
	entries := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		list, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return
		}
		for _, e := range list {
			entries++
			if entries > memoryMaxEntries {
				return
			}
			switch {
			case e.Type().IsRegular():
				if info, err := e.Info(); err == nil {
					files++
					size += info.Size()
				}
			case e.IsDir() && depth < memoryMaxDepth:
				walk(pathJoin(dir, e.Name()), depth+1)
			}
		}
	}
	walk(".", 0)
	return files, size
}

func pathJoin(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

// subdirs are the folder's subfolders, sorted, without symbolic links.
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("always-on: folder not listed", "path", dir, "err", err)
		}
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// platformName keeps a platform name as a short code: lowercase letters,
// digits, «_» and «-»; anything else makes it "other".
func platformName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 32 {
		return ""
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return "other"
		}
	}
	return s
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func intPtr(n int) *int { return &n }

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
