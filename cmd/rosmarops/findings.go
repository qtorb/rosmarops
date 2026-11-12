package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// The «No me afecta» flow (BV-184): after a review (double click or interactive
// terminal), and from `rosmarops findings` at any time, the person marks the
// findings that do not apply to them. A marked finding's id goes to
// allow_list.dismissed; from the next review it no longer counts in the score
// and is dropped from the report and the space package. It is reversible: the
// dismissed findings show marked and can be un-marked. The space never decides
// what is dismissed — only this computer does.
//
// This file holds the cross-platform core: it reads the last review's findings
// from report.v2.json, shows the window (Windows) or the console list (macOS
// and Linux), and, on «Save», writes config.json and re-points the integrity
// reference so the next review does not ask «configuration changed». The window
// itself is in findingswindow_windows.go; the console in findingswindow_other.go.

// dismissUI shows the findings and returns the ids the person left marked and
// whether they saved. It is a seam: production is showDismiss (the window on
// Windows, the console on macOS/Linux); the cmd tests disable it, since a real
// window or a console prompt would hang a headless test.
var dismissUIHook = showDismiss

// offerDismissAfterReport shows the «No me afecta» UI for the last review and
// saves the person's choice. onlyIfNew gates the automatic offer after a review
// to runs with at least one new finding (BV-184 point 3); `rosmarops findings`
// passes false. A missing or unreadable report just means no offer. now is the
// clock (tests pass a fixed one).
func offerDismissAfterReport(lang, cfgPath, stPath string, onlyIfNew bool, now time.Time) {
	cat, err := catalog.Load(lang)
	if err != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Info("dismiss: config not read; not offering", "err", err)
		return
	}
	reportDir := cfg.ReportDirectory
	if reportDir == "" {
		reportDir, err = report.DefaultReportDir()
		if err != nil {
			slog.Info("dismiss: report dir not resolved; not offering", "err", err)
			return
		}
	}
	home, _ := os.UserHomeDir()
	// The repositories the review checked come from the project search, as in
	// the review itself (CTO-38 §2.5); repo_roots is only its input.
	v, err := report.ReadV2(report.DataDir(reportDir), searchProjects(cfg, state.State{}).Repos(), 0)
	if err != nil {
		slog.Info("dismiss: last review not read; not offering", "err", err)
		return
	}
	cards := report.FindingCards(v, cat, home)
	if len(cards) == 0 {
		return // nothing to dismiss or to recover
	}
	if onlyIfNew && !anyNewCard(cards) {
		return // after a review, only when something is new (BV-184 point 3)
	}
	checked, saved := dismissUIHook(cards, cat)
	if !saved {
		return // «Not now» or closed: nothing changes
	}
	list := recomputeDismissed(cfg.AllowList.Dismissed, cards, checked, now.Local().Format("2006-01-02"))
	if err := writeDismissed(cfgPath, reportDir, stPath, list); err != nil {
		slog.Warn("dismiss: the choice could not be saved", "err", err)
	}
}

// consoleFindingLine is one numbered line of the console list (BV-184 point 5):
// «  N. [<pill>] <title> — <sub> · <agent>», with the dismissed mark appended
// for a finding the person has already dismissed.
func consoleFindingLine(n int, c report.FindingCard, cat catalog.Catalog) string {
	pill := cat.Text("findings."+c.Severity, nil)
	var sub string
	if c.New {
		sub = cat.Text("findings.sub.new", nil)
	} else {
		sub = cat.Text("findings.sub.since", map[string]string{"date": c.Since})
	}
	line := fmt.Sprintf("  %d. [%s] %s — %s", n, pill, c.Title, sub)
	if c.Agent != "" {
		line += " · " + c.Agent
	}
	if c.Group == report.CardGroupDismissed {
		line += " (" + cat.Text("findings.console.dismissed", nil) + ")"
	}
	return line
}

// applyConsoleToggles flips the state of the findings whose 1-based numbers the
// person typed (comma-separated) and returns the ids that end up dismissed. A
// finding starts dismissed only if it is in the dismissed group. An empty line,
// or one with no valid number, changes nothing and is not saved.
func applyConsoleToggles(cards []report.FindingCard, answer string) ([]string, bool) {
	dismissed := make([]bool, len(cards))
	for i, c := range cards {
		dismissed[i] = c.Group == report.CardGroupDismissed
	}
	changed := false
	for _, tok := range strings.Split(answer, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n < 1 || n > len(cards) {
			continue
		}
		dismissed[n-1] = !dismissed[n-1]
		changed = true
	}
	if !changed {
		return nil, false
	}
	var ids []string
	for i, c := range cards {
		if dismissed[i] {
			ids = append(ids, c.ID)
		}
	}
	return ids, true
}

// anyNewCard reports whether any card is a new finding.
func anyNewCard(cards []report.FindingCard) bool {
	for _, c := range cards {
		if c.New {
			return true
		}
	}
	return false
}

// recomputeDismissed is the new allow_list.dismissed after the person saved:
// the ids they left marked among the findings shown, plus the dismissed ids
// that did not appear in this review (kept, BV-184 point 1). A newly dismissed
// id takes today's date; one already dismissed keeps its date. Sorted by id.
func recomputeDismissed(old []config.AllowEntryDismissed, cards []report.FindingCard, checked []string, today string) []config.AllowEntryDismissed {
	oldByID := make(map[string]config.AllowEntryDismissed, len(old))
	for _, e := range old {
		oldByID[e.ID] = e
	}
	visible := make(map[string]bool, len(cards))
	for _, c := range cards {
		visible[c.ID] = true
	}
	checkedSet := make(map[string]bool, len(checked))
	for _, id := range checked {
		checkedSet[id] = true
	}
	var out []config.AllowEntryDismissed
	// The dismissed ids that did not appear this run stay in the list.
	for _, e := range old {
		if !visible[e.ID] {
			out = append(out, e)
		}
	}
	// The findings shown that the person left marked.
	for _, c := range cards {
		if !checkedSet[c.ID] {
			continue
		}
		if e, ok := oldByID[c.ID]; ok {
			out = append(out, e) // keep its original date
		} else {
			out = append(out, config.AllowEntryDismissed{ID: c.ID, Date: today})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// writeDismissed writes allow_list.dismissed into config.json and re-points the
// integrity reference, so the next review does not ask about the change (BV-184
// point 2): config.json atomically (a temp file in the same directory, then a
// rename), its new SHA-256 into state.integrity.config_sha256, the accepted
// copy in data/ rewritten, and the state saved. Only allow_list.dismissed is
// touched; every other key of the file is kept as written.
func writeDismissed(cfgPath, reportDir, stPath string, list []config.AllowEntryDismissed) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	al, _ := m["allow_list"].(map[string]any)
	if al == nil {
		al = map[string]any{}
	}
	if len(list) == 0 {
		delete(al, "dismissed")
	} else {
		arr := make([]any, 0, len(list))
		for _, e := range list {
			arr = append(arr, map[string]any{"id": e.ID, "date": e.Date})
		}
		al["dismissed"] = arr
	}
	if len(al) == 0 {
		delete(m, "allow_list")
	} else {
		m["allow_list"] = al
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	out = append(out, '\n')
	if err := atomicWriteFile(cfgPath, out); err != nil {
		return err
	}
	// Re-point the integrity reference to the configuration we just wrote.
	if _, err := config.LoadConfig(cfgPath); err != nil {
		return fmt.Errorf("the written configuration is invalid: %w", err)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	st.Integrity.ConfigSHA256 = hash.Bytes(out)
	if err := writeAcceptedCopy(reportDir, out); err != nil {
		return err
	}
	if err := state.SaveState(stPath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// atomicWriteFile writes data to path through a temporary file in the same
// directory and a rename, so a crash never leaves a half-written config.json.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rosmarops-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
