package report

import (
	"fmt"
	"html"
	"strconv"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-251 (CTO-62 §1, §4, §6; texts signed by the advisor, 17-11): the
// acts_without_you finding's lines, each agent's «Who can give it orders»,
// its memory and what RosmarOps can't see while it's not running.

// actsDetailKey is the catalog key of each wake source detail (the first that
// applies, rules.strongestWake).
var actsDetailKey = map[string]string{
	rules.WakeAnyone:  "finding.acts_without_you.detail.anyone_admitted",
	rules.WakeListed:  "finding.acts_without_you.detail.listed",
	rules.WakeUnknown: "finding.acts_without_you.detail.unknown",
	rules.WakeTasks:   "finding.acts_without_you.detail.tasks",
}

// unseenKeys are CTO-62 §6, one line each.
var unseenKeys = []string{
	"unseen.queue", "unseen.duplicates", "unseen.concurrency", "unseen.retries",
	"unseen.outbox", "unseen.running", "unseen.token",
}

// ActsKeys are every catalog key the acts_without_you finding and the
// always-on lines can show.
func ActsKeys() []string {
	keys := []string{"finding.acts_without_you.title", "finding.acts_without_you.flag"}
	for _, k := range []string{rules.WakeAnyone, rules.WakeListed, rules.WakeUnknown, rules.WakeTasks} {
		keys = append(keys, actsDetailKey[k])
	}
	keys = append(keys, "orders.label", "orders.only_you", "orders.listed", "orders.anyone_admitted", "orders.unknown",
		"orders.not_read", "memory.line", "unseen.title")
	return append(keys, unseenKeys...)
}

// platformNames are the display names of the platforms (proper names, not
// translated).
var platformNames = map[string]string{
	"telegram": "Telegram", "discord": "Discord", "slack": "Slack", "whatsapp": "WhatsApp",
	"signal": "Signal", "matrix": "Matrix", "email": "Email", "api_server": "API Server", "webhook": "Webhooks",
	"webhooks": "Webhooks", "imessage": "iMessage", "teams": "Teams",
}

// PlatformName is a platform's display name; the code itself when unknown.
func PlatformName(code string) string {
	if n, ok := platformNames[code]; ok {
		return n
	}
	return code
}

// ActsDetail are the detail lines of an acts_without_you finding: its source
// (open chat, list, unknown who, only tasks) and, when the source needs a
// launch flag, the «Only when it's started with {flag}» line.
func ActsDetail(cat catalog.Catalog, f rules.Finding) []string {
	key, ok := actsDetailKey[f.Params.Scope]
	if !ok {
		return nil
	}
	p := map[string]string{"platform": PlatformName(f.Params.Service), "n": strconv.Itoa(f.Params.Count)}
	out := []string{cat.Text(key, p)}
	if f.Params.ResourceType == rules.WakeLaunchFlag {
		out = append(out, cat.Text("finding.acts_without_you.flag", map[string]string{"flag": rules.LaunchFlagOf("claude-code")}))
	}
	return out
}

// OrdersText is the value of an agent's «Who can give it orders» line, and
// the «not read» sentence when there is one.
func OrdersText(cat catalog.Catalog, o *rules.OverviewOrders) (value, note string) {
	if o == nil {
		return "", ""
	}
	switch o.Value {
	case rules.OrdersListed:
		n := 0
		if o.Count != nil {
			n = *o.Count
		}
		return cat.Text("orders.listed", map[string]string{"n": strconv.Itoa(n)}), ""
	case rules.OrdersNotRead:
		return cat.Text("orders.unknown", nil), cat.Text("orders.not_read", nil)
	}
	return cat.Text("orders."+o.Value, nil), ""
}

// sizeText is a size in bytes, rounded: B, KB, MB or GB (1024-based).
func sizeText(b int64) string {
	const k = 1024
	switch {
	case b < k:
		return fmt.Sprintf("%d B", b)
	case b < k*k:
		return fmt.Sprintf("%d KB", (b+k/2)/k)
	case b < k*k*k:
		return fmt.Sprintf("%d MB", (b+k*k/2)/(k*k))
	}
	return fmt.Sprintf("%d GB", (b+k*k*k/2)/(k*k*k))
}

// MemoryText is an agent's memory line: every memory folder's files and size
// together; "" without memory.
func MemoryText(cat catalog.Catalog, a rules.OverviewAgent) string {
	if len(a.Memory) == 0 {
		return ""
	}
	files, size := 0, int64(0)
	for _, m := range a.Memory {
		files += m.Files
		size += m.Bytes
	}
	return cat.Text("memory.line", map[string]string{"n": strconv.Itoa(files), "size": sizeText(size)})
}

// hasActiveSource reports an acts_without_you finding on agent a (the agent
// has a wake source on and acts without you).
func (c htmlCtx) hasActs(a rules.OverviewAgent) bool {
	for _, f := range c.v.Findings {
		if f.Rule == rules.RuleActsWithoutYou && len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == a.AgentID {
			return true
		}
	}
	return false
}

// renderAlwaysOn writes, for an evaluated agent of the overview window, its
// «Who can give it orders» line, its memory line, and for an agent with a
// wake source on, the fold of what RosmarOps can't see while it's not running
// (CTO-62 §6).
func (c htmlCtx) renderAlwaysOn(a rules.OverviewAgent) {
	value, note := OrdersText(c.cat, a.Orders)
	if value == "" {
		return
	}
	c.w(`<div class="ovsvc ovord"><p><b>%s</b> · %s: %s`, html.EscapeString(a.Agent), c.t("orders.label", nil), html.EscapeString(value))
	if note != "" {
		c.w(` · %s`, html.EscapeString(note))
	}
	c.w(`</p>`)
	if m := MemoryText(c.cat, a); m != "" {
		c.w(`<p>%s</p>`, html.EscapeString(m))
	}
	if c.hasActs(a) {
		c.w(`<details><summary>%s</summary><ul>`, c.t("unseen.title", nil))
		for _, k := range unseenKeys {
			c.w(`<li>%s</li>`, c.t(k, nil))
		}
		c.w(`</ul></details>`)
	}
	c.w(`</div>`)
}
