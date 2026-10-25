package graph

import (
	"path"
	"strings"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// normPath is the comparison form of a filesystem path: canonicalPath
// (forward slashes; Windows paths lowercased) and cleaned.
func normPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(canonicalPath(p))
}

// isWindowsPath reports a path canonicalPath treats as a Windows path.
func isWindowsPath(p string) bool {
	return strings.Contains(p, `\`) || (len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]))
}

// posixPath is the form Claude Code matches permission patterns against: on
// Windows, "C:\Users\alice" becomes "/c/users/alice"
// (https://code.claude.com/docs/en/permissions#where-a-pattern-anchors).
func posixPath(p string) string {
	n := normPath(p)
	if len(n) >= 2 && n[1] == ':' && isASCIILetter(n[0]) {
		return "/" + strings.ToLower(n[:1]) + n[2:]
	}
	return n
}

// within reports whether file is root or under it.
func within(root, file string) bool {
	r, f := posixPath(root), posixPath(file)
	if r == "" || f == "" {
		return false
	}
	return f == r || r == "/" || strings.HasPrefix(f, r+"/")
}

// relTo returns file relative to anchor, and whether file is under anchor.
func relTo(anchor, file string) (string, bool) {
	a, f := posixPath(anchor), posixPath(file)
	switch {
	case a == "/":
		return strings.TrimPrefix(f, "/"), f != "/"
	case strings.HasPrefix(f, a+"/"):
		return f[len(a)+1:], true
	}
	return "", false
}

// ruleScope is what anchors a Read or Edit pattern: the layer of the
// settings file that defines it (applies_in), Claude Code's configuration
// folder, the home folder and the working roots.
type ruleScope struct {
	appliesIn string
	configDir string
	home      string
	roots     []string
}

// scopeRoot is the repository root of a project or local layer.
func (s ruleScope) scopeRoot() (string, bool) {
	for _, prefix := range []string{"project:", "local:"} {
		if strings.HasPrefix(s.appliesIn, prefix) {
			return s.appliesIn[len(prefix):], true
		}
	}
	return "", false
}

// matchRulePath reports whether the specifier of a Read or Edit rule covers
// file, following https://code.claude.com/docs/en/permissions, "How path
// patterns match", "Where a pattern anchors" and "How deep a pattern
// matches". The patterns use gitignore syntax:
//
//   - "//path": absolute from the filesystem root;
//   - "~/path": from the home folder;
//   - "/path": from the settings source: the repository root for project and
//     local settings (the session's primary working directory, taken to be
//     the repository root), the configuration folder for user settings. The
//     anchor of managed settings is not documented: such a rule matches
//     nothing here;
//   - "path" or "./path": from the current directory, taken to be the
//     repository root for project and local settings and any working root
//     for user and managed settings, whose sessions can start in any of them.
//
// A pattern without "/" (after a leading "./" or a trailing "/") matches at
// any depth, as in gitignore; any other pattern is anchored. A deny or ask
// rule with one literal directory segment followed by "/**" matches that
// directory at any depth. A pattern matches a path when it matches the path
// or one of its parent folders. "*" and "?" stay within a segment, "**"
// spans segments. An allow pattern that starts with "!" or is not a valid
// pattern approves nothing; a deny pattern that starts with "!" is a
// carve-out, not a deny, and is not matched (carve-outs are not applied).
//
// Four choices where the documentation is silent or ambiguous, accepted by
// the owner (BV-29, D2; reported in docs/status/turno-noche-2026-10-24.md,
// T4, "Ambigüedades de la semántica de patrones"):
//   - a "/path" rule in managed settings covers nothing (its anchor is not in
//     the "Where a pattern anchors" table);
//   - "./path" is anchored at the current directory, not matched at any depth;
//   - relative rules of user and managed settings are tried against every
//     working root, since the current directory is not known statically;
//   - "!" negations are not applied.
func matchRulePath(pattern, approval string, s ruleScope, file string) bool {
	if pattern == scopeAll {
		return true
	}
	if pattern == "" || strings.HasPrefix(pattern, "!") {
		return false
	}
	var anchors []string
	anchored := true
	pat := pattern
	switch {
	case strings.HasPrefix(pat, "//"):
		anchors, pat = []string{"/"}, pat[2:]
	case strings.HasPrefix(pat, "~/"):
		anchors, pat = []string{s.home}, pat[2:]
	case strings.HasPrefix(pat, "/"):
		pat = pat[1:]
		if root, ok := s.scopeRoot(); ok {
			anchors = []string{root}
		} else if s.appliesIn == "user" {
			anchors = []string{s.configDir}
		}
	default:
		explicit := strings.HasPrefix(pat, "./")
		pat = strings.TrimPrefix(pat, "./")
		if root, ok := s.scopeRoot(); ok {
			anchors = []string{root}
		} else {
			anchors = s.roots
		}
		anchored = explicit || strings.Contains(strings.TrimSuffix(pat, "/"), "/")
		if approval != ApprovalAllow && singleSegmentTree(pat) {
			anchored = false
		}
	}
	dirOnly := strings.HasSuffix(pat, "/")
	pat = strings.TrimSuffix(pat, "/")
	if pat == "" {
		return false
	}
	if isWindowsPath(file) {
		pat = strings.ToLower(pat)
	}
	if !anchored {
		pat = "**/" + pat
	}
	patSegs := strings.Split(pat, "/")
	for _, anchor := range anchors {
		rel, ok := relTo(anchor, file)
		if !ok || rel == "" {
			continue
		}
		segs := strings.Split(rel, "/")
		// The path itself (unless the pattern names folders only), then each
		// parent folder.
		for n := len(segs); n >= 1; n-- {
			if dirOnly && n == len(segs) {
				continue
			}
			m, valid := globSegments(patSegs, segs[:n])
			if !valid {
				return approval != ApprovalAllow && pattern == rel
			}
			if m {
				return true
			}
		}
	}
	return false
}

// singleSegmentTree reports a pattern of one literal folder followed by
// "/**", such as "secrets/**".
func singleSegmentTree(pat string) bool {
	dir, ok := strings.CutSuffix(pat, "/**")
	return ok && dir != "" && !strings.ContainsAny(dir, "/*?[")
}

// globSegments matches path segments against gitignore pattern segments: a
// "**" segment matches zero or more segments, except at the end, where it
// matches one or more. valid is false for a malformed segment pattern.
func globSegments(pat, segs []string) (match, valid bool) {
	if len(pat) == 0 {
		return len(segs) == 0, true
	}
	if pat[0] == "**" {
		if len(pat) == 1 {
			return len(segs) >= 1, true
		}
		for i := 0; i <= len(segs); i++ {
			m, ok := globSegments(pat[1:], segs[i:])
			if !ok {
				return false, false
			}
			if m {
				return true, true
			}
		}
		return false, true
	}
	if len(segs) == 0 {
		return false, true
	}
	m, err := path.Match(pat[0], segs[0])
	if err != nil {
		return false, false
	}
	if !m {
		return false, true
	}
	return globSegments(pat[1:], segs[1:])
}

// protectedPaths is the protected path list of the knowledge base
// (permission-modes#protected-paths) resolved for one Claude Code client.
type protectedPaths struct {
	entry     knowledge.Entry
	configDir string
	home      string
}

// configAnchors are the folders "~/.claude/" stands for: the client's
// configuration folder and ~/.claude (claude-directory: CLAUDE_CONFIG_DIR
// moves every ~/.claude path).
func (p protectedPaths) configAnchors() []string {
	out := []string{}
	if p.configDir != "" {
		out = append(out, p.configDir)
	}
	if p.home != "" {
		out = append(out, p.home+"/.claude")
	}
	return out
}

// protected reports whether a write to file is a protected-path write.
//
//   - a name without a trailing "/" is a protected file: it matches the
//     file's name, in any folder;
//   - a name with a trailing "/" is a protected folder: it matches when its
//     segments appear, in order, among the folders that hold the file
//     ("~/.claude/..." is resolved against the configuration folder);
//   - ".claude/" also covers the configuration folder itself;
//   - an exception removes only the protection that ".claude/" gives; a
//     file it covers that another entry protects stays protected.
func (p protectedPaths) protected(file string) bool {
	f := posixPath(file)
	if f == "" {
		return false
	}
	lower := isWindowsPath(file)
	norm := func(s string) string {
		if lower {
			return strings.ToLower(s)
		}
		return s
	}
	segs := strings.Split(strings.TrimPrefix(f, "/"), "/")
	name, dirs := segs[len(segs)-1], segs[:len(segs)-1]

	claudeDir := false
	for _, e := range p.entry.Paths {
		e = norm(e)
		if !strings.HasSuffix(e, "/") {
			if name == e {
				return true
			}
			continue
		}
		if p.underEntry(e, f, dirs) {
			if e != ".claude/" {
				return true
			}
			claudeDir = true
		}
	}
	if !claudeDir {
		for _, a := range p.configAnchors() {
			if within(a, file) && posixPath(a) != f {
				claudeDir = true
			}
		}
	}
	if !claudeDir {
		return false
	}
	for _, x := range p.entry.Exceptions {
		if p.underEntry(norm(x), f, dirs) {
			return false
		}
	}
	return true
}

// underEntry reports whether file f (folders dirs) is under the folder entry
// e: "~/.claude/a/b/" anchored at the configuration folders, otherwise "a/b/"
// as consecutive folders anywhere. "<id>"-style segments match any one
// segment; a final "*.md" requires a Markdown file under the folder.
func (p protectedPaths) underEntry(e, f string, dirs []string) bool {
	if rest, ok := strings.CutSuffix(e, "*.md"); ok {
		e = rest
		if !strings.HasSuffix(strings.ToLower(f), ".md") {
			return false
		}
	}
	e = strings.TrimSuffix(e, "/")
	segMatch := func(pat, seg string) bool {
		return (strings.HasPrefix(pat, "<") && strings.HasSuffix(pat, ">")) || pat == seg
	}
	if rest, ok := strings.CutPrefix(e, "~/.claude/"); ok {
		want := strings.Split(rest, "/")
		for _, a := range p.configAnchors() {
			rel, under := relTo(a, f)
			if !under {
				continue
			}
			got := strings.Split(rel, "/")
			if len(got) <= len(want) {
				continue // the file must lie inside the folder
			}
			all := true
			for i, w := range want {
				all = all && segMatch(w, got[i])
			}
			if all {
				return true
			}
		}
		return false
	}
	want := strings.Split(e, "/")
	for i := 0; i+len(want) <= len(dirs); i++ {
		all := true
		for j, w := range want {
			all = all && segMatch(w, dirs[i+j])
		}
		if all {
			return true
		}
	}
	return false
}
