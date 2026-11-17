package graph

import (
	"sort"
	"strings"
)

// Accessors for the rules layer (internal/rules), which reads the built graph
// with the same semantics the inference used.

// EffectiveMode returns the effective permission mode of a Claude Code client
// and the permission_mode capability it comes from (nil for "default" with
// none), from its capabilities in force: the most permissive mode of its
// sessions, one per project in repoRoots (BV-232 §4), as Infer computes them.
func EffectiveMode(inForce []Node, repoRoots []string) (string, *Node) {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	mode, cap := worstMode(claudeSessions(sorted, repoRoots, ""))
	if cap == nil {
		return mode, nil
	}
	c := *cap
	return mode, &c
}

// ShellNetworkSandboxed reports that, in every session of a Claude Code client
// (one per project in repoRoots), an effective sandbox contains the network of
// its shell commands with no allowed domain that opens any host (BV-232 §1): a
// command then reaches no host outside the list without a person, so a shell
// allow is no egress there.
func ShellNetworkSandboxed(inForce []Node, repoRoots []string) bool {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, s := range claudeSessions(sorted, repoRoots, "") {
		// bypassPermissions lets a sandboxed command reach a host outside the
		// list without a prompt (sandboxing, hosts outside your allowed domains).
		if !s.sandboxEffective() || s.broadNetwork() || s.mode == permissionModeBypass {
			return false
		}
	}
	return true
}

// GrokReadsOnlyProjects reports a Grok Build client whose [sandbox] profile in
// force limits reads to the working directory (strict, BV-249 §3.2).
func GrokReadsOnlyProjects(caps []Node) bool {
	return grokSandboxProfile(caps) == grokProfileStrict
}

// ShellSandbox reports how Claude Code's shell commands run in every session
// (BV-249 §3.1): contained, inside an effective sandbox in each one; and
// autoAllow, without asking there (sandbox.autoAllowBashIfSandboxed, true
// when absent) in each one.
func ShellSandbox(inForce []Node, repoRoots []string) (contained, autoAllow bool) {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	sessions := claudeSessions(sorted, repoRoots, "")
	contained, autoAllow = len(sessions) > 0, len(sessions) > 0
	for _, s := range sessions {
		contained = contained && s.sandboxEffective()
		autoAllow = autoAllow && s.sandboxRunsAll()
	}
	return contained, autoAllow
}

// SandboxAutoAllowUnwritten reports a Claude Code session whose sandbox is
// effective and whose layers do not write sandbox.autoAllowBashIfSandboxed
// (CTO-64 §2): the engine assumes true, the default before 2.1.325.
func SandboxAutoAllowUnwritten(inForce []Node, repoRoots []string) bool {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, s := range claudeSessions(sorted, repoRoots, "") {
		if s.sandboxEffective() && !s.sb.autoAllowSet {
			return true
		}
	}
	return false
}

// ClosesWindowsDrive reports that every Claude Code session keeps its reads
// out of /mnt, where WSL mounts the Windows drives (BV-252, CTO-63 §4.3): an
// effective sandbox with blockReadsOutsideWorkingDirectories in force, or an
// effective sandbox whose denyRead covers /mnt ("/mnt/"). Any other agent's
// capabilities carry neither, so it does not close it.
func ClosesWindowsDrive(inForce []Node, repoRoots []string, home string) bool {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	sessions := claudeSessions(sorted, repoRoots, home)
	for _, s := range sessions {
		if !s.sandboxEffective() || !(s.blocksReadsOutside(sorted) || s.sandboxDenies("/mnt", home)) {
			return false
		}
	}
	return len(sessions) > 0
}

// CredentialFiles returns the distinct files of a credential's locations.
func CredentialFiles(cred Node) []string { return credentialFiles(cred) }

// FileOfResource returns the file of a resource path, and false for a path
// with no file (a registry value).
func FileOfResource(p string) (string, bool) { return resourceFile(p) }

// Within reports whether file is root or under it, comparing paths as Claude
// Code does (Windows paths case-insensitively, in POSIX form).
func Within(root, file string) bool { return within(root, file) }

// CanonicalPath is the comparison form of a path: forward slashes, and
// Windows paths lowercased.
func CanonicalPath(p string) string { return canonicalPath(p) }

// KeyHash returns the first 16 hex characters of the SHA-256 of the
// NUL-joined key parts, the hash of every graph id.
func KeyHash(parts ...string) string { return keyHash(parts...) }

// ClientDisplayName is the name shown in reports for a client product.
func ClientDisplayName(product string) string { return clientDisplayName(product) }

// MatchRelGlob reports whether a path relative to a repository root matches
// a gitignore-style pattern ("**" for any number of folders), both with
// forward slashes. A malformed pattern matches nothing.
func MatchRelGlob(pattern, rel string) bool {
	pat := strings.Split(strings.Trim(pattern, "/"), "/")
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	m, ok := globSegments(pat, segs)
	return ok && m
}
