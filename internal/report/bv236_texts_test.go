package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// bv236Texts are BV-236 §7's texts, literal (ES | EN), with the forms of 1.
// cc.back, sheet.findings.count.one and pc.others are Albert's answers of
// 15-11.
var bv236Texts = []struct{ key, es, en string }{
	{"sheet.title", "Control de tus agentes de IA", "Control of your AI agents"},
	{"sheet.sub", "Qué pueden hacer sin preguntarte en este ordenador, y qué cambiar.", "What they can do without asking you on this computer, and what to change."},
	{"sheet.meta.date", "Fecha", "Date"},
	{"sheet.meta.computer", "Ordenador", "Computer"},
	{"sheet.meta.scope", "Alcance", "Scope"},
	{"sheet.meta.program", "Programa", "Program"},
	{"sheet.scope", "{agents} · {projects}", "{agents} · {projects}"},
	{"sheet.scope.agents.one", "1 agente", "1 agent"},
	{"sheet.scope.agents.other", "{n} agentes", "{n} agents"},
	{"sheet.scope.projects.one", "1 proyecto", "1 project"},
	{"sheet.scope.projects.other", "{n} proyectos", "{n} projects"},
	{"sheet.result", "Resultado", "Result"},
	{"sheet.result.label", "Nota de tu ordenador · {agent}", "Your computer's score · {agent}"},
	{"sheet.provisional", "provisional", "provisional"},
	{"sheet.class.A", "Hace poco sin preguntarte", "Does little without asking you"},
	{"sheet.class.B", "Hace algunas cosas sin preguntarte", "Does some things without asking you"},
	{"sheet.class.C", "Hace bastante sin preguntarte", "Does quite a lot without asking you"},
	{"sheet.class.D", "Hace mucho sin preguntarte", "Does a lot without asking you"},
	{"sheet.reference", "Referencia", "Reference"},
	{"sheet.reference.value", "{n} /100 con los ajustes de fábrica, en este ordenador", "{n}/100 with factory settings, on this computer"},
	{"sheet.reading", "Lectura", "Reading"},
	{"sheet.reading.L1", "La nota la marca {agent}, tu agente con menos control.", "{agent} sets the score: it is your agent with the least control."},
	{"sheet.reading.L2.one", "Tus ajustes te quitan 1 punto.", "Your settings take 1 point off."},
	{"sheet.reading.L2.other", "Tus ajustes te quitan {n} puntos.", "Your settings take {n} points off."},
	{"sheet.reading.L3.one", "Tus ajustes te suman 1 punto.", "Your settings add 1 point."},
	{"sheet.reading.L3.other", "Tus ajustes te suman {n} puntos.", "Your settings add {n} points."},
	{"sheet.reading.L4", "Tus ajustes no cambian la nota.", "Your settings don't change the score."},
	{"sheet.reading.L5", "La clase mide cómo trabaja el agente; la nota, lo que queda expuesto en este ordenador. Aquí no coinciden porque hay avisos que no dependen del agente, como una clave en un repositorio.", "The class measures how the agent works; the score, what is exposed on this computer. They differ here because some findings don't depend on the agent, like a key in a repository."},
	{"sheet.reading.L6", "La clase mide cómo trabaja el agente; la nota, lo que queda expuesto en este ordenador. Aquí no coinciden porque el agente hace mucho sin preguntarte, pero no tiene claves a su alcance.", "The class measures how the agent works; the score, what is exposed on this computer. They differ here because the agent does a lot without asking you, but has no keys within reach."},
	{"sheet.reading.L7", "{agent} tiene peor clase: {letter}.", "{agent} has a worse class: {letter}."},
	{"sheet.kind.money", "Dinero", "Money"},
	{"sheet.kind.sends_out", "Enviar fuera", "Sending out"},
	{"sheet.kind.keys", "Tus claves", "Your keys"},
	{"sheet.kind.runs_commands", "Ejecutar", "Running commands"},
	{"sheet.kind.changes_files", "Cambiar ficheros", "Changing files"},
	{"sheet.state.bounded", "Acotado", "Within limits"},
	{"sheet.state.none_known", "Ningún servicio de pago conocido", "No known payment service"},
	{"sheet.who.agents", "{n} agentes", "{n} agents"},
	{"sheet.assumed.note", "* Depende de algo que no se lee desde el disco: mira «Supuestos del análisis».", "* Depends on something that can't be read from disk: see «Analysis assumptions»."},
	{"sheet.findings", "Avisos", "Findings"},
	{"sheet.findings.count.first", "{n} avisos · los 3 primeros", "{n} findings · the first 3"},
	{"sheet.findings.count", "{n} avisos", "{n} findings"},
	{"sheet.col.no", "Nº", "No."},
	{"sheet.col.finding", "Aviso", "Finding"},
	{"sheet.col.affects", "Afecta a", "Affects"},
	{"sheet.col.level", "Nivel", "Level"},
	{"sheet.col.change", "Si lo cambias", "If you change it"},
	{"sheet.col.change.sub", "(nota del agente)", "(agent's score)"},
	{"sheet.would", "{agent} subiría a {n}", "{agent} would go up to {n}"},
	{"sheet.act", "Cómo actuar", "What to do"},
	{"sheet.act.A1", "La forma rápida: dinos para qué usas tus agentes y te proponemos cómo debe trabajar cada uno. Si prefieres hacerlo a mano, debajo tienes los pasos de cada aviso.", "The quick way: tell us what you use your agents for and we suggest how each one should work. If you'd rather do it by hand, the steps for each finding are below."},
	{"sheet.act.profile", "Elegir un perfil", "Choose a profile"},
	{"sheet.act.profile.sub", "Una pregunta y un texto para pegar", "One question and a text to paste"},
	{"sheet.act.findings", "Ver los avisos", "See the findings"},
	{"sheet.act.findings.sub", "Con los pasos de cada uno", "With the steps for each one"},
	{"sheet.foot.P1", "Generado en tu ordenador. Solo lectura: no ha cambiado nada.", "Made on your computer. Read only: nothing was changed."},
	{"sheet.foot.P2", "Clase: metodología weighted_v1, provisional", "Class: weighted_v1 method, provisional"},
	{"sheet.foot.P2.final", "Clase: metodología weighted_v1", "Class: weighted_v1 method"},
	{"pc.back", "← Informe", "← Report"},
	{"pc.kicker", "Hazlo tú por mí", "Do it for me"},
	{"pc.q", "¿Para qué usas tus agentes?", "What do you use your agents for?"},
	{"pc.use.code", "Programo", "I write code"},
	{"pc.use.docs", "Trabajo con documentos", "I work with documents"},
	{"pc.use.mail", "Correo y calendario", "Email and calendar"},
	{"pc.use.tasks", "Tareas programadas", "Scheduled tasks"},
	{"pc.computer", "Tu ordenador", "Your computer"},
	{"pc.B1", "Ahora la marcaría {agent}.", "{agent} would then set it."},
	{"pc.B1.noprofile", "Ahora la marcaría {agent}, que todavía no tiene perfil.", "{agent} would then set it, as it has no profile yet."},
	{"pc.B2", "Desatendido es una orden de arranque: RosmarOps no puede medirlo.", "Unattended is a launch command: RosmarOps can't measure it."},
	{"pc.B3", "Ningún agente pasará de 49 mientras haya una clave en tu historial de git.", "No agent will go above 49 while there is a key in your git history."},
	{"pc.suggest", "Te proponemos esto para cada agente", "Here is what we suggest for each agent"},
	{"pc.col.agent", "Agente", "Agent"},
	{"pc.col.today", "Hoy", "Today"},
	{"pc.col.suggest", "Te proponemos", "We suggest"},
	{"pc.col.nc", "Lo que no cubre", "What it doesn't cover"},
	{"pc.change", "Cambiar", "Change"},
	{"pc.soon", "Perfil en la próxima versión", "Profile in the next version"},
	{"pc.N1", "Su búsqueda web no se puede cerrar todavía desde su configuración.", "Its web search can't be turned off from its settings yet."},
	{"pc.N2", "Lo principal se cambia en su pantalla, y RosmarOps no puede leerlo.", "The main settings are changed on its screen, and RosmarOps can't read them."},
	{"pc.score", "Nota {a} → {b}", "Score {a} → {b}"},
	{"pc.class", "Clase", "Class"},
	{"cc.title", "Elige cómo trabaja Claude Code", "Choose how Claude Code works"},
	{"cc.lead", "Cada perfil usa solo los permisos del propio Claude Code. RosmarOps no toca tu configuración: te da un texto y se lo pides a Claude Code.", "Each profile uses only Claude Code's own permissions. RosmarOps doesn't touch your settings: it gives you a text and you ask Claude Code."},
	{"cc.today", "Hoy:", "Today:"},
	{"cc.use", "Uso", "Use"},
	{"cc.use.where", "{use} · se cambia en «Perfil del ordenador»", "{use} · change it in «Computer profile»"},
	{"cc.name.P0", "Lectura", "Read only"},
	{"cc.name.P1", "Desarrollo local", "Local development"},
	{"cc.name.P3", "Desatendido", "Unattended"},
	{"cc.rec", "Recomendado para {use}", "Recommended for {use}"},
	{"cc.use.lc.code", "programar", "coding"},
	{"cc.use.lc.docs", "trabajar con documentos", "working with documents"},
	{"cc.use.lc.mail", "correo y calendario", "email and calendar"},
	{"cc.use.lc.tasks", "tareas programadas", "scheduled tasks"},
	{"cc.row.does", "Hace solo", "Does on its own"},
	{"cc.row.asks", "Te pregunta", "Asks you"},
	{"cc.row.no", "No hace", "Doesn't do"},
	{"cc.row.nc", "No cubre", "Doesn't cover"},
	{"cc.P0.does.1", "Leer tus proyectos y responder", "Read your projects and answer"},
	{"cc.P0.no.1", "Editar ficheros", "Edit files"},
	{"cc.P0.no.2", "Usar la terminal", "Use the terminal"},
	{"cc.P0.no.3", "Buscar en internet o usar servidores MCP", "Search the web or use MCP servers"},
	{"cc.P0.nc", "Un ajuste de un proyecto o de tu empresa puede volver a abrir una herramienta que este perfil cierra.", "A setting in a project or from your company can reopen a tool this profile closes."},
	{"cc.P1.does.1", "Editar tus proyectos", "Edit your projects"},
	{"cc.P1.does.2", "Lanzar cuatro órdenes: npm run lint, npm run test, git status y git diff", "Run four commands: npm run lint, npm run test, git status and git diff"},
	{"cc.P1.asks.1", "Cualquier otra orden", "Any other command"},
	{"cc.P1.asks.2", "Cambiar la configuración o las instrucciones de otros agentes", "Change other agents' settings or instructions"},
	{"cc.P1.no.1", "Leer fuera de tus proyectos", "Read outside your projects"},
	{"cc.P1.no.2", "Buscar en internet, curl y wget", "Search the web, curl and wget"},
	{"cc.P1.nc", "npm run lint y npm run test ejecutan guiones que el agente puede editar: por ahí puede leer o enviar sin preguntarte. En Windows no hay sandbox que lo corte.", "npm run lint and npm run test run scripts the agent can edit: that way it can read or send without asking you. On Windows there is no sandbox to stop it."},
	{"cc.P1b.nc", "npm run lint y npm run test ejecutan guiones que el agente puede editar: por ahí puede leer o enviar sin preguntarte.", "npm run lint and npm run test run scripts the agent can edit: that way it can read or send without asking you."},
	{"cc.P3.does.1", "Trabajar sin nadie delante, solo con las cuatro órdenes de la lista", "Work with no one watching, only with the four commands on the list"},
	{"cc.P3.no.1", "Nada fuera de la lista: se deniega", "Anything off the list: it is denied"},
	{"cc.P3.no.2", "Usar los hooks y los servidores MCP del proyecto", "Use the project's hooks and MCP servers"},
	{"cc.P3.no.3", "Buscar en internet", "Search the web"},
	{"cc.P3.nc", "Lo que permitan los guiones de npm. RosmarOps no puede comprobar una orden de arranque: la nota no cambia aunque la uses.", "Whatever the npm scripts allow. RosmarOps can't check a launch command: the score doesn't change even if you use it."},
	{"cc.nomeasure", "No se puede medir: es una orden de arranque", "Can't be measured: it is a launch command"},
	{"cc.S1", "Copia el texto", "Copy the text"},
	{"cc.S2", "Pégalo en Claude Code", "Paste it into Claude Code"},
	{"cc.S3", "Claude Code te enseña el cambio y te pregunta antes de guardarlo", "Claude Code shows you the change and asks before saving it"},
	{"cc.S4", "RosmarOps no cambia nada. El perfil va en tu configuración de usuario: vale para todos tus proyectos.", "RosmarOps changes nothing. The profile goes in your user settings: it applies to all your projects."},
	{"cc.see.text", "Ver el texto", "See the text"},
	{"cc.copy.text", "Copiar el texto", "Copy the text"},
	{"cc.S5", "Copia la orden", "Copy the command"},
	{"cc.S6", "Úsala en tu tarea programada o en tu guion, cambiando <tarea>", "Use it in your scheduled task or script, replacing <task>"},
	{"cc.S7", "RosmarOps no cambia nada y no puede comprobar una orden de arranque.", "RosmarOps changes nothing and can't check a launch command."},
	{"cc.see.cmd", "Ver la orden", "See the command"},
	{"cc.copy.cmd", "Copiar la orden", "Copy the command"},
	{"cc.T1", "Añade este bloque a mi configuración de usuario de Claude Code (~/.claude/settings.json) sin cambiar nada más. Si ya tengo listas allow, ask o deny, súmalas. Enséñame el cambio antes de guardarlo.", "Add this block to my Claude Code user settings (~/.claude/settings.json) without changing anything else. If I already have allow, ask or deny lists, merge them. Show me the change before saving it."},
	{"copy.C1", "No se ha podido copiar: selecciona el texto y cópialo tú.", "Couldn't copy: select the text and copy it yourself."},
	{"cc.copied", "Copiado", "Copied"},
	{"cc.lab", "Prueba de laboratorio de «{profile}»: todavía no hecha.", "Lab test of «{profile}»: not done yet."},
	{"cc.R1", "Clase y nota medidas por RosmarOps en este ordenador.", "Class and score measured by RosmarOps on this computer."},
	{"report.accepted.title", "Asumidos por ti ({n})", "Accepted by you ({n})"},
	{"report.accepted.on", "Lo asumiste el {date}", "You accepted it on {date}"},
	{"findings.q.accept", "¿Qué haces con estos avisos?", "What do you want to do with these findings?"},
	{"findings.text.accept", "Lo asumo: sigue contando en tu nota y deja de salir como pendiente. Descartar: deja de contar desde la próxima revisión, para lo que no te afecta. Puedes cambiarlo cuando quieras.", "I accept it: it keeps counting in your score and stops showing as pending. Dismiss: it stops counting from the next check, for what doesn't apply to you. You can change this at any time."},
	{"findings.accept", "Lo asumo", "I accept it"},
	{"findings.dismiss", "Descartar", "Dismiss"},
	{"findings.group.accepted", "Asumidos por ti", "Accepted by you"},
	{"findings.tally", "{accepted} · {dismissed}", "{accepted} · {dismissed}"},
	{"findings.tally.accepted.one", "1 asumido", "1 accepted"},
	{"findings.tally.accepted.other", "{n} asumidos", "{n} accepted"},
	{"findings.tally.dismissed.one", "1 descartado", "1 dismissed"},
	{"findings.tally.dismissed.other", "{n} descartados", "{n} dismissed"},
	{"findings.tally.none", "Ninguno cambiado", "None changed"},
	{"findings.console.prompt.accept", "Escribe los números que quieras cambiar, separados por comas: «3» descarta o recupera el aviso 3; «3a» lo asume o lo deja pendiente (Enter para no cambiar nada): ", "Type the numbers you want to change, separated by commas: «3» dismisses or restores finding 3; «3a» accepts it or makes it pending again (Enter to change nothing): "},
	{"findings.console.accepted", "asumido por ti", "accepted by you"},
	{"cc.back", "← Perfil del ordenador", "← Computer profile"},
	{"sheet.findings.count.one", "1 aviso", "1 finding"},
	{"pc.others", "Los demás", "The others"},
}

// TestBV236Texts: every text of §7 is in the catalog, literal, in EN and ES.
func TestBV236Texts(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	en, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range bv236Texts {
		if !es.HasOwn(x.key) {
			t.Errorf("%s: no Spanish text", x.key)
		}
		if got := es.Text(x.key, nil); got != x.es {
			t.Errorf("%s es: %q, want %q", x.key, got, x.es)
		}
		if got := en.Text(x.key, nil); got != x.en {
			t.Errorf("%s en: %q, want %q", x.key, got, x.en)
		}
	}
}

// TestBV236FormsOfOne: the texts with a form of 1 use it for 1 and the
// general form otherwise.
func TestBV236FormsOfOne(t *testing.T) {
	cases := []struct {
		key        string
		one, other map[string]string // lang → text for 1 and for 3
	}{
		{"sheet.scope.agents", map[string]string{"es": "1 agente", "en": "1 agent"}, map[string]string{"es": "3 agentes", "en": "3 agents"}},
		{"sheet.scope.projects", map[string]string{"es": "1 proyecto", "en": "1 project"}, map[string]string{"es": "3 proyectos", "en": "3 projects"}},
		{"sheet.reading.L2", map[string]string{"es": "Tus ajustes te quitan 1 punto.", "en": "Your settings take 1 point off."}, map[string]string{"es": "Tus ajustes te quitan 3 puntos.", "en": "Your settings take 3 points off."}},
		{"sheet.reading.L3", map[string]string{"es": "Tus ajustes te suman 1 punto.", "en": "Your settings add 1 point."}, map[string]string{"es": "Tus ajustes te suman 3 puntos.", "en": "Your settings add 3 points."}},
		{"findings.tally.accepted", map[string]string{"es": "1 asumido", "en": "1 accepted"}, map[string]string{"es": "3 asumidos", "en": "3 accepted"}},
		{"findings.tally.dismissed", map[string]string{"es": "1 descartado", "en": "1 dismissed"}, map[string]string{"es": "3 descartados", "en": "3 dismissed"}},
	}
	for _, lang := range []string{"es", "en"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			if got := cat.Plural(c.key, 1, map[string]string{"n": "1"}); got != c.one[lang] {
				t.Errorf("%s %s(1): %q, want %q", lang, c.key, got, c.one[lang])
			}
			if got := cat.Plural(c.key, 3, map[string]string{"n": "3"}); got != c.other[lang] {
				t.Errorf("%s %s(3): %q, want %q", lang, c.key, got, c.other[lang])
			}
		}
		// W3: «{a} accepted · {d} dismissed», or «None changed» with both at 0.
		tally := cat.Text("findings.tally", map[string]string{
			"accepted":  cat.Plural("findings.tally.accepted", 1, map[string]string{"n": "1"}),
			"dismissed": cat.Plural("findings.tally.dismissed", 2, map[string]string{"n": "2"}),
		})
		want := map[string]string{"es": "1 asumido · 2 descartados", "en": "1 accepted · 2 dismissed"}[lang]
		if tally != want {
			t.Errorf("%s W3: %q, want %q", lang, tally, want)
		}
		if strings.Contains(cat.Text("findings.tally.none", nil), "{") {
			t.Errorf("%s: findings.tally.none has a placeholder", lang)
		}
	}
}
