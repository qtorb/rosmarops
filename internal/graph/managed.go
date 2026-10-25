package graph

import (
	"github.com/qtorb/rosmarops/internal/platform"
)

// managedPolicy is the managed policy Claude Code applies, resolved from its
// managed sources as documented in
// https://code.claude.com/docs/en/managed-settings#how-claude-code-combines-managed-sources
// and https://code.claude.com/docs/en/settings-reference#managedsourcesbehavior.
type managedPolicy struct {
	// docs are the documents the policy takes keys from, highest priority
	// first and, within the managed settings files, in merge order. They are
	// copies: a single-valued key is kept only in the document whose value
	// applies, so a capability is emitted only from there. Lists and locks
	// are kept in every document: lists combine, and a lock's only strict
	// value is the same everywhere (dedupe keeps its first occurrence).
	docs []platform.ClaudeSettingsFile

	// env are the env variables that apply, each from the document that
	// supplies it: env merges per variable across every admin source, the
	// highest-priority source defining a variable wins ("Keys read from
	// every admin source"). Under an HKCU-only policy, HKCU's env.
	env []managedEnv

	// rulesOnly is allowManagedPermissionRulesOnly in force.
	rulesOnly bool
}

// managedEnv is one env variable of the managed policy and its document.
type managedEnv struct {
	configPath string
	configDir  string
	platform.EnvVar
}

// readDocs returns the documents of a source that were read.
func readDocs(src platform.ManagedSource) []platform.ClaudeSettingsFile {
	var out []platform.ClaudeSettingsFile
	for _, f := range src.Files {
		if f.Status == platform.MCPSourceRead {
			out = append(out, f)
		}
	}
	return out
}

func hasPolicy(src platform.ManagedSource) bool {
	for _, f := range readDocs(src) {
		if f.HasPolicyKey {
			return true
		}
	}
	return false
}

// present reports an admin document that counts as present: one that sets a
// policy key, or an HKLM value, managed settings file or managed-settings.d
// that exists but cannot be read ("When an admin document counts as present").
func present(src platform.ManagedSource) bool {
	if hasPolicy(src) {
		return true
	}
	if src.Kind == platform.ManagedRemote {
		return false
	}
	for _, f := range src.Files {
		if f.Status == platform.MCPSourceFailed {
			return true
		}
	}
	return false
}

// resolveManaged applies the documented rules:
//   - sources rank remote, HKLM, managed settings files, HKCU; the first
//     three are admin sources;
//   - managedSourcesBehavior is read from the highest-priority admin source
//     that carries it or a policy key; default "first-wins";
//   - "first-wins": the highest-ranked admin source with a policy key is the
//     policy; "merge": every admin source with a policy key contributes;
//   - HKCU applies only when no admin document is present, and never merges;
//   - within the managed settings files, later files replace single values
//     of earlier ones, and lists combine;
//   - under "merge", lists combine, locks take the strictest value,
//     permissions.defaultMode and apiKeyHelper come from the highest-ranked
//     source only, and every other key from the highest-ranked source that
//     sets it.
func resolveManaged(sources []platform.ManagedSource) managedPolicy {
	var admin []platform.ManagedSource
	var hkcu *platform.ManagedSource
	for i := range sources {
		if sources[i].Kind == platform.ManagedHKCU {
			hkcu = &sources[i]
			continue
		}
		admin = append(admin, sources[i])
	}

	merge := false
	for _, src := range admin {
		behavior, carries := "", hasPolicy(src)
		for _, f := range readDocs(src) {
			if f.ManagedSourcesBehavior != "" {
				behavior, carries = f.ManagedSourcesBehavior, true
			}
		}
		if carries {
			merge = behavior == "merge"
			break
		}
	}

	var chosen []platform.ManagedSource
	for _, src := range admin {
		if hasPolicy(src) {
			chosen = append(chosen, src)
			if !merge {
				break
			}
		}
	}
	hkcuOnly := false
	if len(chosen) == 0 && hkcu != nil && hasPolicy(*hkcu) {
		anyPresent := false
		for _, src := range admin {
			anyPresent = anyPresent || present(src)
		}
		if !anyPresent {
			chosen, hkcuOnly = []platform.ManagedSource{*hkcu}, true
		}
	}

	var p managedPolicy
	var groups [][]int // indices into p.docs, one group per chosen source
	for _, src := range chosen {
		var g []int
		for _, f := range readDocs(src) {
			f.Scope = platform.MCPScopeManaged
			g = append(g, len(p.docs))
			p.docs = append(p.docs, f)
		}
		groups = append(groups, g)
	}

	// Single-valued keys: keep each only in the document whose value applies.
	type single struct {
		highestOnly bool
		has         func(platform.ClaudeSettingsFile) bool
		clear       func(*platform.ClaudeSettingsFile)
	}
	singles := []single{
		{
			true, func(f platform.ClaudeSettingsFile) bool { return f.Permissions.DefaultMode != "" },
			func(f *platform.ClaudeSettingsFile) { f.Permissions.DefaultMode = "" },
		},
		{
			true, func(f platform.ClaudeSettingsFile) bool { return hasPointer(f.Executables, apiKeyHelperPointer) },
			func(f *platform.ClaudeSettingsFile) {
				f.Executables = withoutPointer(f.Executables, apiKeyHelperPointer)
			},
		},
		{
			false, func(f platform.ClaudeSettingsFile) bool { return hasPointer(f.Executables, statusLinePointer) },
			func(f *platform.ClaudeSettingsFile) { f.Executables = withoutPointer(f.Executables, statusLinePointer) },
		},
		{
			false, func(f platform.ClaudeSettingsFile) bool { return f.EnableAllProjectMcpServers != nil },
			func(f *platform.ClaudeSettingsFile) { f.EnableAllProjectMcpServers = nil },
		},
	}
	for _, s := range singles {
		winner := -1
		for gi, g := range groups {
			if s.highestOnly && gi > 0 {
				break
			}
			for _, i := range g {
				if s.has(p.docs[i]) {
					winner = i // a later file replaces an earlier one
				}
			}
			if winner >= 0 {
				break
			}
		}
		for i := range p.docs {
			if i != winner {
				s.clear(&p.docs[i])
			}
		}
	}

	// env: per variable across the admin sources (or HKCU alone).
	envSources := admin
	if hkcuOnly {
		envSources = []platform.ManagedSource{*hkcu}
	}
	taken := map[string]bool{}
	for _, src := range envSources {
		winners := map[string]managedEnv{}
		for _, f := range readDocs(src) {
			for _, ev := range f.Env {
				if !taken[ev.Key] {
					winners[ev.Key] = managedEnv{f.ConfigPath, f.ConfigDir, ev} // later file replaces
				}
			}
		}
		for k, w := range winners {
			taken[k] = true
			p.env = append(p.env, w)
		}
	}
	for i := range p.docs {
		p.docs[i].Env = nil // env is taken from p.env only
		p.rulesOnly = p.rulesOnly || p.docs[i].AllowManagedPermissionRulesOnly
	}
	return p
}

const (
	apiKeyHelperPointer = "/apiKeyHelper"
	statusLinePointer   = "/statusLine/command"
)

func hasPointer(ps []string, p string) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

func withoutPointer(ps []string, p string) []string {
	var out []string
	for _, x := range ps {
		if x != p {
			out = append(out, x)
		}
	}
	return out
}
