package problems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAggregatesAndKeepsSeparate(t *testing.T) {
	dir := t.TempDir()
	nb := New(dir)

	// Same (code, step, agent, http_status, day) aggregates.
	if _, err := nb.Record(Entry{Code: SpaceRejected, Step: StepSpaceSend, HTTPStatus: 409, Day: "2026-11-09"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Record(
		Entry{Code: SpaceRejected, Step: StepSpaceSend, HTTPStatus: 409, Day: "2026-11-09"},
		Entry{Code: SpaceRejected, Step: StepSpaceSend, HTTPStatus: 409, Day: "2026-11-09", Count: 2},
	); err != nil {
		t.Fatal(err)
	}
	// A different day is a separate line.
	if _, err := nb.Record(Entry{Code: ConfigUnreadable, Step: StepAgents, Agent: "cline", Day: "2026-11-09"}); err != nil {
		t.Fatal(err)
	}

	entries, err := nb.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 lines, got %d: %+v", len(entries), entries)
	}
	var rej *Entry
	for i := range entries {
		if entries[i].Code == SpaceRejected {
			rej = &entries[i]
		}
	}
	if rej == nil || rej.Count != 4 {
		t.Fatalf("space_rejected count want 4, got %+v", rej)
	}
}

func TestRecordDropsUnknownCodeOrStep(t *testing.T) {
	nb := New(t.TempDir())
	n, err := nb.Record(
		Entry{Code: "not_a_code", Step: StepReview, Day: "2026-11-09"},
		Entry{Code: StepFailed, Step: "not_a_step", Day: "2026-11-09"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 recorded, got %d", n)
	}
	entries, _ := nb.Load()
	if len(entries) != 0 {
		t.Fatalf("notebook should be empty, got %+v", entries)
	}
}

func TestMaxLinesDropsOldest(t *testing.T) {
	nb := New(t.TempDir())
	// 205 distinct days -> 205 lines, capped to the newest 200.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 205; i++ {
		day := Day(base.AddDate(0, 0, i))
		if _, err := nb.Record(Entry{Code: StepFailed, Step: "vscode", Day: day}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := nb.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxLines {
		t.Fatalf("want %d lines, got %d", MaxLines, len(entries))
	}
	// The five oldest days are gone.
	if entries[0].Day != Day(base.AddDate(0, 0, 5)) {
		t.Fatalf("oldest kept day = %s, want %s", entries[0].Day, Day(base.AddDate(0, 0, 5)))
	}
}

func TestUnsentAndMarkSent(t *testing.T) {
	nb := New(t.TempDir())
	_, _ = nb.Record(
		Entry{Code: StepFailed, Step: "mcp", Day: "2026-11-09"},
		Entry{Code: TimeRanOut, Step: StepReview, Day: "2026-11-09"},
	)
	unsent, err := nb.Unsent(MaxPerSend)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 2 {
		t.Fatalf("want 2 unsent, got %d", len(unsent))
	}
	if _, err := nb.MarkSent(unsent); err != nil {
		t.Fatal(err)
	}
	after, _ := nb.Unsent(MaxPerSend)
	if len(after) != 0 {
		t.Fatalf("want 0 unsent after mark, got %d", len(after))
	}
	// A fresh occurrence after a send starts a new unsent line, not touching the
	// sent one.
	_, _ = nb.Record(Entry{Code: StepFailed, Step: "mcp", Day: "2026-11-09"})
	again, _ := nb.Unsent(MaxPerSend)
	if len(again) != 1 || again[0].Count != 1 {
		t.Fatalf("want 1 fresh unsent line count 1, got %+v", again)
	}
	all, _ := nb.Load()
	if len(all) != 3 {
		t.Fatalf("want 3 lines total (2 sent + 1 unsent), got %d", len(all))
	}
}

func TestRunMarkDetectsUnfinished(t *testing.T) {
	dir := t.TempDir()
	nb := New(dir)
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	prev, err := nb.StartReview(now)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "" {
		t.Fatalf("first start should see no previous unfinished review, got %q", prev)
	}
	// Crash: no EndReview. Next startup sees the mark.
	nb2 := New(dir)
	prev2, err := nb2.StartReview(now.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if prev2 != "2026-11-09" {
		t.Fatalf("want previous unfinished day 2026-11-09, got %q", prev2)
	}
	// Clean finish clears the mark.
	if err := nb2.EndReview(); err != nil {
		t.Fatal(err)
	}
	nb3 := New(dir)
	prev3, _ := nb3.StartReview(now.AddDate(0, 0, 14))
	if prev3 != "" {
		t.Fatalf("after EndReview no unfinished review expected, got %q", prev3)
	}
	_ = nb3.EndReview()
	if _, err := os.Stat(filepath.Join(dir, RunMarkName)); !os.IsNotExist(err) {
		t.Fatalf("run mark should be gone after EndReview")
	}
}

func TestLoadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	nb := New(dir)
	content := strings.Join([]string{
		`{"code":"step_failed","step":"mcp","day":"2026-11-09","count":1,"sent":false}`,
		`not json at all`,
		`{"code":"bogus","step":"mcp","day":"2026-11-09","count":1}`,
		`{"code":"time_ran_out","step":"review","day":"2026-11-09","count":3,"sent":true}`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, NotebookName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := nb.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 valid lines, got %d: %+v", len(entries), entries)
	}
}

// TestLoadDropsBadDayOrCountKeepsGood is CTO-32 N1: a line whose day is not a
// real date, or whose count is above the problems.v1 maximum, would fail
// Marshal and block every send. Load drops it and keeps the good lines, so the
// good problems still go out.
func TestLoadDropsBadDayOrCountKeepsGood(t *testing.T) {
	dir := t.TempDir()
	nb := New(dir)
	content := strings.Join([]string{
		`{"code":"step_failed","step":"mcp","day":"2026-11-09","count":1}`,               // good
		`{"code":"time_ran_out","step":"review","day":"2026-13-40","count":1}`,           // day is not a real date
		`{"code":"config_unreadable","step":"agents","day":"not-a-day","count":1}`,       // day out of pattern
		`{"code":"space_rejected","step":"space_send","day":"2026-11-09","count":10001}`, // count over the max
		`{"code":"report_not_opened","step":"notify","day":"2026-11-09","count":2}`,      // good
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, NotebookName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := nb.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want only the 2 good lines, got %d: %+v", len(entries), entries)
	}
	// A line at exactly the maximum is kept.
	if err := os.WriteFile(filepath.Join(dir, NotebookName),
		[]byte(`{"code":"step_failed","step":"mcp","day":"2026-11-09","count":10000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unsent, err := nb.Unsent(MaxPerSend)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 1 || unsent[0].Count != MaxCount {
		t.Fatalf("count at the maximum should survive: %+v", unsent)
	}
}
