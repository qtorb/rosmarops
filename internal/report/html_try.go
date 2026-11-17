package report

import (
	"html"
	"path"
	"path/filepath"
	"strconv"
)

// tryCmd is one «Try it just this once» command: the project it is for (""
// when it names none) and the command.
type tryCmd struct{ name, cmd string }

// renderTry is «Try it just this once» (BV-249 §8, CTO-60): a secondary
// button that opens a window like «See the text», with the signed sentence,
// the session command (one per project, chosen in a list when there are
// several) and «Copy the command». note is a catalog key under the command,
// or "". Nothing is measured: the score does not change.
func (c htmlCtx) renderTry(id, agent string, cmds []tryCmd, note string) {
	if len(cmds) == 0 {
		return
	}
	c.w(`<div class="bt"><button type="button" class="btn s" data-see="%s" aria-controls="%s" aria-expanded="false">%s</button></div>`, id, id, c.t("try.button", nil))
	c.w(`<div class="ptext ptry" id="%s" tabindex="-1"><p>%s</p>`, id, c.t("try.intro", map[string]string{"agent": agent}))
	if len(cmds) > 1 {
		c.w(`<select data-try="%s" aria-label="%s">`, id, html.EscapeString(agent))
		for i, x := range cmds {
			c.w(`<option value="%d">%s</option>`, i, html.EscapeString(x.name))
		}
		c.w(`</select>`)
	}
	for i, x := range cmds {
		c.w(`<div data-tryi="%d"%s><pre class="tcmd" id="%s-%d">%s</pre><span class="live" aria-live="polite"></span></div>`, i, hide(i == 0), id, i, html.EscapeString(x.cmd))
	}
	if note != "" {
		c.w(`<p>%s</p>`, c.t(note, nil))
	}
	c.w(`<div class="bt"><button type="button" class="btn p" data-copy="%s-0" data-done="%s" data-fail="%s">%s</button></div></div>`,
		id, c.t("cc.copied", nil), c.t("copy.C1", nil), c.t("cc.copy.cmd", nil))
}

// claudeTryCmds are Claude Code's session commands, one per project.
func (c htmlCtx) claudeTryCmds(build func(root string) string) []tryCmd {
	var out []tryCmd
	for _, r := range c.v.repoRoots {
		out = append(out, tryCmd{name: path.Base(filepath.ToSlash(r)), cmd: build(r)})
	}
	return out
}

func tryID(where, agent string, n int) string { return where + "-try-" + agent + "-" + strconv.Itoa(n) }
