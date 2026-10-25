package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/schemas"
)

// HistoryFileName is the score history in the reports directory (I9): the
// reports directory is the user's, within the write scope of invariant 2;
// state.json is not touched.
const HistoryFileName = "index-history.json"

// MaxEntries is the history's length: two years of weekly runs. Past it,
// the oldest entry goes.
const MaxEntries = 104

// History mirrors schemas/v2/index-history.v1.schema.json. It never holds
// paths, repository names, services, fingerprints or host_id.
type History struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

// Entry is one run.
type Entry struct {
	Date             string       `json:"date"` // YYYY-MM-DD
	IndexVersion     int          `json:"index_version"`
	RulesVersion     string       `json:"rules_version"`
	KnowledgeVersion string       `json:"knowledge_version"`
	Agents           []EntryAgent `json:"agents"`
	Scope            EntryScope   `json:"scope"`
}

// EntryAgent is one agent's score in an entry.
type EntryAgent struct {
	Agent       string `json:"agent"`
	Value       *int   `json:"value"`
	Unavailable string `json:"unavailable,omitempty"`
	Band        string `json:"band,omitempty"`
	Red         int    `json:"red"`
	Yellow      int    `json:"yellow"`
}

// EntryScope is the scope of an entry: the evaluated agents and the number
// of repositories.
type EntryScope struct {
	Agents []string `json:"agents"`
	Repos  int      `json:"repos"`
}

// EntryOf is the history entry of an index, dated day (YYYY-MM-DD).
func EntryOf(idx Index, day string) Entry {
	e := Entry{
		Date: day, IndexVersion: idx.IndexVersion, RulesVersion: idx.RulesVersion, KnowledgeVersion: idx.KnowledgeVersion,
		Agents: []EntryAgent{}, Scope: EntryScope{Agents: idx.Scope.Evaluated, Repos: idx.Scope.Repos},
	}
	for _, a := range idx.Agents {
		e.Agents = append(e.Agents, EntryAgent{Agent: a.Agent, Value: a.Value, Unavailable: a.Unavailable, Band: a.Band, Red: a.Red, Yellow: a.Yellow})
	}
	return e
}

func validate(raw []byte) error {
	s, err := schemaval.Compile(schemas.IndexHistorySchema)
	if err != nil {
		return fmt.Errorf("compile index history schema: %w", err)
	}
	r, err := s.Validate(raw)
	if err != nil {
		return fmt.Errorf("validate index history: %w", err)
	}
	if !r.Valid() {
		msg := "index history fails schema validation:"
		for _, e := range r.Errors() {
			msg += "\n  " + e.String()
		}
		return errors.New(msg)
	}
	return nil
}

// ReadHistory reads dir/index-history.json, validated. A missing file is an
// empty history. A file that does not validate is renamed with the suffix
// .invalid in the same directory (a free name, never over another file) and
// the history starts again: reset is true, for the report to say so.
func ReadHistory(dir string) (h History, reset bool, err error) {
	path := filepath.Join(dir, HistoryFileName)
	h = History{SchemaVersion: 1, Entries: []Entry{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, false, nil
	}
	if err != nil {
		return h, false, fmt.Errorf("read index history: %w", err)
	}
	var parsed History
	if verr := validate(raw); verr == nil {
		if jerr := json.Unmarshal(raw, &parsed); jerr == nil {
			return parsed, false, nil
		}
	}
	target := path + ".invalid"
	for n := 1; ; n++ {
		if _, statErr := os.Lstat(target); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		target = path + "." + strconv.Itoa(n) + ".invalid"
	}
	if err := os.Rename(path, target); err != nil {
		return h, false, fmt.Errorf("set aside invalid index history: %w", err)
	}
	return h, true, nil
}

// Last is the last entry, nil for an empty history.
func (h History) Last() *Entry {
	if len(h.Entries) == 0 {
		return nil
	}
	e := h.Entries[len(h.Entries)-1]
	return &e
}

// WriteHistory appends e to h, keeps the last MaxEntries, validates and
// writes dir/index-history.json.
func WriteHistory(dir string, h History, e Entry) error {
	h.SchemaVersion = 1
	h.Entries = append(h.Entries, e)
	if len(h.Entries) > MaxEntries {
		h.Entries = h.Entries[len(h.Entries)-MaxEntries:]
	}
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index history: %w", err)
	}
	if err := validate(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create reports dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, HistoryFileName), append(raw, '\n'), 0o600)
}

// SeriesLen is how many scores the evolution line shows (informe HTML v2,
// §5): the last comparable ones.
const SeriesLen = 8

// Series is the agent's scores over the runs, oldest first, ending with
// cur: back from cur while each run is comparable with the next one (the
// rule of compare, I7), at most SeriesLen. Empty when cur has no score.
func Series(entries []Entry, cur Entry, agent string) []int {
	value := func(e Entry) *int {
		for _, a := range e.Agents {
			if a.Agent == agent {
				return a.Value
			}
		}
		return nil
	}
	next := cur
	v := value(cur)
	if v == nil {
		return []int{}
	}
	out := []int{*v}
	for i := len(entries) - 1; i >= 0 && len(out) < SeriesLen; i-- {
		e := entries[i]
		pv := value(e)
		if pv == nil || !comparableEntries(e, next) {
			break
		}
		out = append(out, *pv)
		next = e
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// comparableEntries is compare's rule between two runs, prev before next.
func comparableEntries(prev, next Entry) bool {
	return prev.IndexVersion == next.IndexVersion && prev.RulesVersion == next.RulesVersion &&
		prev.KnowledgeVersion == next.KnowledgeVersion && sameStrings(prev.Scope.Agents, next.Scope.Agents) &&
		float64(next.Scope.Repos) >= 0.9*float64(prev.Scope.Repos)
}
