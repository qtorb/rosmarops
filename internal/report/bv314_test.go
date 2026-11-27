package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-314 («Ya la he cambiado»): the finding cards of a key saved in git carry
// its credential; the report's step sends the person to `rosmarops findings`
// and folds the agent and by-hand ways; the Markdown says it in Spanish; and a
// credential in allow_list.rotated_credentials gives no cred_in_git.

// bv314Texts are the signed texts (docs/research/2026-11-27-clave-en-git-ya-la-he-cambiado.md).
var bv314Texts = []struct{ key, es, en string }{
	{"findings.rotated", "Ya la he cambiado", "I have changed it"},
	{"findings.when.rotated", "Ya la he cambiado: si has creado una clave nueva en el proveedor y has revocado esta. RosmarOps no puede comprobarlo: queda como declarado por ti.", "I have changed it: if you have created a new key at the provider and revoked this one. RosmarOps cannot check it: it is recorded as declared by you."},
	{"findings.when.cred_in_git", "Si es una clave de prueba o de un proyecto que nunca se publicó, y no abre nada real.", "If it is a test key or from a project that was never pushed, and it opens nothing real."},
	{"findings.text.accept", "Lo asumo: sigue contando en tu nota y deja de salir como pendiente. Descartar: deja de contar desde la próxima revisión, para lo que no te afecta. Ya la he cambiado: solo para una clave guardada en git que ya has revocado en su proveedor. Puedes cambiarlo cuando quieras.", "I accept it: it keeps counting in your score and stops showing as pending. Dismiss: it stops counting from the next check, for what doesn't apply to you. I have changed it: only for a key saved in git that you have already revoked at its provider. You can change this at any time."},
	{"findings.group.rotated", "Claves que has cambiado", "Keys you have changed"},
	{"findings.tally.rotated.one", "1 clave cambiada", "1 key changed"},
	{"findings.tally.rotated.other", "{n} claves cambiadas", "{n} keys changed"},
	{"findings.console.rotated", "clave cambiada, declarado por ti", "key changed, declared by you"},
	{"findings.console.rotated.na", "El aviso {n} no es una clave guardada en git: «c» no se aplica.", "Finding {n} is not a key saved in git: «c» does not apply."},
	{"findings.console.prompt.accept", "Escribe los números que quieras cambiar, separados por comas: «3» descarta o recupera el aviso 3; «3a» lo asume o lo deja pendiente; «3c» marca como cambiada la clave del aviso 3, o lo deshace (Enter para no cambiar nada): ", "Type the numbers you want to change, separated by commas: «3» dismisses or restores finding 3; «3a» accepts it or makes it pending again; «3c» marks the key of finding 3 as changed, or undoes it (Enter to change nothing): "},
	{"console.findings_hint", "Para descartar o asumir avisos, o marcar una clave como cambiada, ejecuta: rosmarops findings", "To dismiss or accept findings, or mark a key as changed, run: rosmarops findings"},
	{"html.fix.rotate.step2", "Cuando la hayas revocado, díselo a RosmarOps: abre una terminal, escribe `rosmarops findings` y marca este aviso con «Ya la he cambiado». Marcarlo no revoca la clave. RosmarOps no puede comprobar la revocación: quedará como declarada por ti.", "Once you have revoked it, tell RosmarOps: open a terminal, type `rosmarops findings` and mark this finding with «I have changed it». Marking it does not revoke the key. RosmarOps cannot check the revocation: it will be recorded as declared by you."},
	{"html.fix.rotate.step3", "El aviso dejará de salir desde la próxima revisión.", "The finding will stop appearing from the next check."},
	{"html.fix.rotate.other", "Otras formas de registrarlo", "Other ways to record it"},
	{"html.fix.rotate.other.note", "Si lo registra tu agente o lo haces a mano, en la próxima revisión RosmarOps te preguntará si hiciste tú ese cambio. Confírmalo solo si lo hiciste.", "If your agent records it or you do it by hand, on the next check RosmarOps will ask whether you made that change. Confirm only if you did."},
	{"html.r3.fix.rotate", "Ver cómo cambiar la clave de {service} ›", "See how to change the {service} key ›"},
	{"action.rotate_credential.label", "Cambia esta clave en su proveedor", "Rotate this credential"},
	{"action.rotate_credential.how", "Crea una clave nueva en el proveedor, ponla donde usas la antigua y revoca la antigua. Borrarla del historial no revoca una clave que puede haber salido. Después díselo a RosmarOps: ejecuta `rosmarops findings` y marca este aviso con «Ya la he cambiado».", "Create a new key at the provider, replace it where you use it, and revoke the old one. Deleting it from the history does not revoke a key that may have left. Then tell RosmarOps: run `rosmarops findings` and mark this finding with «I have changed it»."},
}

func TestBV314Texts(t *testing.T) {
	es, _ := catalog.Load("es")
	en, _ := catalog.Load("en")
	for _, x := range bv314Texts {
		if !es.Has(x.key) || es.Text(x.key, nil) != x.es {
			t.Errorf("%s es: %q, want %q", x.key, es.Text(x.key, nil), x.es)
		}
		if en.Text(x.key, nil) != x.en {
			t.Errorf("%s en: %q, want %q", x.key, en.Text(x.key, nil), x.en)
		}
	}
	// The agent's text ends asking for today's date (§5).
	if s := es.Text("html.agent.prompt.rotated", nil); !strings.HasSuffix(s, " Enséñame el cambio antes de guardar. Pon en «date» la fecha de hoy.") {
		t.Errorf("html.agent.prompt.rotated es: %q", s)
	}
	if s := en.Text("html.agent.prompt.rotated", nil); !strings.HasSuffix(s, " Show me the change before saving. Set «date» to today's date.") {
		t.Errorf("html.agent.prompt.rotated en: %q", s)
	}
}

// Only a cred_in_git card offers «Ya la he cambiado»: it carries the
// credential and findings.when.rotated.
func TestBV314Cards(t *testing.T) {
	v := cto7Report(t)
	cat, _ := catalog.Load("es")
	n := 0
	for _, c := range report.FindingCards(v, cat, "/h") {
		var f rules.Finding
		for _, g := range v.Findings {
			if g.ID == c.ID {
				f = g
			}
		}
		if f.Rule == rules.RuleCredInGit {
			n++
			if c.CredID != f.Params.CredID || c.CredID == "" || c.WhenRotated != cat.Text("findings.when.rotated", nil) {
				t.Errorf("cred_in_git card: %+v", c)
			}
		} else if c.CredID != "" || c.WhenRotated != "" {
			t.Errorf("card %s (%s) offers «Ya la he cambiado»", c.ID, f.Rule)
		}
	}
	if n == 0 {
		t.Fatal("no cred_in_git card")
	}
}

// §3: the step's order — the three steps, with `rosmarops findings` in
// <code>; then «Other ways to record it», folded, holding its note, the copy
// button and «I'd rather do it by hand»; then the closing line. The table's
// link says what it does.
func TestBV314Step(t *testing.T) {
	v := cto7Report(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		step2 := strings.ReplaceAll(htmlEscape(cat.Text("html.fix.rotate.step2", nil)), "`rosmarops findings`", "<code>rosmarops findings</code>")
		other := `<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>` + htmlEscape(cat.Text("html.fix.rotate.other", nil)) + `</summary><p>` + htmlEscape(cat.Text("html.fix.rotate.other.note", nil)) + `</p><pre hidden>`
		order := []string{
			`<ol class="rot"><li>` + htmlEscape(cat.Text("html.fix.rotate.step1", nil)) + `</li><li>` + step2 + `</li><li>` + htmlEscape(cat.Text("html.fix.rotate.step3", nil)) + `</li></ol>`,
			other,
			`">` + htmlEscape(cat.Text("html.fix.copy.rotated", nil)) + `</button>`,
			`›</span>` + htmlEscape(cat.Text("html.fix.manual", nil)) + `</summary>`,
			htmlEscape(cat.Text("html.fix.manual.save", nil)) + `</li></ol></details></details>`,
			htmlEscape(cat.Text("html.after.rotate", nil)),
		}
		at := 0
		for i, s := range order {
			j := strings.Index(page[at:], s)
			if j < 0 {
				t.Fatalf("%s: part %d not found after %d: %q", lang, i, at, s)
			}
			at += j + len(s)
		}
		if !strings.Contains(page, htmlEscape(cat.Text("html.r3.fix.rotate", map[string]string{"service": "Stripe"}))) {
			t.Errorf("%s: the table's link is not %q", lang, cat.Text("html.r3.fix.rotate", map[string]string{"service": "Stripe"}))
		}
		if strings.Contains(page, "https://") && !strings.Contains(page, "https://app.rosmarops.com/space") {
			t.Errorf("%s: the page carries an external URL", lang)
		}
	}
}

// §4: the Markdown's rotation in Spanish is Spanish now: label and how.
func TestBV314MarkdownSpanish(t *testing.T) {
	v := cto7Report(t)
	es, _ := catalog.Load("es")
	en, _ := catalog.Load("en")
	md := report.RenderV2Markdown(v, es, time.UTC)
	for _, k := range []string{"action.rotate_credential.label", "action.rotate_credential.how"} {
		if !strings.Contains(md, es.Text(k, nil)) {
			t.Errorf("Markdown es lacks %s %q", k, es.Text(k, nil))
		}
		if strings.Contains(md, en.Text(k, nil)) {
			t.Errorf("Markdown es still carries the English %s", k)
		}
	}
}

// What «Ya la he cambiado» writes hides the key's cred_in_git from the next
// review, and the report says a key declared as changed hid it, as before.
// The rotation's entry is the only change: the other findings stay.
func TestBV314RotatedHidesCredInGit(t *testing.T) {
	before := cto7Report(t)
	credID := ""
	for _, f := range before.Findings {
		if f.Rule == rules.RuleCredInGit {
			credID = f.Params.CredID
		}
	}
	if credID == "" {
		t.Fatal("no cred_in_git")
	}
	after := cto7ReportRotated(t, []config.AllowEntryRotated{{ID: credID, Date: "2026-11-27", Note: ""}})
	for _, f := range after.Findings {
		if f.Rule == rules.RuleCredInGit && f.Params.CredID == credID {
			t.Fatalf("cred_in_git of %s still reported", credID)
		}
	}
	if after.HiddenCount() < 1 {
		t.Errorf("the report does not say the configuration hid it (hidden %d)", after.HiddenCount())
	}
	if len(after.Findings) != len(before.Findings)-1 {
		t.Errorf("findings %d, want %d (only the cred_in_git goes)", len(after.Findings), len(before.Findings)-1)
	}
}
