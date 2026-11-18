// Package docs_test keeps the human documentation in step with the code:
// ARCHITECTURE.md names every package of internal/, the decisions index lists
// every decision record, and the relative links of the documentation point to
// files that exist.
package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot is the repository root, two levels above this file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Every first-level folder of internal/ is named in ARCHITECTURE.md as
// internal/<name>.
func TestArchitectureNamesEveryInternalPackage(t *testing.T) {
	root := repoRoot(t)
	arch := read(t, filepath.Join(root, "ARCHITECTURE.md"))
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	named := regexp.MustCompile("internal/([a-z0-9_]+)")
	seen := map[string]bool{}
	for _, m := range named.FindAllStringSubmatch(arch, -1) {
		seen[m[1]] = true
	}
	dirs := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs++
		if !seen[e.Name()] {
			t.Errorf("ARCHITECTURE.md does not name internal/%s", e.Name())
		}
	}
	if dirs == 0 {
		t.Fatal("no folder found in internal/")
	}
}

// Every file of docs/decisions/ but the index itself is in the index.
func TestDecisionsIndexListsEveryRecord(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "docs", "decisions")
	index := read(t, filepath.Join(dir, "README.md"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	records := 0
	for _, e := range entries {
		if e.IsDir() || e.Name() == "README.md" {
			continue
		}
		records++
		if !strings.Contains(index, "("+e.Name()+")") {
			t.Errorf("docs/decisions/README.md does not link %s", e.Name())
		}
	}
	if records == 0 {
		t.Fatal("no decision record in docs/decisions/")
	}
}

// mdLink is an inline Markdown link or image: [text](target) or
// [text](target "title").
var mdLink = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// relativeLinks are the relative link targets of a Markdown text, without
// their #fragment, outside fenced code blocks. Links with a scheme (https:,
// mailto:) and in-page anchors are not relative.
func relativeLinks(md string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if i := strings.Index(target, "#"); i >= 0 {
				target = target[:i]
			}
			if target != "" {
				out = append(out, target)
			}
		}
	}
	return out
}

func TestRelativeLinksSkipsWhatIsNotARelativeLink(t *testing.T) {
	md := "[a](b.md) [c](https://x.y/z) [d](#top) [e](f/g.md#h) ![i](j.png \"title\")\n```\n[k](missing.md)\n```\n"
	got := strings.Join(relativeLinks(md), " ")
	if want := "b.md f/g.md j.png"; got != want {
		t.Errorf("relativeLinks = %q, want %q", got, want)
	}
}

// Every relative link of ARCHITECTURE.md, CONTRIBUTING.md, docs/how-to/ and
// docs/decisions/ points to a file or folder that exists.
func TestDocumentationLinksExist(t *testing.T) {
	root := repoRoot(t)
	files := []string{filepath.Join(root, "ARCHITECTURE.md"), filepath.Join(root, "CONTRIBUTING.md")}
	for _, dir := range []string{"how-to", "decisions"} {
		matches, err := filepath.Glob(filepath.Join(root, "docs", dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Errorf("no Markdown file in docs/%s/", dir)
		}
		files = append(files, matches...)
	}
	for _, f := range files {
		for _, target := range relativeLinks(read(t, f)) {
			path := filepath.Join(filepath.Dir(f), filepath.FromSlash(target))
			if _, err := os.Stat(path); err != nil {
				rel, _ := filepath.Rel(root, f)
				t.Errorf("%s: broken link %s", filepath.ToSlash(rel), target)
			}
		}
	}
}
