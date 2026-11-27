package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-236 §5 «Lo asumo» in the console and in the window's shared core: «N»
// dismisses or restores, «Na» accepts or makes pending again, a finding is in
// at most one list, and both lists are written through writeFindingLists.

func bv236Cards() []report.FindingCard {
	return []report.FindingCard{
		{ID: "f_new", Severity: "red", Title: "A", New: true, Group: report.CardGroupNew},
		{ID: "f_rest", Severity: "yellow", Title: "B", Since: "1 Nov", Group: report.CardGroupRest},
		{ID: "f_acc", Severity: "yellow", Title: "C", Since: "2 Nov", Group: report.CardGroupAccepted},
		{ID: "f_dis", Severity: "red", Title: "D", Since: "3 Nov", Group: report.CardGroupDismissed},
	}
}

func TestBV236_ConsoleToggles(t *testing.T) {
	cards := bv236Cards()
	for _, tc := range []struct {
		answer              string
		dismissed, accepted []string
	}{
		// «1» dismisses the pending f_new; «2a» accepts f_rest; the others keep
		// their state.
		{"1, 2a\n", []string{"f_new", "f_dis"}, []string{"f_rest", "f_acc"}},
		// «3a» on an accepted finding makes it pending; «4» on a dismissed one
		// restores it.
		{"3a,4\n", nil, nil},
		// «3» on an accepted finding dismisses it (it leaves the other list);
		// «4a» on a dismissed one accepts it. Upper case works too.
		{"3, 4A\n", []string{"f_acc"}, []string{"f_dis"}},
		// Typing «1a» twice toggles it back: pending.
		{"1a, 1a\n", []string{"f_dis"}, []string{"f_acc"}},
	} {
		ch, saved, _ := applyConsoleToggles(cards, tc.answer)
		if !saved {
			t.Errorf("%q: not saved", tc.answer)
			continue
		}
		if !reflect.DeepEqual(ch.Dismissed, tc.dismissed) || !reflect.DeepEqual(ch.Accepted, tc.accepted) {
			t.Errorf("%q: dismissed %v accepted %v, want %v %v", tc.answer, ch.Dismissed, ch.Accepted, tc.dismissed, tc.accepted)
		}
	}
	for _, junk := range []string{"\n", "9a, x, a\n", "0\n"} {
		if _, saved, _ := applyConsoleToggles(cards, junk); saved {
			t.Errorf("%q counted as a change", junk)
		}
	}
}

// TestBV236_ConsoleTexts: the prompt is K1 and the marks are K2 and K3, in EN
// and ES, from the catalog.
func TestBV236_ConsoleTexts(t *testing.T) {
	want := map[string][3]string{
		// BV-314: K1 names «3c».
		"en": {"Type the numbers you want to change, separated by commas: «3» dismisses or restores finding 3; «3a» accepts it or makes it pending again; «3c» marks the key of finding 3 as changed, or undoes it (Enter to change nothing): ", "(accepted by you)", "(dismissed by you)"},
		"es": {"Escribe los números que quieras cambiar, separados por comas: «3» descarta o recupera el aviso 3; «3a» lo asume o lo deja pendiente; «3c» marca como cambiada la clave del aviso 3, o lo deshace (Enter para no cambiar nada): ", "(asumido por ti)", "(descartado por ti)"},
	}
	for lang, w := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		if got := cat.Text("findings.console.prompt.accept", nil); got != w[0] {
			t.Errorf("%s K1 = %q", lang, got)
		}
		cards := bv236Cards()
		if l := consoleFindingLine(3, cards[2], cat); !strings.HasSuffix(l, " "+w[1]) {
			t.Errorf("%s accepted line %q lacks K2 %q", lang, l, w[1])
		}
		if l := consoleFindingLine(4, cards[3], cat); !strings.HasSuffix(l, " "+w[2]) {
			t.Errorf("%s dismissed line %q lacks K3 %q", lang, l, w[2])
		}
		for _, c := range cards[:2] {
			if l := consoleFindingLine(1, c, cat); strings.HasSuffix(l, ")") {
				t.Errorf("%s pending line %q carries a mark", lang, l)
			}
		}
	}
}

// TestBV236_SaveWritesBothLists: what the person chose is written into
// config.json, both lists, through writeFindingLists: a new choice takes
// today's date, a kept one its own, an id moved from one list to the other
// leaves the first, and entries of findings not shown this run are kept.
func TestBV236_SaveWritesBothLists(t *testing.T) {
	cfgPath, stPath, reports, _ := bv235Config(t) // dismissed: f_aaaaaaaaaaaa (2026-11-10), cred_bbbbbbbbbbbb (2026-11-12)
	al := config.AllowList{
		Dismissed: []config.AllowEntryDismissed{{ID: "f_aaaaaaaaaaaa", Date: "2026-11-10"}, {ID: "cred_bbbbbbbbbbbb", Date: "2026-11-12"}},
	}
	cards := []report.FindingCard{
		{ID: "cred_bbbbbbbbbbbb", Group: report.CardGroupDismissed},
		{ID: "f_cccccccccccc", Group: report.CardGroupRest},
		{ID: "f_dddddddddddd", Group: report.CardGroupNew},
	}
	// The person accepts cred_bbbb (it leaves «dismissed») and f_cccc, and
	// dismisses f_dddd. f_aaaa was not shown: it stays dismissed.
	ch, saved, _ := applyConsoleToggles(cards, "1a, 2a, 3\n")
	if !saved {
		t.Fatal("not saved")
	}
	if err := saveFindingChoice(cfgPath, reports, stPath, al, cards, ch, "2026-11-15"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	wantD := []config.AllowEntryDismissed{{ID: "f_aaaaaaaaaaaa", Date: "2026-11-10"}, {ID: "f_dddddddddddd", Date: "2026-11-15"}}
	wantA := []config.AllowEntryAccepted{{ID: "cred_bbbbbbbbbbbb", Date: "2026-11-15"}, {ID: "f_cccccccccccc", Date: "2026-11-15"}}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, wantD) || !reflect.DeepEqual(cfg.AllowList.Accepted, wantA) {
		t.Fatalf("dismissed %v accepted %v, want %v %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted, wantD, wantA)
	}
	// Saving again with f_cccc left accepted keeps its date; making it
	// pending removes it.
	cards2 := []report.FindingCard{
		{ID: "cred_bbbbbbbbbbbb", Group: report.CardGroupAccepted},
		{ID: "f_cccccccccccc", Group: report.CardGroupAccepted},
	}
	ch, _, _ = applyConsoleToggles(cards2, "2a\n")
	if err := saveFindingChoice(cfgPath, reports, stPath, cfg.AllowList, cards2, ch, "2026-11-20"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.LoadConfig(cfgPath)
	if want := []config.AllowEntryAccepted{{ID: "cred_bbbbbbbbbbbb", Date: "2026-11-15"}}; !reflect.DeepEqual(cfg.AllowList.Accepted, want) {
		t.Errorf("accepted after the second save %v, want %v", cfg.AllowList.Accepted, want)
	}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, wantD) {
		t.Errorf("dismissed after the second save %v, want %v", cfg.AllowList.Dismissed, wantD)
	}
}

// TestBV236_OfferWritesAccepted: the flow after a review writes «Lo asumo»
// from the UI's answer.
func TestBV236_OfferWritesAccepted(t *testing.T) {
	cfgPath, stPath, _ := bv184CmdSetup(t)
	var id string
	prev := dismissUIHook
	dismissUIHook = func(cards []report.FindingCard, _ catalog.Catalog) (findingChoice, bool) {
		id = cards[0].ID
		return findingChoice{Accepted: []string{id}}, true
	}
	defer func() { dismissUIHook = prev }()
	offerDismissAfterReport("es", cfgPath, stPath, false, time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local))
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := []config.AllowEntryAccepted{{ID: id, Date: "2026-11-15"}}; !reflect.DeepEqual(cfg.AllowList.Accepted, want) || len(cfg.AllowList.Dismissed) != 0 {
		t.Errorf("accepted %v dismissed %v, want %v", cfg.AllowList.Accepted, cfg.AllowList.Dismissed, want)
	}
}
