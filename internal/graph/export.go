package graph

import (
	"sort"
	"strings"
)

// Accessors for the rules layer (internal/rules), which reads the built graph
// with the same semantics the inference used.

// EffectiveMode returns the effective permission mode of a Claude Code client
// and the permission_mode capability it comes from (nil for "default" with
// none), from its capabilities in force, as Infer computes it.
func EffectiveMode(inForce []Node) (string, *Node) {
	sorted := append([]Node{}, inForce...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	mode, cap := effectiveMode(sorted)
	if cap == nil {
		return mode, nil
	}
	c := *cap
	return mode, &c
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
