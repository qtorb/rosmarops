package main

import (
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
)

// A window's PowerShell error output is thrown away: a person never sees it
// and it may name their folders. Only under a window's self-test hook (the
// journey tests on Windows) it is kept, bounded and with nothing of the
// computer in it, and written to the run's log when the window ends, so a
// window that ends without a word shows why.

// selfTestStderrLimit is how much of the error output is kept: the first 4 KB.
const selfTestStderrLimit = 4096

// stderrTail keeps the first selfTestStderrLimit bytes written to it and
// drops the rest. It never fails a write, so the process is never blocked.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
	cut bool
}

func (s *stderrTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if room := selfTestStderrLimit - len(s.buf); room > 0 {
		if len(p) > room {
			s.buf, s.cut = append(s.buf, p[:room]...), true
		} else {
			s.buf = append(s.buf, p...)
		}
	} else if len(p) > 0 {
		s.cut = true
	}
	return len(p), nil
}

// selfTestStderr is the writer for a window's error output: a stderrTail under
// the window's self-test hook env, and nil without it, so the process's error
// output stays discarded exactly as before.
func selfTestStderr(env string) *stderrTail {
	if os.Getenv(env) == "" {
		return nil
	}
	return &stderrTail{}
}

// logSelfTestStderr writes what the window wrote on its error output to the
// run's log, without the user's folders or any environment value. Nothing is
// logged without the hook (s nil) or when the window wrote nothing.
func logSelfTestStderr(s *stderrTail, window string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	text, cut := string(s.buf), s.cut
	s.mu.Unlock()
	text = strings.TrimSpace(scrubComputer(text, os.Environ()))
	if text == "" {
		return
	}
	slog.Info("window self-test: error output", "window", window, "stderr", text, "cut", cut)
}

// scrubComputer removes from text what belongs to this computer: the value of
// every environment variable (paths such as the user's profile, the user and
// computer names) is replaced by its name, longest values first so a path is
// replaced whole before a part of it. Values shorter than 4 characters are
// left, so that words and numbers are not mangled.
func scrubComputer(text string, environ []string) string {
	type kv struct{ name, value string }
	var vars []kv
	for _, e := range environ {
		name, value, ok := strings.Cut(e, "=")
		if !ok || len(value) < 4 || name == "" {
			continue
		}
		vars = append(vars, kv{name, value})
		// Windows paths come both with \ and with /.
		if alt := strings.ReplaceAll(value, `\`, "/"); alt != value {
			vars = append(vars, kv{name, alt})
		}
	}
	sort.SliceStable(vars, func(i, j int) bool { return len(vars[i].value) > len(vars[j].value) })
	for _, v := range vars {
		text = replaceFold(text, v.value, "%"+v.name+"%")
	}
	return text
}

// replaceFold replaces every occurrence of old in s, ignoring case (Windows
// paths and names are case-insensitive), with repl.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	lower, lowOld := strings.ToLower(s), strings.ToLower(old)
	if len(lower) != len(s) { // a case change altered byte lengths: exact only
		return strings.ReplaceAll(s, old, repl)
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, lowOld)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s, lower = s[i+len(old):], lower[i+len(old):]
	}
}
