package repos

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
)

// readDiffStream reads the output of gitStreamDiffs and calls deliver with
// each commit's hash and its added lines, extracted exactly as
// extractAddedLines extracts them from one `git show`: every line that
// starts with "+", except "+++" lines, without the "+", with its line end
// ("\r" kept). It never holds more than one commit's added lines.
//
// A commit is delivered when the next commit's marker arrives. The last
// commit is returned, not delivered: it is complete only if git exited
// cleanly, which the caller learns after the stream ends.
//
// Within a commit, a file whose added lines exceed MaxFileBytes is not
// scanned (invariant 6): its lines are dropped.
func readDiffStream(r io.Reader, deliver func(hash string, added []byte) error) (lastHash string, lastAdded []byte, err error) {
	lastHash, lastAdded, _, err = readDiffStreamFiles(r, func(hash string, added []byte, _ []fileSpan) error {
		return deliver(hash, added)
	})
	return lastHash, lastAdded, err
}

// fileSpan is where one file's added lines start in a commit's added
// lines, and the file's path (from its "+++ b/" header).
type fileSpan struct {
	start int
	path  string
}

// fileAt is the path of the file whose added lines hold offset.
func fileAt(spans []fileSpan, offset int) string {
	path := ""
	for _, s := range spans {
		if s.start > offset {
			break
		}
		path = s.path
	}
	return path
}

// readDiffStreamFiles is readDiffStream that also delivers, per commit,
// the span of each file in its added lines.
func readDiffStreamFiles(r io.Reader, deliver func(hash string, added []byte, files []fileSpan) error) (lastHash string, lastAdded []byte, lastFiles []fileSpan, err error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var (
		hash     string
		commit   []byte     // added lines of the files done so far
		spans    []fileSpan // where each file starts in commit
		path     string     // the current file
		file     []byte     // added lines of the current file
		fileOver bool       // the current file exceeded MaxFileBytes
	)
	endFile := func() {
		if !fileOver && len(file) > 0 {
			spans = append(spans, fileSpan{start: len(commit), path: path})
			commit = append(commit, file...)
		}
		file, fileOver = nil, false
	}
	// Per line: the class is set from its first fragment.
	const (
		lineOther = iota
		lineAdded
	)
	class, start := lineOther, true
	for {
		frag, rerr := br.ReadSlice('\n')
		ends := len(frag) > 0 && frag[len(frag)-1] == '\n'
		if len(frag) > 0 {
			if start {
				class = lineOther
				switch {
				case frag[0] == 0 && isMarker(frag):
					endFile()
					if hash != "" {
						if err := deliver(hash, commit, spans); err != nil {
							return "", nil, nil, err
						}
					}
					hash, commit, spans, path = string(bytes.TrimRight(frag[1:], "\r\n")), nil, nil, ""
				case bytes.HasPrefix(frag, []byte("diff --git ")), bytes.HasPrefix(frag, []byte("diff --cc ")),
					bytes.HasPrefix(frag, []byte("diff --combined ")):
					endFile()
				case bytes.HasPrefix(frag, []byte("+++")):
					if ends {
						path = diffPath(frag)
					}
				case frag[0] == '+':
					class = lineAdded
					frag = frag[1:]
				}
			}
			if class == lineAdded && !fileOver {
				file = append(file, frag...)
				if len(file) > MaxFileBytes {
					file, fileOver = nil, true
				}
			}
			start = ends
		}
		switch {
		case rerr == nil, errors.Is(rerr, bufio.ErrBufferFull):
			continue
		case errors.Is(rerr, io.EOF):
			if !start && class == lineAdded && !fileOver {
				// extractAddedLines ends every added line with "\n".
				file = append(file, '\n')
			}
			endFile()
			return hash, commit, spans, nil
		default:
			return "", nil, nil, rerr
		}
	}
}

// diffPath is the path of a "+++ b/<path>" header line (quoted by git
// when it has special characters); empty when it has no b/ side.
func diffPath(line []byte) string {
	p := strings.TrimRight(string(line[3:]), "\r\n")
	p = strings.TrimPrefix(p, " ")
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
	}
	if !strings.HasPrefix(p, "b/") {
		return ""
	}
	return p[2:]
}

// isMarker reports a commit marker line: NUL, a 40- or 64-digit lowercase
// hex hash, end of line.
func isMarker(line []byte) bool {
	h := bytes.TrimRight(line[1:], "\r\n")
	if len(h) != 40 && len(h) != 64 {
		return false
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
