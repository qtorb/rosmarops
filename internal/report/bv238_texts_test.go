package report_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// bv238Texts are BV-238 §4's texts, literal (ES | EN), with Albert's
// changes of 15-11 to Grok Build (T3 and the three «Doesn't cover») and
// BV-239 (Grok Bot's G3 and «Doesn't cover», G4 with the date).
var bv238Texts = []struct{ key, es, en string }{
	{"gp.G1", "Sin medir", "Not measured"},
	{"gp.G2", "Las plantillas sin medir no cuentan aquí: RosmarOps todavía no puede medir su efecto.", "Templates that aren't measured don't count here: RosmarOps can't measure their effect yet."},
	{"gp.G3", "RosmarOps todavía no puede medir el efecto de esta plantilla.", "RosmarOps can't measure this template's effect yet."},
	{"gp.bot.G3", "RosmarOps todavía no puede medir el efecto de esta forma de trabajar.", "RosmarOps can't measure the effect of this way of working yet."},
	{"gp.G4", "Los pasos salen de la documentación de {agent} leída el {date}.", "The steps come from {agent}'s documentation, read on {date}."},
	{"gp.see_steps", "Ver los pasos", "See the steps"},
	{"gp.bot.q", "¿Usas Grok Bot?", "Do you use Grok Bot?"},
	{"gp.title", "Elige cómo trabaja {agent}", "Choose how {agent} works"},
	{"gp.side", "Cada plantilla usa solo los permisos del propio {agent}. RosmarOps no toca tu configuración.", "Each template uses only {agent}'s own permissions. RosmarOps doesn't touch your settings."},
	{"gp.bot.side", "Grok Bot trabaja en la nube de su fabricante. Tiene dos plantillas, una para cada forma de trabajar: solo en la nube, o también con tu ordenador.", "Grok Bot works in its maker's cloud. It has two templates, one for each way of working: cloud only, or with your computer too."},
	{"gp.cursor.use.none", "Es un editor de código: no hay plantilla para este uso.", "It's a code editor: there's no template for this use."},
	{"gp.cursor.use.tasks", "Su plantilla desatendida llega en la próxima versión.", "Its unattended template comes in the next version."},
	{"gp.cursor.P0.step.1", "Abre Settings > Agents > Approvals & Execution y elige el Run mode «Allowlist», sin ninguna orden en la lista.", "Open Settings > Agents > Approvals & Execution and choose the «Allowlist» run mode, with no commands on the list."},
	{"gp.cursor.step.read", "En la misma pantalla, en «Read Access», elige «Workspace».", "On the same screen, under «Read Access», choose «Workspace»."},
	{"gp.cursor.P0.step.3", "Al empezar cada conversación, elige el modo «Ask» en el panel del agente (Ctrl+I).", "When you start each conversation, choose «Ask» mode in the agent panel (Ctrl+I)."},
	{"gp.cursor.step.paste", "Copia el texto y pégalo en Cursor, que te enseñará el cambio antes de guardarlo.", "Copy the text and paste it into Cursor, which will show you the change before saving it."},
	{"gp.cursor.P0.does.1", "Leer tus proyectos y responder en el chat", "Read your projects and answer in the chat"},
	{"gp.cursor.P0.asks.1", "Cualquier orden", "Any command"},
	{"gp.cursor.P0.no.1", "Editar ficheros, en el modo Ask", "Edit files, in Ask mode"},
	{"gp.cursor.no.outside", "Leer fuera de tus proyectos", "Read outside your projects"},
	{"gp.cursor.P0.nc", "El modo Ask se elige en cada conversación: si cambias de modo, el agente puede volver a editar. En Windows, la documentación de Cursor no dice si el límite de lectura se aplica.", "Ask mode is chosen in each conversation: if you switch modes, the agent can edit again. On Windows, Cursor's documentation doesn't say whether the read limit applies."},
	{"gp.cursor.P1.step.1", "Abre Settings > Agents > Approvals & Execution, elige el Run mode «Allowlist» y añade cuatro órdenes: npm run lint, npm test, git status y git diff.", "Open Settings > Agents > Approvals & Execution, choose the «Allowlist» run mode and add four commands: npm run lint, npm test, git status and git diff."},
	{"gp.cursor.P1.step.3", "Activa «File-Deletion Protection» y comprueba que «External-File Protection» está activada.", "Turn on «File-Deletion Protection» and check that «External-File Protection» is on."},
	{"gp.cursor.P1.does.1", "Editar tus proyectos", "Edit your projects"},
	{"gp.cursor.P1.does.2", "Lanzar cuatro órdenes: npm run lint, npm test, git status y git diff", "Run four commands: npm run lint, npm test, git status and git diff"},
	{"gp.cursor.P1.asks.1", "Cualquier otra orden", "Any other command"},
	{"gp.cursor.P1.asks.2", "Borrar ficheros", "Delete files"},
	{"gp.cursor.P1.nc", "Cursor puede llegar a la configuración y las instrucciones de otros agentes en tus proyectos. En Windows, su documentación no dice si el límite de lectura se aplica.", "Other agents' settings and instructions inside your projects can't be put out of its reach in Cursor. On Windows, the documentation doesn't say whether the read limit applies."},
	{"gp.cursor.T2", "Crea o actualiza ~/.cursor/sandbox.json con este contenido, sin cambiar nada más. Enséñame el cambio antes de guardarlo.", "Create or update ~/.cursor/sandbox.json with this content, without changing anything else. Show me the change before saving it."},
	{"gp.grok.use.none", "Es un agente de código: no hay plantilla para este uso.", "It's a coding agent: there's no template for this use."},
	{"gp.grok.step.paste", "Copia el texto y pégalo en Grok Build, que te enseñará el cambio antes de guardarlo.", "Copy the text and paste it into Grok Build, which will show you the change before saving it."},
	{"gp.grok.P0.does.1", "Leer tus proyectos", "Read your projects"},
	{"gp.grok.P0.asks.1", "Cualquier otra cosa", "Anything else"},
	{"gp.grok.P0.no.1", "Editar ficheros", "Edit files"},
	{"gp.grok.P0.no.2", "Usar la terminal", "Use the terminal"},
	{"gp.grok.P0.no.3", "Abrir páginas web o usar servidores MCP", "Open web pages or use MCP servers"},
	{"gp.grok.no.hooks", "Ejecutar los hooks de Claude Code y Cursor", "Run Claude Code and Cursor hooks"},
	{"gp.grok.P0.nc", "Su documentación dice que la regla corta la búsqueda en internet; no lo hemos comprobado. En Windows no hay sandbox; en macOS, el sandbox no corta la red.", "Its documentation says the rule blocks web search; we haven't checked. On Windows there's no sandbox; on macOS, the sandbox doesn't cut the network."},
	{"gp.grok.P1.does.1", "Lanzar cuatro órdenes: npm run lint, npm run test, git status y git diff", "Run four commands: npm run lint, npm run test, git status and git diff"},
	{"gp.grok.P1.asks.1", "Editar ficheros", "Edit files"},
	{"gp.grok.P1.asks.2", "Cualquier otra orden", "Any other command"},
	{"gp.grok.no.web", "Abrir páginas web", "Open web pages"},
	{"gp.grok.P1.nc", "Su documentación dice que la regla corta la búsqueda en internet; no lo hemos comprobado. npm run lint y npm run test ejecutan guiones que el agente puede editar. En Windows no hay sandbox; en macOS y Linux, el sandbox no impide leer ~/.ssh ni ~/.aws.", "Its documentation says the rule blocks web search; we haven't checked. npm run lint and npm run test run scripts the agent can edit. On Windows there's no sandbox; on macOS and Linux, the sandbox doesn't stop it reading ~/.ssh or ~/.aws."},
	{"gp.grok.P3.step.1", "Aplica antes la plantilla «Desarrollo local».", "Apply the «Local development» template first."},
	{"gp.grok.P3.step.2", "Copia la orden y úsala en tu tarea programada o en tu guion, cambiando <tarea>. No añadas --trust en proyectos de otros.", "Copy the command and use it in your scheduled task or script, replacing <task>. Don't add --trust in other people's projects."},
	{"gp.grok.P3.does.1", "Trabajar sin nadie delante, solo con las cuatro órdenes de la lista", "Work with no one watching, only with the four commands on the list"},
	{"gp.grok.P3.no.1", "Nada fuera de la lista: se deniega", "Anything off the list: it is denied"},
	{"gp.grok.P3.nc", "Su documentación dice que la regla corta la búsqueda en internet; no lo hemos comprobado. En Windows el sandbox no está documentado; en macOS, no corta la red.", "Its documentation says the rule blocks web search; we haven't checked. On Windows the sandbox isn't documented; on macOS, it doesn't cut the network."},
	{"gp.grok.T3", "Añade estos bloques a mi configuración de usuario de Grok Build (~/.grok/config.toml), sin cambiar nada más. Si ya tengo reglas en [permission], súmalas. Enséñame el cambio antes de guardarlo.", "Add these blocks to my Grok Build user settings (~/.grok/config.toml), without changing anything else. If I already have rules in [permission], merge them. Show me the change before saving it."},
	{"gp.bot.name.cloud", "Solo nube", "Cloud only"},
	{"gp.bot.name.local", "Con tu ordenador", "With your computer"},
	{"gp.bot.use.tasks", "Sus rutinas no tienen un ajuste propio: sus aprobaciones caducan a los 30 minutos.", "Its routines have no setting of their own: their approvals expire after 30 minutes."},
	{"gp.bot.cloud.step.1", "En los ajustes de Grok Bot, pon «Execution on this computer» en «Never allow».", "In Grok Bot's settings, set «Execution on this computer» to «Never allow»."},
	{"gp.bot.step.review", "Activa «Auto-review».", "Turn on «Auto-review»."},
	{"gp.bot.step.ask", "Pon «Ask first» para enviar, publicar, borrar y comprar.", "Set «Ask first» for sending, publishing, deleting and buying."},
	{"gp.bot.cloud.does.1", "Trabajar en su ordenador en la nube", "Work on its computer in the cloud"},
	{"gp.bot.cloud.asks.1", "Enviar, publicar, borrar o comprar", "Send, publish, delete or buy"},
	{"gp.bot.cloud.no.1", "Ejecutar nada en tu ordenador", "Run anything on your computer"},
	{"gp.bot.cloud.nc", "«Ask first» lo decide otro modelo de IA: no es un corte. Lo que necesite de tus ficheros, se lo subes tú.", "Another AI model decides «Ask first»: it isn't a hard stop. Whatever it needs from your files, you upload yourself."},
	{"gp.bot.local.step.1", "En los ajustes de Grok Bot, pon «Execution on this computer» en «Ask every time».", "In Grok Bot's settings, set «Execution on this computer» to «Ask every time»."},
	{"gp.bot.local.does.1", "Trabajar en la nube", "Work in the cloud"},
	{"gp.bot.local.asks.1", "Cada orden en tu ordenador, enseñándote la orden exacta", "Each command on your computer, showing you the exact command"},
	{"gp.bot.local.nc", "Si un contenido engañoso le pide una orden, lo único que te queda es leer la tarjeta antes de aceptar. «Always allow» no tiene plantilla: ejecuta cualquier orden sin preguntarte.", "If deceptive content asks it for a command, all you have left is reading the card before you accept. «Always allow» has no template: it runs any command without asking you."},
}

// TestBV238Texts: every text of BV-238 §4 is in the catalog, literal, in EN
// and ES.
func TestBV238Texts(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	en, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range bv238Texts {
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
