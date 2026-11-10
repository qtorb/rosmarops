package problems

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NotebookName is the local problem notebook, JSON Lines, in the reports data
// directory next to rosmarops-sends.log (an already-allowed path, invariant 2).
const NotebookName = "rosmarops-problems.jsonl"

// RunMarkName is the sentinel written while a review is in progress. If it is
// still there at the next startup, the previous review did not finish
// (run_unfinished). It holds only the day the review started — never anything
// sensitive.
const RunMarkName = "rosmarops-run.inprogress"

// MaxLines caps the notebook; the oldest lines are dropped past it (§1).
const MaxLines = 200

// MaxPerSend caps how many unsent lines one send carries (problems.v1 allows up
// to 20 problems, §4/§11).
const MaxPerSend = 20

// Entry is one notebook line: one code, part, agent and day, with a running
// count. It carries nothing free-form (§1). Agent and HTTPStatus are omitted
// when absent so lines without them compare and read cleanly.
type Entry struct {
	Code       Code   `json:"code"`
	Step       string `json:"step"`
	Agent      string `json:"agent,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Day        string `json:"day"`
	Count      int    `json:"count"`
	Sent       bool   `json:"sent"`
}

// key is the aggregation key: a repeated (code, step, agent, http_status, day)
// increments an existing unsent line instead of appending a new one.
func (e Entry) key() Entry {
	return Entry{Code: e.Code, Step: e.Step, Agent: e.Agent, HTTPStatus: e.HTTPStatus, Day: e.Day}
}

// Day formats t as the notebook's day (local date, no time).
func Day(t time.Time) string { return t.Format("2006-01-02") }

// Notebook is the problem notebook and run mark in one data directory. All its
// methods are best-effort and local; none does any network.
type Notebook struct{ dir string }

// New returns the notebook rooted at the reports data directory.
func New(dataDir string) *Notebook { return &Notebook{dir: dataDir} }

func (nb *Notebook) notebookPath() string { return filepath.Join(nb.dir, NotebookName) }
func (nb *Notebook) markPath() string     { return filepath.Join(nb.dir, RunMarkName) }

// Load reads the notebook. A missing file is an empty notebook. Malformed or
// invalid lines are skipped so one bad line never loses the rest.
func (nb *Notebook) Load() ([]Entry, error) {
	f, err := os.Open(nb.notebookPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open problem notebook: %w", err)
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if !ValidCode(e.Code) || !ValidStep(e.Step) || e.Day == "" || e.Count < 1 {
			continue
		}
		// Defence in depth (§7): an agent that is not a known product id, or an
		// out-of-range status, is dropped even from a hand-edited notebook, so
		// nothing but closed-list values can ever be read back and sent.
		if e.Agent != "" && !ValidAgent(e.Agent) {
			e.Agent = ""
		}
		if e.HTTPStatus != 0 && (e.HTTPStatus < 100 || e.HTTPStatus > 599) {
			e.HTTPStatus = 0
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read problem notebook: %w", err)
	}
	return out, nil
}

// save writes entries back as JSON Lines, capping to the newest MaxLines.
func (nb *Notebook) save(entries []Entry) error {
	if len(entries) > MaxLines {
		entries = entries[len(entries)-MaxLines:]
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode problem line: %w", err)
		}
	}
	if err := os.MkdirAll(nb.dir, 0o755); err != nil {
		return fmt.Errorf("create reports data dir: %w", err)
	}
	if err := os.WriteFile(nb.notebookPath(), buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write problem notebook: %w", err)
	}
	return nil
}

// Record aggregates the given problems into the notebook and persists it. Each
// incoming entry with a Count < 1 counts as 1. A repeated (code, step, agent,
// http_status, day) increments the matching unsent line; otherwise a new unsent
// line is appended. Entries with an unknown code or step are dropped (never
// written). Returns the number actually recorded.
func (nb *Notebook) Record(incoming ...Entry) (int, error) {
	if len(incoming) == 0 {
		return 0, nil
	}
	entries, err := nb.Load()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, in := range incoming {
		if !ValidCode(in.Code) || !ValidStep(in.Step) {
			continue
		}
		// Keep only values that fit problems.v1; drop an out-of-range agent or
		// status rather than the whole line.
		if in.Agent != "" && !ValidAgent(in.Agent) {
			in.Agent = ""
		}
		if in.HTTPStatus != 0 && (in.HTTPStatus < 100 || in.HTTPStatus > 599) {
			in.HTTPStatus = 0
		}
		in.Day = pickDay(in.Day)
		c := in.Count
		if c < 1 {
			c = 1
		}
		if i := findUnsent(entries, in.key()); i >= 0 {
			entries[i].Count += c
		} else {
			e := in.key()
			e.Count = c
			entries = append(entries, e)
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	return n, nb.save(entries)
}

// Unsent returns the unsent lines, newest-key order preserved, up to max
// (max <= 0 means all).
func (nb *Notebook) Unsent(max int) ([]Entry, error) {
	entries, err := nb.Load()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if !e.Sent {
			out = append(out, e)
			if max > 0 && len(out) >= max {
				break
			}
		}
	}
	return out, nil
}

// MarkSent marks the lines matching the given keys as sent and persists. A key
// matches the first still-unsent line with the same (code, step, agent,
// http_status, day). Returns the number marked.
func (nb *Notebook) MarkSent(sent []Entry) (int, error) {
	if len(sent) == 0 {
		return 0, nil
	}
	entries, err := nb.Load()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range sent {
		for i := range entries {
			if !entries[i].Sent && entries[i].key() == s.key() {
				entries[i].Sent = true
				n++
				break
			}
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, nb.save(entries)
}

// StartReview is called at the start of a review. It reports the day of a
// previous review that did not finish (empty when none), then writes the mark
// for this review so a crash before EndReview is detected next time. The caller
// records run_unfinished for a non-empty prevDay.
func (nb *Notebook) StartReview(now time.Time) (prevDay string, err error) {
	if raw, e := os.ReadFile(nb.markPath()); e == nil {
		prevDay = pickDay(string(bytes.TrimSpace(raw)))
	}
	if e := os.MkdirAll(nb.dir, 0o755); e != nil {
		return prevDay, fmt.Errorf("create reports data dir: %w", e)
	}
	if e := os.WriteFile(nb.markPath(), []byte(Day(now)+"\n"), 0o600); e != nil {
		return prevDay, fmt.Errorf("write run mark: %w", e)
	}
	return prevDay, nil
}

// EndReview clears the in-progress mark after a review finishes cleanly. A
// missing mark is not an error.
func (nb *Notebook) EndReview() error {
	if err := os.Remove(nb.markPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear run mark: %w", err)
	}
	return nil
}

// pickDay returns a valid YYYY-MM-DD day: the given one if it already looks
// like a date, otherwise today's local date. This keeps a stray or empty mark
// from producing an out-of-pattern day.
func pickDay(day string) string {
	if _, err := time.Parse("2006-01-02", day); err == nil {
		return day
	}
	return Day(time.Now())
}

func findUnsent(entries []Entry, key Entry) int {
	for i := range entries {
		if !entries[i].Sent && entries[i].key() == key {
			return i
		}
	}
	return -1
}
