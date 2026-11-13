package main

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/winpkg"
)

// ownBinaryCheck is CTO-44 §4d's condition 3 for this run: a command's program
// path leads to RosmarOps's own binary when the executable it names has the
// SHA-256 ref (the state's current reference). A bare «rosmarops» is looked up
// in the person's PATH, as Claude Code would. The Store's execution alias
// counts only when this process runs from our own package (winpkg.Family)
// and its real executable, inside the package, has ref. Anything else, such
// as another program named rosmarops or an older copy, is not ours.
func ownBinaryCheck(ref, localAppData string) func(string) bool {
	return func(p string) bool {
		if ref == "" {
			return false
		}
		if base := strings.ToLower(p); base == "rosmarops" || base == "rosmarops.exe" {
			lp, err := exec.LookPath(p)
			if err != nil {
				return false
			}
			p = lp
		}
		if !filepath.IsAbs(p) {
			return false
		}
		if localAppData != "" && insideDir(p, filepath.Join(localAppData, "Microsoft", "WindowsApps")) {
			id := currentPackage()
			if id.FamilyName != winpkg.Family || id.Path == "" {
				return false
			}
			real, err := currentExe()
			if err != nil || !insideDir(real, id.Path) {
				return false
			}
			p = real
		}
		h, err := hash.File(p)
		return err == nil && h == ref
	}
}

// setOwnBinary arms the settings reader's recognition of our own hooks for
// this run.
func setOwnBinary(ref, localAppData string) {
	windows.OwnBinary = ownBinaryCheck(ref, localAppData)
}

// agentHooksFor is what the report needs for RosmarOps inside Claude Code
// (BV-226 §4, §5), from this run's Claude Code settings: whether Claude Code
// was found, its user settings file, our hooks it already runs and whether
// the person has a status line of their own; exe is RosmarOps's path as the
// run resolves it (the Store's alias when packaged).
func agentHooksFor(scan *platform.ClaudeSettingsScan, v report.V2, exe string) report.AgentHooks {
	a := report.AgentHooks{Exe: exe}
	claude := graph.ClientDisplayName(knowledge.ClaudeCodeProduct)
	for _, n := range append(append([]string{}, v.Index.Scope.Evaluated...), v.Index.Scope.NotEvaluated...) {
		a.ClaudeCode = a.ClaudeCode || n == claude
	}
	if scan == nil {
		return a
	}
	files := append([]platform.ClaudeSettingsFile{}, scan.Files...)
	for _, m := range scan.Managed {
		files = append(files, m.Files...)
	}
	own := map[string]bool{}
	taken := false
	for _, f := range files {
		if f.Scope != platform.MCPScopeUser && f.Scope != platform.MCPScopeManaged {
			continue
		}
		if f.Scope == platform.MCPScopeUser && a.File == "" {
			a.File = f.ConfigPath
		}
		mine := false
		for _, h := range f.OwnHooks {
			own[h] = true
			mine = mine || h == platform.OwnHookStatusline
		}
		taken = taken || (f.HasStatusLine && !mine)
	}
	a.StatusLineTaken = taken
	for _, h := range []string{platform.OwnHookSessionCheck, platform.OwnHookStatusline} {
		if own[h] {
			a.Installed = append(a.Installed, h)
		}
	}
	return a
}
