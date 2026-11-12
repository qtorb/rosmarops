package main

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// progressInterval is how often, at most, a change in the repositories count
// rewrites its line on a console (informe HTML v3 §9.6).
const progressInterval = 250 * time.Millisecond

// reposSlowAfter is how long the repositories count may stay still before the
// console shows the «this one has a long history» note (BV-211 §3), so a slow
// repository does not look like a hang. The note is shown at most once per
// review, for the first repository that stalls (BV-221).
const reposSlowAfter = 15 * time.Second

// progress prints the run's progress lines from the catalog: never a
// repository's name or a path. On a console the repositories line is rewritten
// in place (\r) — on every counted change (throttled) and, so it never looks
// hung, once a second by a background refresher that also carries the elapsed
// time and shows the slow-repository note. Elsewhere it is printed when the
// scan starts and when it ends.
type progress struct {
	mu       sync.Mutex
	out      io.Writer
	terminal bool
	cat      catalog.Catalog
	last     time.Time // last rewrite, for the change throttle
	open     bool      // a repositories line is being rewritten
	now      func() time.Time
	bar      *taskbar // BV-151: taskbar progress; nil when off

	// BV-211 §3: the repositories phase clock and slow-repository note.
	reposActive bool
	reposStart  time.Time // when the repositories phase began
	reposDone   int
	reposTotal  int
	lastChange  time.Time // when reposDone last moved
	slowShown   bool      // the slow note was shown in this review (BV-221: once)
	stopTick    chan struct{}
}

func newProgress(out io.Writer, terminal bool, cat catalog.Catalog) *progress {
	return &progress{out: out, terminal: terminal, cat: cat, now: time.Now, bar: ui.taskbar()}
}

// start runs a background refresher that rewrites the repositories line once a
// second (so the elapsed time ticks and a slow repository is explained even
// when the count does not move). It does nothing off a console. stop ends it.
func (p *progress) start() {
	if !p.terminal {
		return
	}
	stop := make(chan struct{})
	p.mu.Lock()
	p.stopTick = stop
	p.mu.Unlock()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.tick()
			}
		}
	}()
}

func (p *progress) stop() {
	p.mu.Lock()
	if p.stopTick != nil {
		close(p.stopTick)
		p.stopTick = nil
	}
	p.mu.Unlock()
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

// repos is repos.ScanConfig.OnProgress: the repositories done and the total.
func (p *progress) repos(done, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	first := !p.reposActive
	if first {
		p.reposActive = true
		p.reposStart = p.now()
		p.lastChange = p.now()
	}
	if done != p.reposDone {
		p.reposDone = done
		p.lastChange = p.now()
	}
	p.reposTotal = total
	last := total > 0 && done >= total

	if !p.terminal {
		if first || last {
			fmt.Fprintln(p.out, p.reposText())
		}
		if last {
			p.reposActive = false
		}
		return
	}
	// BV-151: the taskbar percentage, under the same throttle.
	if t := p.now(); first || last || t.Sub(p.last) >= progressInterval {
		p.last = t
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		p.bar.reposPct(pct)
		p.renderReposLocked()
	}
	if last {
		p.end()
		p.reposActive = false
	}
}

// tick is the once-a-second refresh from the background goroutine: it updates
// the elapsed time and shows the slow-repository note the first time the count
// has not moved for reposSlowAfter; later stalls in the same review show no
// note (BV-221). It does nothing outside the repositories phase.
func (p *progress) tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.reposActive || !p.terminal {
		return
	}
	if !p.slowShown && p.now().Sub(p.lastChange) >= reposSlowAfter {
		p.slowShown = true
		p.end() // finish the current line so the note gets its own
		fmt.Fprintln(p.out, p.cat.Text("console.progress.repos_slow", nil))
	}
	p.renderReposLocked()
}

// renderReposLocked rewrites the repositories line in place. Caller holds mu.
func (p *progress) renderReposLocked() {
	fmt.Fprint(p.out, "\r"+p.reposText())
	p.open = true
}

// reposText is the repositories line from the catalog, with the elapsed time
// since the phase began in m:ss. Caller holds mu.
func (p *progress) reposText() string {
	return p.cat.Text("console.progress.repos", map[string]string{
		"done":    strconv.Itoa(p.reposDone),
		"total":   strconv.Itoa(p.reposTotal),
		"elapsed": formatElapsed(p.now().Sub(p.reposStart)),
	})
}

// formatElapsed renders a duration as m:ss (a negative or zero duration is
// 0:00).
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}
