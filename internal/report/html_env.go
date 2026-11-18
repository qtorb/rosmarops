package report

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/hostenv"
)

// envTag is the «Review of a test environment» tag under the sheet's title
// (BV-249 §5, Albert 16-11 15:27): with a container, a virtual machine or
// Windows Sandbox, not with WSL.
func (c htmlCtx) envTag() string {
	if !c.v.Environment.TestEnvironment() {
		return ""
	}
	return `<div><span class="sh-env">` + c.t("env.tag", nil) + `</span></div>`
}

// EnvTexts are the environment's sentences in a catalog (BV-249 §5): the
// first that applies, then for a container its mounts and the Docker socket.
func EnvTexts(e *hostenv.Env, text func(key string, params map[string]string) string) []string {
	if e == nil {
		return nil
	}
	name := func(n string) string {
		if n == "" {
			return text("env.engine.unidentified", nil)
		}
		return n
	}
	switch e.Kind {
	case hostenv.KindContainer:
		out := []string{text("env.container", map[string]string{"engine": name(hostenv.EngineName(e.Engine))})}
		if len(e.HostMounts) > 0 {
			out = append(out, text("env.container.mounts", map[string]string{"list": strings.Join(e.HostMounts, ", ")}))
		} else {
			out = append(out, text("env.container.nomounts", nil))
		}
		if e.DockerSocket {
			out = append(out, text("env.container.dockersock", nil))
		}
		return out
	case hostenv.KindWSL:
		if len(e.Drives) == 0 {
			return nil // the sentence names the drives it sees; none seen, nothing to say
		}
		return []string{text("env.wsl", map[string]string{"list": strings.Join(e.Drives, ", ")})}
	case hostenv.KindVirtualMachine:
		return []string{text("env.vm", map[string]string{"type": name(hostenv.VMTypeName(e.VMType))})}
	case hostenv.KindWindowsSandbox:
		return []string{text("env.wsb", nil)}
	}
	return nil
}
