// Package kitfind looks for a RosmarOps kit file in the user's Downloads
// folder, so a person can bring the key to a new computer without a terminal
// (CTO-12 H3). It only reads: it never writes to Downloads and never deletes
// anything. Only the file name is ever logged, never its contents.
package kitfind

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/kit"
)

// MaxSize is the largest kit file read (CTO-12 H3): a kit file is tiny, so a
// larger file is not one.
const MaxSize = 64 * 1024

// maxCandidates bounds how many files are read, newest first, so a Downloads
// folder full of matching names cannot make the scan do unbounded work.
const maxCandidates = 20

// nameRe is the kit file name, with the browser's « (1)» duplicate suffix
// allowed. The 8 hex characters are the start of the kit_id and are checked
// against the file's own kit_id.
var nameRe = regexp.MustCompile(`^rosmarops-kit-([0-9a-f]{8})( \([0-9]{1,3}\))?\.json$`)

// Found is the kit file to offer: its path, the kit_id inside it, and whether
// the window should show the file's date because several different kits were
// found.
type Found struct {
	Path     string
	KitID    string
	ModTime  time.Time
	ShowDate bool
}

// Scan looks in dir (no subfolders) for kit files and returns the one to
// offer, or nil when there is none. The rules (CTO-12 H3): the name must
// match nameRe; only a normal file (no link, reparse point or cloud
// placeholder); at most MaxSize bytes, read once; it must parse as a kit and
// its name's 8 hex must be its kit_id's first 8; of the valid ones the newest
// is offered, and the date is shown when they are not all the same kit.
func Scan(dir string) *Found {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type cand struct {
		path, hex string
		mod       time.Time
	}
	var cands []cand
	for _, e := range entries {
		m := nameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || special(info) {
			continue // a link, a dir, a device, a reparse point or a cloud placeholder
		}
		cands = append(cands, cand{path: p, hex: m[1], mod: info.ModTime()})
	}
	// Newest first, and read at most maxCandidates of them.
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod.After(cands[j].mod) })
	if len(cands) > maxCandidates {
		cands = cands[:maxCandidates]
	}

	var valid []Found
	kinds := map[string]bool{}
	for _, c := range cands {
		info, err := os.Lstat(c.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxSize {
			continue
		}
		raw, err := os.ReadFile(c.path)
		if err != nil || int64(len(raw)) > MaxSize {
			continue
		}
		f, err := kit.Parse(raw)
		if err != nil || len(f.KitID) < 8 || !strings.HasPrefix(f.KitID, c.hex) {
			continue
		}
		valid = append(valid, Found{Path: c.path, KitID: f.KitID, ModTime: c.mod})
		kinds[f.KitID] = true
	}
	if len(valid) == 0 {
		return nil
	}
	// valid keeps cands' newest-first order; the first is the newest.
	out := valid[0]
	out.ShowDate = len(kinds) > 1
	return &out
}
