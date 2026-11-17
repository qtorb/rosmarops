package report

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hostenv"
)

// BV-249 §5: the environment's sentences, the first that applies, and the
// «Review of a test environment» tag (not with WSL).
func TestEnvTexts(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		env  *hostenv.Env
		want []string
		tag  bool
	}{
		{"none", nil, nil, false},
		{"docker", &hostenv.Env{Kind: hostenv.KindContainer, Engine: hostenv.EngineDocker, HostMounts: []string{"/notes", "/workspace"}, DockerSocket: true}, []string{
			"Esta revisión se ha hecho dentro de un contenedor (Docker). Lo que ves es lo que un agente podría tocar ahí dentro, no en tu ordenador. Desde dentro no se puede saber si el agente corre en este mismo contenedor ni si el contenedor tiene internet.",
			"Este contenedor tiene montadas carpetas de fuera: /notes, /workspace. Desde dentro no se puede saber qué carpeta de tu ordenador es cada una.",
			"Este contenedor tiene acceso a Docker del ordenador: un agente aquí dentro podría controlar tu ordenador.",
		}, true},
		{"overlay", &hostenv.Env{Kind: hostenv.KindContainer, Engine: hostenv.Unidentified}, []string{
			"Esta revisión se ha hecho dentro de un contenedor (sin identificar). Lo que ves es lo que un agente podría tocar ahí dentro, no en tu ordenador. Desde dentro no se puede saber si el agente corre en este mismo contenedor ni si el contenedor tiene internet.",
			"No se ve ninguna carpeta de tu ordenador montada aquí.",
		}, true},
		{"wsl", &hostenv.Env{Kind: hostenv.KindWSL, Drives: []string{"C:", "D:"}}, []string{
			"Esta revisión se ha hecho en WSL. Desde aquí se ven tus discos de Windows: C:, D:. Un agente que trabaje en WSL podría tocar también tus carpetas de Windows.",
		}, false},
		{"vm", &hostenv.Env{Kind: hostenv.KindVirtualMachine, VMType: "hyper_v"}, []string{
			"Esta revisión se ha hecho en una máquina virtual (Hyper-V). Lo que ves es lo que un agente podría tocar en ella, no en el ordenador que la aloja.",
		}, true},
		{"sandbox", &hostenv.Env{Kind: hostenv.KindWindowsSandbox}, []string{
			"Esta revisión se ha hecho en Windows Sandbox: lo que ves es lo que un agente podría tocar dentro, y todo se borra al cerrarlo.",
		}, true},
	} {
		got := EnvTexts(c.env, es.Text)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: %q", c.name, got)
		}
		v := V2{Environment: c.env}
		ctx := htmlCtx{v: v, cat: es}
		tag := ctx.envTag()
		if (tag != "") != c.tag || c.tag && !strings.Contains(tag, "Revisión de un entorno de pruebas") {
			t.Errorf("%s: tag %q", c.name, tag)
		}
	}
}
