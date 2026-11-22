package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/state"
)

var bv248Now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func bv248Ask(t *testing.T, input string, st *state.State, dc declaredContext) (string, bool) {
	t.Helper()
	cat, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	changed := askDeclared(strings.NewReader(input), &out, cat, st, dc, bv248Now)
	return out.String(), changed
}

// Cursor's questions: a number is its option, 0 «I don't know», Enter no
// answer (not stored), anything else asks again; C2 only in Allowlist or
// Auto-review.
func TestAskDeclared_Cursor(t *testing.T) {
	var st state.State
	out, changed := bv248Ask(t, "x\n4\n\n0\n2\n2\n", &st, declaredContext{cursor: true, grokBot: declared.GrokBotFacts{Present: true, LocalRead: true, AutoReviewRead: true}})
	if !changed {
		t.Fatal("nothing stored")
	}
	got := st.Declared[declared.Cursor].Answers
	want := map[string]string{"c1_run_mode": "auto_review_sandbox", "c3_read_access": "unknown", "c4_external_file_protection": "off"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["c2_allowlist"]; ok {
		t.Errorf("Enter stored an answer: %v", got)
	}
	if strings.Count(out, "En Cursor, Settings > Agents > Approvals & Execution: ¿qué Run mode tienes?") != 2 {
		t.Errorf("an invalid answer did not ask again:\n%s", out)
	}
	if st.Declared[declared.Cursor].KnowledgeVersion != knowledge.CursorVersion || st.Declared[declared.Cursor].AnsweredAt != "2026-11-16T10:00:00Z" {
		t.Errorf("stored %+v", st.Declared[declared.Cursor])
	}
}

// Grok Bot: B0 «No» stops (no other question), and is shown next time with
// «¿siguen igual?»; Enter keeps it, «c» asks again.
func TestAskDeclared_GrokBotNoAndAgain(t *testing.T) {
	var st state.State
	out, _ := bv248Ask(t, "2\n", &st, declaredContext{})
	if st.Declared[declared.GrokBot].Answers["b0_uses"] != "no" || len(st.Declared[declared.GrokBot].Answers) != 1 {
		t.Fatalf("stored %+v", st.Declared)
	}
	if strings.Contains(out, "Execution on this computer") {
		t.Errorf("B0 «No» asked more:\n%s", out)
	}
	out, changed := bv248Ask(t, "\n", &st, declaredContext{})
	if changed || !strings.Contains(out, "Tus respuestas sobre Grok Bot, del 16 ") || !strings.Contains(out, ": No. ¿Siguen igual?") {
		t.Errorf("again: changed %v\n%s", changed, out)
	}
	out, changed = bv248Ask(t, "c\n1\n2\n1\n1\n2\n", &st, declaredContext{})
	got := st.Declared[declared.GrokBot].Answers
	if !changed || got["b0_uses"] != "yes" || got["b1_local_execution"] != "always_allow" || got["b4_send_tools"] != "no" {
		t.Errorf("changed %v %v\n%s", changed, got, out)
	}
}

// A detected app does not ask B0; a measured B1 and B2 are not asked.
func TestAskDeclared_GrokBotMeasured(t *testing.T) {
	var st state.State
	out, _ := bv248Ask(t, "1\n2\n", &st, declaredContext{grokBot: declared.GrokBotFacts{Present: true, LocalRead: true, Local: "ask_every_time", AutoReviewRead: true, AutoReview: "on"}})
	for _, q := range []string{"¿Usas Grok Bot", "Execution on this computer", "Auto-review encendido"} {
		if strings.Contains(out, q) {
			t.Errorf("asked %q:\n%s", q, out)
		}
	}
	got := st.Declared[declared.GrokBot].Answers
	if got["b3_ask_first"] != "all_four" || got["b4_send_tools"] != "no" || len(got) != 2 {
		t.Errorf("stored %v", got)
	}
	if !strings.Contains(out, "Contesta 2 preguntas") {
		t.Errorf("entry count:\n%s", out)
	}
}

// Every literal of the brief, ES and EN, is the catalog's text.
func TestBV248_Literals(t *testing.T) {
	lit := map[string][2]string{
		"decl.entry":                            {"RosmarOps can't read some of {agent}'s settings: they live inside the app. Answer {n} questions to sharpen its score. What you answer shows in the report as «declared by you». If you press Enter without typing anything, the least controlled case is assumed.", "RosmarOps no puede leer algunos ajustes de {agent}: viven dentro de la aplicación. Contesta {n} preguntas para afinar su nota. Lo que contestes sale en el informe como «declarado por ti». Si pulsas Enter sin escribir nada, se supone el caso que menos controla."},
		"decl.unknown":                          {"I don't know", "No lo sé"},
		"decl.c1_run_mode.q":                    {"In Cursor, Settings > Agents > Approvals & Execution: which Run mode do you have?", "En Cursor, Settings > Agents > Approvals & Execution: ¿qué Run mode tienes?"},
		"decl.c2_allowlist.q":                   {"Which commands are on your list on that screen? Pick the most open one you have.", "¿Qué órdenes hay en tu lista de esa pantalla? Elige la más abierta que tengas."},
		"decl.c2_allowlist.empty":               {"The list is empty", "La lista está vacía"},
		"decl.c2_allowlist.no_reads":            {"Only commands that don't read files or go online (for example, git status or npm test)", "Solo órdenes que no leen ficheros ni salen a internet (por ejemplo, git status o npm test)"},
		"decl.c2_allowlist.reads_files":         {"A command that reads files (for example, cat or type)", "Alguna orden que lee ficheros (por ejemplo, cat o type)"},
		"decl.c2_allowlist.network":             {"A command that goes online (for example, curl)", "Alguna orden que sale a internet (por ejemplo, curl)"},
		"decl.c2_allowlist.runs_code":           {"A command that runs code or installs (for example, node, python or npm install)", "Alguna orden que ejecuta código o instala (por ejemplo, node, python o npm install)"},
		"decl.c3_read_access.q":                 {"On the same screen, under «Read Access», what do you have?", "En la misma pantalla, en «Read Access», ¿qué tienes?"},
		"decl.c3_read_access.system":            {"System (the whole computer)", "System (todo el ordenador)"},
		"decl.c4_external_file_protection.q":    {"Is «External-File Protection» on?", "¿Tienes activada «External-File Protection»?"},
		"decl.b0_uses.q":                        {"Do you use Grok Bot on this computer?", "¿Usas Grok Bot en este ordenador?"},
		"decl.b1_local_execution.q":             {"In the Grok app, Settings > Computer > Your computer: what do you have under «Execution on this computer»?", "En la app de Grok, Settings > Computer > Your computer: ¿qué tienes en «Execution on this computer»?"},
		"decl.b2_auto_review.q":                 {"In Settings > General > Bot, is Auto-review on?", "En Settings > General > Bot, ¿tienes Auto-review encendido?"},
		"decl.b3_ask_first.q":                   {"Do you have «Ask first» rules for sending, publishing, deleting and buying?", "¿Tienes reglas «Ask first» para enviar, publicar, borrar y comprar?"},
		"decl.b3_ask_first.all_four":            {"For all four", "Para las cuatro"},
		"decl.b3_ask_first.some":                {"For some", "Para algunas"},
		"decl.b3_ask_first.none":                {"None", "Ninguna"},
		"decl.b4_send_tools.q":                  {"Do you have tools connected that send or publish, such as email, messaging or shared documents?", "¿Tienes conectadas herramientas que envían o publican, como el correo, la mensajería o documentos compartidos?"},
		"decl.again":                            {"Your answers about {agent}, from {date}: {summary}. Still the same? Press Enter to continue, or type «c» to change them.", "Tus respuestas sobre {agent}, del {date}: {summary}. ¿Siguen igual? Pulsa Enter para seguir, o escribe «c» para cambiarlas."},
		"unknown.declared_by_you":               {"Declared by you on {date}: RosmarOps can't read this setting, it lives inside the app.", "Declarado por ti el {date}: RosmarOps no puede leer este ajuste, vive dentro de la aplicación."},
		"unknown.not_declared_least_controlled": {"No answer: the least controlled case is assumed. Answer the questions in your next review with a console.", "Sin respuesta: se supone el caso que menos controla. Contesta las preguntas en tu próxima revisión con consola."},
		"decl.line":                             {"Your answers ({date}): {summary}.", "Tus respuestas ({date}): {summary}."},
		"decl.c1_run_mode.auto_review_sandbox":  {"Auto-review (with Sandbox)", "Auto-review (with Sandbox)"},
		"decl.b1_local_execution.never_allow":   {"Never allow", "Never allow"},
		"decl.c4_external_file_protection.on":   {"Yes", "Sí"},
	}
	for i, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range lit {
			if got := cat.Text(k, nil); got != v[i] {
				t.Errorf("%s %s = %q, want %q", lang, k, got, v[i])
			}
		}
	}
}
