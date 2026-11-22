package report

import (
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// tplScans are the settings a template in use is recognised from (BV-273):
// the same scans the graph was built from.
type tplScans struct {
	claude *platform.ClaudeSettingsScan
	grok   platform.GrokScan
	cursor platform.CursorScan
}

// tplInUse are the templates agent may be using: one id for Claude Code and
// Grok Build; for Cursor both «Read only» and «Local development», which leave
// the same file and cannot be told apart; none when no template is in use.
func (c htmlCtx) tplInUse(agent string) []string {
	s := c.v.tplScans
	id := ""
	switch agent {
	case graph.ClientDisplayName(knowledge.ClaudeCodeProduct):
		if s.claude != nil {
			id = profiles.InUse(*s.claude, c.v.OS)
		}
	case graph.ClientDisplayName(knowledge.GrokBuildProduct):
		id = profiles.GrokInUse(s.grok, c.v.OS)
	case graph.ClientDisplayName(knowledge.CursorProduct):
		if profiles.CursorInUse(s.cursor) {
			return []string{profiles.P0, profiles.P1}
		}
	}
	if id == "" {
		return nil
	}
	return []string{id}
}

// tplNoLine is the first «Doesn't do» row of agent's template id, already in
// the catalog: Claude Code's from profileRows, Grok Build's and Cursor's from
// their guided profiles.
func (c htmlCtx) tplNoLine(agent, id string) string {
	if agent == graph.ClientDisplayName(knowledge.ClaudeCodeProduct) {
		if r := profileRows[id][2]; len(r) > 0 {
			return r[0][1]
		}
		return ""
	}
	for _, g := range guidedAgents {
		if graph.ClientDisplayName(g.product) != agent {
			continue
		}
		for _, p := range g.profiles {
			if p.id == id && len(p.rows[2]) > 0 {
				return p.rows[2][0][1]
			}
		}
	}
	return ""
}

// tplInUseLine is block 3's line for a template in use: «You already use the
// … template in …», then the next stricter one for this system with its first
// «Doesn't do» row, or that it is the strictest. "" when none is in use.
func (c htmlCtx) tplInUseLine(agent string) string {
	ids := c.tplInUse(agent)
	switch len(ids) {
	case 0:
		return ""
	case 2: // Cursor: either of its two templates, so no next one and no «strictest»
		return c.t("html.r3.tpl.in_use", map[string]string{"template": c.cat.Text("html.r3.tpl.cursor_either", nil), "agent": agent})
	}
	id := ids[0]
	line := c.t("html.r3.tpl.in_use", map[string]string{"template": c.cat.Text("cc.name."+id, nil), "agent": agent})
	if next := profiles.Next(id, c.v.OS); next != "" && c.offers(agent, next) {
		return line + " " + c.t("html.r3.tpl.next", map[string]string{
			"next": c.cat.Text("cc.name."+next, nil), "next_line": c.cat.Text(c.tplNoLine(agent, next), nil),
		})
	}
	return line + " " + c.t("html.r3.tpl.top", nil)
}

// offers reports whether agent has template id on this system: Claude Code's
// measured ones, Grok Build's and Cursor's guided ones.
func (c htmlCtx) offers(agent, id string) bool {
	if agent == graph.ClientDisplayName(knowledge.ClaudeCodeProduct) {
		for _, x := range c.ccIDs() {
			if x == id {
				return true
			}
		}
		return false
	}
	for _, g := range c.guided() {
		if graph.ClientDisplayName(g.product) != agent {
			continue
		}
		for _, p := range g.profiles {
			if p.id == id {
				return true
			}
		}
	}
	return false
}

// inUsePill is the green «In use» pill of a template card, or "": on both
// Cursor cards when one of its templates is in use.
func (c htmlCtx) inUsePill(agent, id string) string {
	for _, x := range c.tplInUse(agent) {
		if x == id {
			return ` <span class="r3-pill green">` + c.t("html.tpl.in_use_pill", nil) + `</span>`
		}
	}
	return ""
}
