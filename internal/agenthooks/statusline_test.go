package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStatuslineText (BV-226 §6): counts in their plural forms, no red part
// with 0 red, no yellow part with 0 yellow.
func TestStatuslineText(t *testing.T) {
	en, es := cat(t, "en"), cat(t, "es")
	for _, c := range []struct {
		red, yellow int
		en, es      string
	}{
		{2, 1, "RosmarOps · 2 red · 1 yellow · 72/100 · {date}", "RosmarOps · 2 rojos · 1 amarillo · 72/100 · {date}"},
		{0, 3, "RosmarOps · 3 yellow · 72/100 · {date}", "RosmarOps · 3 amarillos · 72/100 · {date}"},
		{1, 0, "RosmarOps · 1 red · 72/100 · {date}", "RosmarOps · 1 rojo · 72/100 · {date}"},
		{0, 0, "RosmarOps · 72/100 · {date}", "RosmarOps · 72/100 · {date}"},
	} {
		if got := StatuslineText(en, c.red, c.yellow, 72); got != c.en {
			t.Errorf("en %d/%d: %q", c.red, c.yellow, got)
		}
		if got := StatuslineText(es, c.red, c.yellow, 72); got != c.es {
			t.Errorf("es %d/%d: %q", c.red, c.yellow, got)
		}
	}
}

// TestShowStatusline (BV-226 §2, §7): the date of the check, or how old it is
// past 8 days; an aborted check's line as is; the program's language, English
// when there is no line for it.
func TestShowStatusline(t *testing.T) {
	en, es := cat(t, "en"), cat(t, "es")
	dir := t.TempDir()
	checked := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	if err := WriteStatusline(dir, checked, map[string]string{"en": StatuslineText(en, 2, 0, 72), "es": StatuslineText(es, 2, 0, 72)}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, StatuslineFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := ShowStatusline(raw, "en", checked.Add(time.Hour), time.UTC, en); got != "RosmarOps · 2 red · 72/100 · 13 NOV 2026" {
		t.Errorf("fresh: %q", got)
	}
	if got := ShowStatusline(raw, "es", checked.Add(8*24*time.Hour), time.UTC, es); !strings.HasSuffix(got, es.DayLabel(checked)) {
		t.Errorf("8 days is not stale yet: %q", got)
	}
	if got := ShowStatusline(raw, "es", checked.Add(10*24*time.Hour+time.Hour), time.UTC, es); got != "RosmarOps · 2 rojos · 72/100 · última revisión hace 10 días" {
		t.Errorf("stale: %q", got)
	}
	if got := ShowStatusline(raw, "fr", checked, time.UTC, en); !strings.HasPrefix(got, "RosmarOps · 2 red") {
		t.Errorf("fallback to English: %q", got)
	}
	if err := WriteStatusline(dir, checked, map[string]string{"en": en.Text("statusline.aborted", nil)}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, StatuslineFile))
	if got := ShowStatusline(raw, "en", checked.Add(30*24*time.Hour), time.UTC, en); got != en.Text("statusline.aborted", nil) {
		t.Errorf("aborted: %q", got)
	}
	if got := ShowStatusline([]byte("garbage"), "en", checked, time.UTC, en); got != "" {
		t.Errorf("garbage: %q", got)
	}
}
