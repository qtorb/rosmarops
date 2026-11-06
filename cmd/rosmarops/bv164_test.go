package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-164 point 1/6: the connect offer no longer shows a number — connect.number
// and connect.wait are gone from the catalog (so neither the console nor the
// window can render them) and from the key lists.
func TestBV164_NoNumberKeys(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"connect.number", "connect.wait"} {
			if cat.HasOwn(k) {
				t.Errorf("%s: %s should have been removed from the catalog", lang, k)
			}
		}
	}
	for _, k := range report.CatalogKeys() {
		if k == "connect.number" || k == "connect.wait" {
			t.Errorf("CatalogKeys still lists %s", k)
		}
	}
}

// BV-164 point 2/6: the connect offer's main button (connect.yes) now says
// «Guardar en mi espacio» / «Save to my space».
func TestBV164_OfferButtonText(t *testing.T) {
	for lang, want := range map[string]string{"en": "Save to my space", "es": "Guardar en mi espacio"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		if got := cat.Text("connect.yes", nil); got != want {
			t.Errorf("%s: connect.yes = %q, want %q", lang, got, want)
		}
	}
}

// BV-164 point 4/6: report.space_next is the new text in EN and ES.
func TestBV164_SpaceNextText(t *testing.T) {
	want := map[string]string{
		"es": "Al terminar la próxima revisión, RosmarOps te ofrecerá guardarla en tu espacio. Si aún no tienes cuenta, la creas en ese momento con tu correo y una contraseña.",
		"en": "When the next check finishes, RosmarOps will offer to save it to your space. If you don't have an account yet, you create it then with your email and a password.",
	}
	for lang, w := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		if got := cat.Text("report.space_next", nil); got != w {
			t.Errorf("%s: report.space_next = %q, want %q", lang, got, w)
		}
	}
}

// BV-164 point 3/6: a 409 key_mismatch on send forgets the space credential
// (like disconnect) and clears the 7-day decline, so the connect offer is due
// again in this same review.
func TestBV164_KeyMismatch_ForgetsAndReoffers(t *testing.T) {
	paths, sec := connectedSpace(t)
	// A recent decline that, without the fix, would suppress the offer.
	st, err := state.LoadState(paths.state)
	if err != nil {
		t.Fatal(err)
	}
	st.ConnectOfferDeclinedAt = time.Now().UTC().Format(time.RFC3339)
	if err := state.SaveState(paths.state, st); err != nil {
		t.Fatal(err)
	}
	cur := writePkg(t, paths.reports, "2026-11-01", sec)
	stubSend(t, 409, nil)

	spaceSend(context.Background(), paths, cur, time.Now())

	st, err = state.LoadState(paths.state)
	if err != nil {
		t.Fatal(err)
	}
	if st.SpacePermission {
		t.Error("permission not forgotten after 409 key_mismatch")
	}
	if st.ConnectOfferDeclinedAt != "" {
		t.Errorf("the 7-day decline was not cleared: %q", st.ConnectOfferDeclinedAt)
	}
	if !connectOfferDue(st.ConnectOfferDeclinedAt, time.Now()) {
		t.Error("the connect offer is not due in this same review after a 409")
	}
	stored, err := kit.Load(paths.store)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SpaceCredential) != 0 {
		t.Error("credential not forgotten after 409 key_mismatch")
	}
	if !strings.Contains(readLog(t, paths.reports), sendRejected) {
		t.Error("409 not logged as rejected")
	}
}
