package report_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// The first screen's texts are the signed ones, literally (design of the
// report in three blocks, §5, signed 18-11-2026; the counts in pieces with
// their form for one, signed 19-11-2026), in Spanish and English.
var r3Texts = []struct{ key, es, en string }{
	{"html.r3.title", "Informe RosmarOps", "RosmarOps report"},
	{"html.r3.sub", "Qué pueden hacer tus agentes de IA sin preguntarte en este ordenador, y qué cambiar.", "What your AI agents can do without asking you on this computer, and what to change."},
	{"html.r3.meta", "{date} · {os} · {agents} · {projects} · RosmarOps {version}", "{date} · {os} · {agents} · {projects} · RosmarOps {version}"},
	{"html.r3.agents", "{n} agentes", "{n} agents"},
	{"html.r3.agents.one", "1 agente", "1 agent"},
	{"html.r3.projects", "{n} proyectos", "{n} projects"},
	{"html.r3.projects.one", "1 proyecto", "1 project"},
	{"html.r3.h1", "Resultado", "Result"},
	{"html.r3.h2", "Avisos", "Findings"},
	{"html.r3.h3", "Para que no vuelva a pasar", "So it doesn't happen again"},
	{"html.r3.result", "Nota de {agent}, tu agente con menos control: {class_phrase} (clase {class}{provisional}). Con los ajustes de fábrica sacaría {reference}.", "Score of {agent}, your agent with the least control: {class_phrase} (class {class}{provisional}). With factory settings it would get {reference}."},
	{"html.r3.provisional", ", provisional", ", provisional"},
	{"html.r3.counts", "{groups} · {findings} · {urgent} · {review}", "{groups} · {findings} · {urgent} · {review}"},
	{"html.r3.groups", "{n} problemas", "{n} problems"},
	{"html.r3.groups.one", "1 problema", "1 problem"},
	{"html.r3.findings", "{n} avisos", "{n} findings"},
	{"html.r3.findings.one", "1 aviso", "1 finding"},
	{"html.r3.urgent", "{n} urgentes", "{n} urgent"},
	{"html.r3.urgent.one", "1 urgente", "1 urgent"},
	{"html.r3.review", "{n} para revisar", "{n} to review"},
	{"html.r3.col.n", "Nº", "No."},
	{"html.r3.col.problem", "Problema", "Problem"},
	{"html.r3.col.count", "Avisos", "Findings"},
	{"html.r3.col.level", "Nivel", "Level"},
	{"html.r3.col.fix", "Cómo se arregla", "How to fix it"},
	{"html.r3.group.cred.one", "Tu {service} está al alcance de 1 agente.", "Your {service} is within reach of 1 agent."},
	{"html.r3.group.cred.other", "Tu {service} está al alcance de {n} agentes.", "Your {service} is within reach of {n} agents."},
	{"html.r3.group.cred.project", "Tu proyecto de {service} está al alcance de {n} agentes.", "Your {service} project is within reach of {n} agents."},
	{"html.r3.group.cred.project.one", "Tu proyecto de {service} está al alcance de 1 agente.", "Your {service} project is within reach of 1 agent."},
	{"html.r3.group.cred.detail", "Pueden leer sus claves sin preguntarte: {agents}.", "These agents can read its keys without asking you: {agents}."},
	{"html.r3.group.cred.git", "Una de ellas está, además, en el historial de git de {projects} proyectos.", "One of them is also in the git history of {projects} projects."},
	{"html.r3.group.cred.git.one", "Una de ellas está, además, en el historial de git de 1 proyecto.", "One of them is also in the git history of 1 project."},
	{"html.r3.group.config", "{agent} puede cambiar la configuración de otros agentes:", "{agent} can change the settings of other agents:"},
	{"html.r3.group.config.detail", "{targets}.", "{targets}."},
	{"html.r3.fix.rotate", "Cambiar la clave en {service} ›", "Change the key in {service} ›"},
	{"html.r3.fix.rotate_tpl", "Cambia la clave y aplica la plantilla ›", "Change the key and apply the template ›"},
	{"html.r3.fix.tpl_all", "Con una plantilla (bloque 3)", "With a template (block 3)"},
	{"html.r3.fix.tpl_some", "La plantilla arregla {k} de {n}; quedan {rest}", "The template fixes {k} of {n}; {rest} remain"},
	{"html.r3.fix.steps", "Ver los pasos ›", "See the steps ›"},
	{"html.r3.more", "Los otros {groups} ({findings}) ›", "The other {groups} ({findings}) ›"},
	{"html.r3.more.one", "El otro problema ({findings}) ›", "The other problem ({findings}) ›"},
	{"html.card.counts.one", "1 urgente, {yellow} para revisar", "{red} urgent, {yellow} to review"},
	{"ai.counts.total.one", "En total, 1 urgente y {yellow} para revisar; algunos afectan a más de un agente.", "In total, {red} urgent and {yellow} to review; some affect more than one agent."},
	{"html.r3.tpl.body", "Elige una plantilla: contestas una pregunta («¿Para qué usas tus agentes?») y te damos un texto para pegar en tu agente. Te enseña el cambio antes de guardarlo.", "Choose a template: you answer one question («What do you use your agents for?») and we give you a text to paste into your agent. It shows you the change before saving it."},
	{"html.r3.tpl.fixes", "Arregla {k} de tus avisos urgentes (grupos {groups}).", "It fixes {k} of your urgent findings (groups {groups})."},
	{"html.r3.tpl.fixes_none", "Deja a {agent} con menos permisos desde hoy.", "It leaves {agent} with fewer permissions from today."},
	{"html.r3.tpl.button", "Elegir una plantilla", "Choose a template"},
	{"html.r3.tpl.note", "Unos 2 minutos. RosmarOps no cambia nada.", "About 2 minutes. RosmarOps changes nothing."},
	{"html.r3.how", "Cómo se calcula la nota ›", "How the score is worked out ›"},
	{"html.r3.assumptions", "Supuestos del análisis ›", "Analysis assumptions ›"},
	{"html.r3.footer", "Generado en tu ordenador. Solo lectura: no ha cambiado nada.", "Generated on your computer. Read only: nothing was changed."},
	{"html.r3.method", "Metodología {method}", "Methodology {method}"},
}

func TestR3Texts(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	en, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r3Texts {
		if !es.HasOwn(c.key) {
			t.Errorf("%s: no Spanish text", c.key)
		}
		if got := es.Text(c.key, nil); got != c.es {
			t.Errorf("%s es: %q, want %q", c.key, got, c.es)
		}
		if got := en.Text(c.key, nil); got != c.en {
			t.Errorf("%s en: %q, want %q", c.key, got, c.en)
		}
	}
}
