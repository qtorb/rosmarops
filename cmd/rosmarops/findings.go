package main

import (
	"encoding/json"
	"fmt"
	"io"
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

// findingChoice is what the person left chosen when they saved (BV-236 §5):
// the ids marked «Descartar» and the ids marked «Lo asumo». An id is in at most
// one of them; a finding in neither is pending.
type findingChoice struct {
	Dismissed []string
	Accepted  []string
	// Rotated are the card ids marked «Ya la he cambiado» (BV-314): only
	// cards of a key saved in git, whose credential goes to
	// allow_list.rotated_credentials.
	Rotated []string
}

// dismissUIHook shows the findings and returns the person's choice and whether
// they saved. It is a seam: production is showDismiss (the window on Windows,
// the console on macOS/Linux); the cmd tests disable it, since a real window
// or a console prompt would hang a headless test.
var dismissUIHook = showDismiss

// offerDismissAfterReport shows the «No me afecta» UI for the last review and
// saves the person's choice. onlyIfNew gates the automatic offer after a
// double-click review to runs with at least one new finding (BV-184 point 3);
// `rosmarops findings` passes false. A missing or unreadable report just means
// no offer. now is the clock (tests pass a fixed one).
func offerDismissAfterReport(lang, cfgPath, stPath string, onlyIfNew bool, now time.Time) {
	o, ok := loadDismissOffer(lang, cfgPath, onlyIfNew)
	if !ok {
		return
	}
	choice, saved := dismissUIHook(o.cards, o.cat)
	if !saved {
		return // «Not now» or closed: nothing changes
	}
	if err := saveFindingChoice(cfgPath, o.reportDir, stPath, o.cfg.AllowList, o.cards, choice, now.Local().Format("2006-01-02")); err != nil {
		slog.Warn("dismiss: the choice could not be saved", "err", err)
	}
}

// findingsHintAfterReport is what run prints after the report instead of
// asking about the findings: one line naming `rosmarops findings`, under the
// same conditions the question had (a configuration of the person's own and
// at least one new finding).
func findingsHintAfterReport(out io.Writer, lang, cfgPath string) {
	if o, ok := loadDismissOffer(lang, cfgPath, true); ok {
		fmt.Fprintln(out, o.cat.Text("console.findings_hint", nil))
	}
}

// dismissOffer is what the findings UI shows: the last review's findings and
// where the choice is saved.
type dismissOffer struct {
	cat       catalog.Catalog
	cfg       config.Config
	reportDir string
	cards     []report.FindingCard
}

// loadDismissOffer reads the last review's findings for the UI; false when
// there is nothing to offer.
func loadDismissOffer(lang, cfgPath string, onlyIfNew bool) (dismissOffer, bool) {
	// CTO-49 v2: what is dismissed is only ever written to
	// ~/.rosmarops/config.json. A configuration read from anywhere else (a
	// ./rosmarops.config.json in the working directory) is never offered for
	// change; `rosmarops findings` says so (findings.config_outside).
	if outside, err := dismissConfigOutside(cfgPath); outside {
		slog.Info("dismiss: configuration outside ~/.rosmarops; not offering", "path", cfgPath, "err", err)
		return dismissOffer{}, false
	}
	cat, err := catalog.Load(lang)
	if err != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Info("dismiss: config not read; not offering", "err", err)
		return dismissOffer{}, false
	}
	reportDir := cfg.ReportDirectory
	if reportDir == "" {
		reportDir, err = report.DefaultReportDir()
		if err != nil {
			slog.Info("dismiss: report dir not resolved; not offering", "err", err)
			return dismissOffer{}, false
		}
	}
	home, _ := os.UserHomeDir()
	// The repositories the review checked come from the project search, as in
	// the review itself (CTO-38 §2.5); repo_roots is only its input.
	v, err := report.ReadV2(report.DataDir(reportDir), searchProjects(cfg, state.State{}).Repos(), 0)
	if err != nil {
		slog.Info("dismiss: last review not read; not offering", "err", err)
		return dismissOffer{}, false
	}
	cards := report.FindingCards(v, cat, home)
	if len(cards) == 0 {
		return dismissOffer{}, false // nothing to dismiss or to recover
	}
	if onlyIfNew && !anyNewCard(cards) {
		return dismissOffer{}, false // after a review, only when something is new (BV-184 point 3)
	}
	return dismissOffer{cat: cat, cfg: cfg, reportDir: reportDir, cards: cards}, true
}

// saveFindingChoice writes the lists after the person saved (BV-236 §5,
// BV-314): allow_list.dismissed and allow_list.accepted recomputed from what
// they left chosen among the findings shown, keeping the entries of findings
// that did not appear in this review, and allow_list.rotated_credentials with
// the credentials of the keys they marked «Ya la he cambiado», through
// writeFindingLists. A finding is in at most one of the three: an id kept from
// an earlier review never stays in another list too.
func saveFindingChoice(cfgPath, reportDir, stPath string, al config.AllowList, cards []report.FindingCard, choice findingChoice, today string) error {
	rotatedCards := map[string]bool{}
	for _, id := range choice.Rotated {
		rotatedCards[id] = true
	}
	notRotated := func(ids []string) []string {
		var out []string
		for _, id := range ids {
			if !rotatedCards[id] {
				out = append(out, id)
			}
		}
		return out
	}
	dismissed := recomputeDismissed(al.Dismissed, cards, notRotated(choice.Dismissed), today)
	accepted := recomputeAccepted(al.Accepted, cards, notRotated(choice.Accepted), today)
	inD := map[string]bool{}
	for _, d := range dismissed {
		inD[d.ID] = true
	}
	var acc []config.AllowEntryAccepted
	for _, a := range accepted {
		if !inD[a.ID] {
			acc = append(acc, a)
		}
	}
	rotated := recomputeRotated(al.RotatedCredentials, cards, choice.Rotated, today)
	return writeFindingLists(cfgPath, reportDir, stPath, dismissed, acc, &rotated)
}

// recomputeRotated is the new allow_list.rotated_credentials after the person
// saved (BV-314): the credentials of the key cards they left marked «Ya la he
// cambiado», plus the entries of credentials no card shows in this review
// (kept, whoever wrote them: the window, an agent or the person by hand). A
// newly marked credential takes today's date, the day the person marks it;
// one already in the list keeps its date and its note. Sorted by id.
func recomputeRotated(old []config.AllowEntryRotated, cards []report.FindingCard, chosen []string, today string) []config.AllowEntryRotated {
	oldByID := make(map[string]config.AllowEntryRotated, len(old))
	for _, e := range old {
		oldByID[e.ID] = e
	}
	visible := map[string]bool{}
	for _, c := range cards {
		if c.CredID != "" {
			visible[c.CredID] = true
		}
	}
	chosenSet := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		chosenSet[id] = true
	}
	var out []config.AllowEntryRotated
	seen := map[string]bool{}
	for _, e := range old {
		if !visible[e.ID] && !seen[e.ID] {
			out = append(out, e)
			seen[e.ID] = true
		}
	}
	for _, c := range cards {
		if c.CredID == "" || !chosenSet[c.ID] || seen[c.CredID] {
			continue
		}
		seen[c.CredID] = true
		if e, ok := oldByID[c.CredID]; ok {
			out = append(out, e)
		} else {
			out = append(out, config.AllowEntryRotated{ID: c.CredID, Date: today, Note: ""})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// consoleFindingLine is one numbered line of the console list (BV-184 point 5):
// «  N. [<pill>] <title> — <sub> · <agent>», with the accepted (K2) or the
// dismissed (K3) mark appended for a finding already in one of the lists.
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
	switch c.Group {
	case report.CardGroupAccepted:
		line += " (" + cat.Text("findings.console.accepted", nil) + ")" // K2
	case report.CardGroupDismissed:
		line += " (" + cat.Text("findings.console.dismissed", nil) + ")" // K3
	case report.CardGroupRotated:
		line += " (" + cat.Text("findings.console.rotated", nil) + ")" // BV-314
	}
	return line
}

// applyConsoleToggles applies the numbers the person typed (comma-separated,
// 1-based; BV-236 §5, BV-314): «N» flips finding N between dismissed and
// pending, «Na» between accepted and pending, «Nc» between «Ya la he cambiado»
// and pending — only on a key saved in git; on any other finding «Nc» changes
// nothing and N is returned in na, for findings.console.rotated.na. A finding
// is in at most one list, so «N» on an accepted finding dismisses it and «Na»
// on a dismissed one accepts it. A finding starts in the state of its group.
// An empty line, or one with no valid token, changes nothing and is not saved.
func applyConsoleToggles(cards []report.FindingCard, answer string) (choice findingChoice, saved bool, na []int) {
	const (
		pending = iota
		dismissed
		accepted
		rotated
	)
	st := make([]int, len(cards))
	for i, c := range cards {
		switch c.Group {
		case report.CardGroupDismissed:
			st[i] = dismissed
		case report.CardGroupAccepted:
			st[i] = accepted
		case report.CardGroupRotated:
			st[i] = rotated
		}
	}
	changed := false
	for _, tok := range strings.Split(answer, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		want := dismissed
		if t, ok := strings.CutSuffix(tok, "a"); ok {
			tok, want = t, accepted
		} else if t, ok := strings.CutSuffix(tok, "c"); ok {
			tok, want = t, rotated
		}
		n, err := strconv.Atoi(strings.TrimSpace(tok))
		if err != nil || n < 1 || n > len(cards) {
			continue
		}
		if want == rotated && cards[n-1].CredID == "" {
			na = append(na, n)
			continue
		}
		if st[n-1] == want {
			st[n-1] = pending
		} else {
			st[n-1] = want
		}
		changed = true
	}
	if !changed {
		return findingChoice{}, false, na
	}
	for i, c := range cards {
		switch st[i] {
		case dismissed:
			choice.Dismissed = append(choice.Dismissed, c.ID)
		case accepted:
			choice.Accepted = append(choice.Accepted, c.ID)
		case rotated:
			choice.Rotated = append(choice.Rotated, c.ID)
		}
	}
	return choice, true, na
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

// recomputeAccepted is the new allow_list.accepted after the person saved,
// with the same rule as recomputeDismissed: the ids they left marked «Lo
// asumo» among the findings shown, plus the accepted ids that did not appear
// in this review. A newly accepted id takes today's date; one already accepted
// keeps its date. Sorted by id.
func recomputeAccepted(old []config.AllowEntryAccepted, cards []report.FindingCard, chosen []string, today string) []config.AllowEntryAccepted {
	d := make([]config.AllowEntryDismissed, 0, len(old))
	for _, e := range old {
		d = append(d, config.AllowEntryDismissed(e))
	}
	var out []config.AllowEntryAccepted
	for _, e := range recomputeDismissed(d, cards, chosen, today) {
		out = append(out, config.AllowEntryAccepted(e))
	}
	return out
}

// writeDismissed writes allow_list.dismissed into config.json (BV-184 point 2)
// through writeFindingLists. An id it dismisses leaves allow_list.accepted in
// the same write (BV-235: an id is never in both lists).
func writeDismissed(cfgPath, reportDir, stPath string, list []config.AllowEntryDismissed) error {
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	in := map[string]bool{}
	for _, d := range list {
		in[d.ID] = true
	}
	var accepted []config.AllowEntryAccepted
	for _, a := range cfg.AllowList.Accepted {
		if !in[a.ID] {
			accepted = append(accepted, a)
		}
	}
	return writeFindingLists(cfgPath, reportDir, stPath, list, accepted, nil)
}

// writeAccepted writes allow_list.accepted («Lo asumo», BV-235 R3) into
// config.json through writeFindingLists, the same path and the same file as
// writeDismissed. An id it accepts leaves allow_list.dismissed in the same
// write.
func writeAccepted(cfgPath, reportDir, stPath string, list []config.AllowEntryAccepted) error {
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	in := map[string]bool{}
	for _, a := range list {
		in[a.ID] = true
	}
	var dismissed []config.AllowEntryDismissed
	for _, d := range cfg.AllowList.Dismissed {
		if !in[d.ID] {
			dismissed = append(dismissed, d)
		}
	}
	return writeFindingLists(cfgPath, reportDir, stPath, dismissed, list, nil)
}

// writeFindingLists writes allow_list.dismissed and allow_list.accepted into
// config.json and re-points the integrity reference, so the next review does
// not ask about the change (BV-184 point 2): config.json atomically (a temp
// file in the same directory, then a rename), its new SHA-256 into
// state.integrity.config_sha256, the accepted copy in data/ rewritten, and the
// state saved. With rotated not nil it also writes
// allow_list.rotated_credentials (BV-314: a key saved in git marked as
// changed, invariant 2); nil leaves that list as it is. Only those lists are
// touched, each absent when empty; every other key of the file is kept as
// written. An id in both dismissed and accepted is refused.
func writeFindingLists(cfgPath, reportDir, stPath string, list []config.AllowEntryDismissed, accepted []config.AllowEntryAccepted, rotated *[]config.AllowEntryRotated) error {
	both := map[string]bool{}
	for _, d := range list {
		both[d.ID] = true
	}
	for _, a := range accepted {
		if both[a.ID] {
			return fmt.Errorf("finding %s cannot be both dismissed and accepted", a.ID)
		}
	}
	// CTO-49 v2 (invariant 2): never write a configuration outside
	// ~/.rosmarops/config.json.
	if outside, err := dismissConfigOutside(cfgPath); outside {
		if err != nil {
			return fmt.Errorf("resolve RosmarOps's configuration: %w", err)
		}
		return fmt.Errorf("configuration %s is outside RosmarOps's folder: not written", cfgPath)
	}
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
	if len(accepted) == 0 {
		delete(al, "accepted")
	} else {
		arr := make([]any, 0, len(accepted))
		for _, e := range accepted {
			arr = append(arr, map[string]any{"id": e.ID, "date": e.Date})
		}
		al["accepted"] = arr
	}
	if rotated != nil {
		if len(*rotated) == 0 {
			delete(al, config.ListRotatedCredentials)
		} else {
			arr := make([]any, 0, len(*rotated))
			for _, e := range *rotated {
				arr = append(arr, map[string]any{"id": e.ID, "date": e.Date, "note": e.Note})
			}
			al[config.ListRotatedCredentials] = arr
		}
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

// dismissConfigOutside reports whether cfgPath is not ~/.rosmarops/config.json
// (config.HomeConfigPath), the only configuration the dismiss flow may write
// (CTO-49 v2, invariant 2). If the home path cannot be resolved, it reports
// outside, so nothing is written.
func dismissConfigOutside(cfgPath string) (bool, error) {
	home, err := config.HomeConfigPath()
	if err != nil {
		return true, err
	}
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return true, err
	}
	return filepath.Clean(abs) != filepath.Clean(home), nil
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
