package main

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// progressInterval is how often, at most, the repositories line is
// rewritten on a console (informe HTML v3 §9.6).
const progressInterval = 250 * time.Millisecond

// progress prints the run's progress lines from the catalog: never a
// repository's name or a path. On a console the repositories line is
// rewritten in place (\r), at most every progressInterval and when the last
// one ends; elsewhere it is printed when the scan starts and when it ends.
type progress struct {
	mu       sync.Mutex
	out      io.Writer
	terminal bool
	cat      catalog.Catalog
	last     time.Time
	open     bool // a repositories line is being rewritten
	now      func() time.Time
	bar      *taskbar // BV-151: taskbar progress; nil when off
}

func newProgress(out io.Writer, terminal bool, cat catalog.Catalog) *progress {
	return &progress{out: out, terminal: terminal, cat: cat, now: time.Now, bar: ui.taskbar()}
}

// line prints one progress line, ending a repositories line in progress.
func (p *progress) line(key string, params map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.end()
	// BV-151: the matching taskbar state for the two step lines.
	switch key {
	case "console.progress.agents":
		p.bar.agents()
	case "console.progress.report":
		p.bar.report()
	}
	fmt.Fprintln(p.out, p.cat.Text(key, params))
}

func (p *progress) end() {
	if p.open {
		fmt.Fprintln(p.out)
		p.open = false
	}
}

// repos is repos.ScanConfig.OnProgress.
func (p *progress) repos(done, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := p.cat.Text("console.progress.repos", map[string]string{"done": strconv.Itoa(done), "total": strconv.Itoa(total)})
	first, last := done == 0, done == total
	if !p.terminal {
		if first || last {
			fmt.Fprintln(p.out, text)
		}
		return
	}
	if t := p.now(); first || last || t.Sub(p.last) >= progressInterval {
		p.last = t
		// BV-151: the taskbar percentage, under the same 250 ms limit. The
		// sequence goes on its own (no newline) before the line is rewritten.
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		p.bar.reposPct(pct)
		fmt.Fprint(p.out, "\r"+text)
		p.open = true
		if last {
			p.end()
		}
	}
}
