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
	// BV-294 (signed 24-11 09:22): the detail a person can manage.
	{"html.r3.cfg.agent.count", "{n} avisos", "{n} findings"},
	{"html.r3.cfg.agent.count.one", "1 aviso", "1 finding"},
	{"html.r3.cfg.agent.targets", "Puede cambiar a: {targets}.", "It can change: {targets}."},
	{"html.r3.cfg.many.config", "{agent} puede cambiar la configuración de {n} agentes", "{agent} can change the settings of {n} agents"},
	{"html.r3.cfg.many.instructions", "{agent} puede reescribir las instrucciones de {n} agentes", "{agent} can rewrite the instructions of {n} agents"},
	{"html.r3.cfg.many.program", "{agent} puede cambiar un programa que ejecutan {n} agentes", "{agent} can change a program that {n} agents run"},
	{"html.r3.group.secret.many", "{n} secretos al alcance de {agents} sin preguntarte", "{n} secrets within reach of {agents} without asking you"},
	{"html.r3.group.same", "{title} · {n} veces", "{title} · {n} times"},
	{"html.r3.rest.title", "Los otros {n} problemas", "The other {n} problems"},
	{"html.r3.rest.title.one", "El otro problema", "The other problem"},
	{"html.r3.rest.sub", "Plegados. Ábrelos cuando te toque.", "Folded. Open them when you get to them."},
	// BV-297 (signed 24-11, blocks 153 and 161): titles that say whom they are about.
	{"finding.computer_use_app_allowed.red", "{agent} puede usar {app} en tu escritorio sin preguntarte, que es lo mismo que ejecutar comandos sin preguntarte.", "{agent} can use {app} on your desktop without asking, which is the same as running commands without asking."},
	{"finding.computer_use_app_allowed.red.all", "{agent} puede usar cualquier aplicación de tu escritorio sin preguntarte, que es lo mismo que ejecutar comandos sin preguntarte.", "{agent} can use any app on your desktop without asking, which is the same as running commands without asking."},
	{"finding.computer_use_app_allowed.red.many", "{agent} puede usar {n} aplicaciones de tu escritorio sin preguntarte, que es lo mismo que ejecutar comandos sin preguntarte.", "{agent} can use {n} apps on your desktop without asking, which is the same as running commands without asking."},
	{"finding.computer_use_app_allowed.yellow", "{agent} puede usar {apps} en tu escritorio sin preguntarte, con las sesiones que tienes abiertas.", "{agent} can use {apps} on your desktop without asking, with your signed-in sessions."},
	{"finding.computer_use_app_allowed.yellow.many", "{agent} puede usar {n} aplicaciones de tu escritorio sin preguntarte, con las sesiones que tienes abiertas.", "{agent} can use {n} apps on your desktop without asking, with your signed-in sessions."},
	{"finding.acts_without_you.title", "{agent} puede actuar sin ti: algo de fuera puede despertarlo y puede hacer cosas sin preguntarte.", "{agent} can act without you: something outside can wake it up, and it can do things without asking you."},
	{"html.where.all_apps", "todas las aplicaciones", "all apps"},
	{"html.r3.issue.own_instructions_many", "{n} de tus agentes pueden reescribir sus propias instrucciones: {agents}.", "{n} of your agents can rewrite their own instructions: {agents}."},
	{"html.r3.issue.own_config_many", "{n} de tus agentes pueden cambiar su propia configuración: {agents}.", "{n} of your agents can change their own settings: {agents}."},
	{"html.r3.issue.acts_many", "{n} de tus agentes pueden actuar sin ti: {agents}.", "{n} of your agents can act without you: {agents}."},
	{"html.f.cred_in_git.title", "{cred_name}: en git", "{cred_name} is saved in git"},
	{"html.f.cred_in_git.title.history", "{cred_name}: en tu historial de git", "{cred_name} is in your git history"},
	// BV-292 (signed 24-11 08:51): the families and the single row.
	{"html.family.key", "Clave", "Key"},
	{"html.family.between_agents", "Entre agentes", "Between agents"},
	{"html.family.agent", "Agente", "Agent"},
	{"html.family.mcp", "MCP", "MCP"},
	{"html.family.orchestrator", "Orquestador", "Orchestrator"},
	{"html.family.subagent", "Subagente", "Subagent"},
	{"html.family.project", "Proyecto", "Project"},
	{"html.family.desktop", "Escritorio", "Desktop"},
	{"html.r3.issue.config_many", "{n} de tus agentes pueden cambiar la configuración de otros: {agents}.", "{n} of your agents can change other agents' settings: {agents}."},
	// BV-289 (signed 23-11 18:36; the corrected sentences of §6.1): the
	// verdict, «Start here» and «What it means».
	{"html.r3.verdict.exposed", "Al menos uno de tus agentes puede hacer mucho en este ordenador sin preguntarte.", "At least one of your agents can do a lot on this computer without asking you."},
	{"html.r3.verdict.open_gaps", "Tus agentes te preguntan casi siempre, pero quedan huecos abiertos.", "Your agents ask you before most things, but some gaps are open."},
	{"html.r3.verdict.contained", "En lo que RosmarOps ha revisado, tus agentes te preguntan antes de actuar.", "In what RosmarOps checked, your agents ask you before they act."},
	{"html.r3.start.eyebrow", "Empieza por aquí", "Start here"},
	{"html.r3.start.tpl", "Elige una plantilla. Arregla {k} de tus {n} avisos urgentes. Contestas una pregunta y te damos un texto para pegar en tu agente.", "Choose a template. It fixes {k} of your {n} urgent findings. You answer one question and get a text to paste into your agent."},
	{"html.r3.start.git", "Cambia tu clave de {service}. Está guardada en git, y git la conserva aunque borres el fichero.", "Change your {service} key. It is saved in git, and git keeps it even if you delete the file."},
	{"html.r3.start.steps", "Ver los pasos", "See the steps"},
	{"html.r3.start.first", "Empieza por el problema 1. {title}", "Start with problem 1. {title}"},
	{"html.meaning.computer_use_app_allowed", "Dentro de {app}, {agent} puede hacer clic y escribir como tú: lo que tú puedes hacer ahí, lo puede hacer él.", "Inside {app}, {agent} can click and type as you: whatever you can do there, it can do."},
	{"html.meaning.computer_use_app_allowed.all", "{agent} puede hacer clic y escribir como tú en cualquier aplicación de tu escritorio: lo que tú puedes hacer ahí, lo puede hacer él.", "{agent} can click and type as you in any app on your desktop: whatever you can do there, it can do."},
	{"html.meaning.orchestrator_skips_approvals", "{agent} trabaja entonces sin las preguntas que te haría si lo abrieras tú, así que nadie aprueba cada paso.", "{agent} then works without the questions it would ask if you started it yourself, so nobody approves each step."},
	{"html.meaning.orchestrator_skips_approvals.unknown", "RosmarOps no puede saber si {agent} mantiene sus preguntas cuando lo lanza {orchestrator}. Si no las mantiene, nadie aprueba cada paso.", "RosmarOps cannot tell whether {agent} keeps its questions when {orchestrator} starts it. If it does not, nobody approves each step."},
	{"html.meaning.orchestrator_vulnerable_version", "El fallo es público, así que quien pueda llegar a {orchestrator} sabe cómo aprovecharlo.", "The flaw is public, so anyone who can reach {orchestrator} knows how to use it."},
	{"html.meaning.orchestrator_vulnerable_version.npm_cache", "Esta copia no está en marcha. Importa si {orchestrator} se vuelve a arrancar desde ella.", "This copy is not running. It matters if {orchestrator} is started from it again."},
	{"html.meaning.orchestrator_network_listen", "Cualquiera de la misma red que llegue a él puede intentar dar trabajo a tus agentes.", "Anyone on the same network who reaches it can try to give work to your agents."},
	{"html.meaning.orchestrator_open_signup", "Con una cuenta, podría dar trabajo a los agentes que lanza {orchestrator}.", "With an account, they could give work to the agents {orchestrator} starts."},
	{"html.meaning.subagent_all_tools", "Un subagente pensado para una tarea puede hacer entonces todo lo que puede {agent}, también lo que esa tarea no necesitaba.", "A subagent meant for one task can then do everything {agent} can, including what that task never needed."},
	{"html.meaning.subagent_permissive_mode", "Cuando {agent} le pasa una tarea, actúa sin pararse a preguntarte.", "When {agent} hands it a task, it acts without stopping to ask you."},
	{"html.meaning.subagent_permissive_mode.yellow", "Cuando {agent} le pasa una tarea, hace parte del trabajo sin preguntarte; cuánto, depende del modo.", "When {agent} hands it a task, it does part of the work without asking you; how much depends on the mode."},
	{"html.meaning.subagent_hook_command", "Ese comando se ejecuta en tu ordenador, con tus permisos, cuando el subagente hace lo que ese hook vigila.", "That command runs on your computer, with your permissions, when the subagent does what the hook watches."},
	{"html.meaning.subagent_inline_mcp", "Esos servidores no están en la lista que revisas normalmente, así que es fácil no verlos.", "Those servers are not in the list you normally review, so they are easy to miss."},
	{"html.meaning.mcp_unpinned_launch", "Si algún día ese paquete se sustituye por una versión maliciosa, tu agente la ejecutaría la próxima vez que arranque.", "If that package is ever replaced by a malicious release, your agent would run it the next time it starts."},
	{"html.meaning.mcp_project_autoapprove", "Un proyecto que no has escrito tú puede traer entonces sus propios servidores MCP, y Claude Code los arranca sin preguntarte por cada uno.", "A project you did not write can then bring its own MCP servers, and Claude Code starts them without asking you about each one."},
	{"html.meaning.mcp_project_autoapprove.project", "En este proyecto, Claude Code arranca todos los servidores MCP que traiga el proyecto sin preguntarte por cada uno, también los que se añadan después.", "In this project, Claude Code starts every MCP server the project brings without asking you about each one, also the ones added later."},
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
