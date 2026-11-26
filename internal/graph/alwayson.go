package graph

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-251 (CTO-53 §5.1–§5.3, CTO-62 §1, §4): what can wake an agent without
// the person — its inbound channels and its own scheduled tasks — and the
// memory it writes. Only agents already in f get them; nothing here makes an
// agent.

// AddAlwaysOn adds to f, for each ai_client in it:
//   - an inbound_channel resource per channel of ao, and per Hermes Agent
//     gateway platform in hermes's configs (enabled_in_config when enabled,
//     configured when listed without enabled), and per Telegram or Discord
//     bot token f's credentials hold in a Hermes folder's .env (option C,
//     signed: origin env_name, configured) unless config.yaml disables that
//     platform; each with a wakes edge to the client;
//   - an agent_schedule resource per schedule of ao, with a wakes edge;
//   - an agent_memory resource per memory of ao, configuring the client.
//
// It is a pure function.
func AddAlwaysOn(f Fragment, hermes platform.HermesScan, ao platform.AlwaysOnScan, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	clients := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient {
			clients[n.ID] = true
		}
	}
	b := newFragmentBuilder()
	wake := func(res Node, client, source string) {
		if !b.node(res) {
			return
		}
		b.edge(Edge{ID: edgeID(EdgeWakes, res.ID, client), Type: EdgeWakes, From: res.ID, To: client, Source: source, Certainty: CertaintyObserved})
	}
	channel := func(ch platform.InboundChannel) {
		client := clientID(ch.Product, ch.ConfigDir)
		if !clients[client] || ch.Platform == "" {
			return
		}
		res := Node{
			ID: "res_inbound_channel_" + keyHash(ResourceInboundChannel, client, ch.Platform, canonicalPath(ch.Path)), Kind: KindResource, Type: ResourceInboundChannel,
			Path: ch.Path, DiscoveredAt: seen, Platform: ch.Platform, Status: ch.Status, Origin: ch.Origin,
			LaunchFlag: ch.LaunchFlag, Approvers: ch.Approvers, AllowAll: ch.AllowAll, AllowlistCount: ch.AllowlistCount,
		}
		wake(res, client, ch.Path)
	}
	for _, ch := range ao.Channels {
		channel(ch)
	}
	for _, ch := range hermesChannels(f, hermes) {
		channel(ch)
	}
	for _, s := range ao.Schedules {
		client := clientID(s.Product, s.ConfigDir)
		if !clients[client] {
			continue
		}
		wake(Node{
			ID: "res_agent_schedule_" + keyHash(ResourceAgentSchedule, client, canonicalPath(s.Path)), Kind: KindResource, Type: ResourceAgentSchedule,
			Path: s.Path, DiscoveredAt: seen, TaskCount: s.Tasks, ActiveCount: s.Active, Destinations: s.Destinations,
		}, client, s.Path)
	}
	for _, m := range ao.Memory {
		client := clientID(m.Product, m.ConfigDir)
		if !clients[client] {
			continue
		}
		files, size := m.Files, m.Bytes
		b.node(Node{
			ID: "res_agent_memory_" + keyHash(ResourceAgentMemory, canonicalPath(m.Path), client), Kind: KindResource, Type: ResourceAgentMemory,
			Path: m.Path, Configures: client, DiscoveredAt: seen, FileCount: &files, TotalBytes: &size, InRepository: m.InRepository,
			AutoMemoryOff: m.AutoMemoryOff,
		})
	}
	for _, fail := range ao.Failed {
		b.failed = append(b.failed, SourceFailed{Path: fail.Path, Reason: fail.Reason})
	}
	return Merge(f, b.fragment())
}

// hermesTokenPlatforms are the platforms a bot token found in Hermes Agent's
// .env turns on (gateway/config_env.py:95-118), by the credential's service.
var hermesTokenPlatforms = map[string]string{"telegram": "telegram", "discord": "discord"}

// hermesChannels are Hermes Agent's inbound channels (CTO-53 §1.1, §3): its
// gateway platforms, and the bot tokens its .env holds. Who can give orders
// follows approvals.approvers: absent or "*" is any admitted member (the
// default), a list without "*" is that list, an empty list is only the host.
func hermesChannels(f Fragment, hermes platform.HermesScan) []platform.InboundChannel {
	var out []platform.InboundChannel
	for _, cfg := range hermes.Configs {
		if cfg.Status != platform.MCPSourceRead {
			continue
		}
		approvers, count := platform.ApproversAnyoneAdmitted, (*int)(nil)
		if cfg.ApproversSet && !cfg.ApproversAnyone {
			n := cfg.ApproversCount
			approvers, count = platform.ApproversListed, &n
			if n == 0 {
				approvers, count = platform.ApproversHostOnly, nil
			}
		}
		base := platform.InboundChannel{
			Product: knowledge.HermesProduct, ConfigDir: cfg.ConfigDir, Path: cfg.Path,
			Origin: platform.ChannelOriginConfig, Approvers: approvers, AllowlistCount: count,
		}
		if cfg.GatewayUnread {
			ch := base
			ch.Platform, ch.Status, ch.Approvers, ch.AllowlistCount = "gateway", platform.ChannelUnknown, platform.ApproversUnknown, nil
			out = append(out, ch)
		}
		disabled, listed := map[string]bool{}, map[string]bool{}
		for _, p := range cfg.GatewayPlatforms {
			if p.EnabledSet && !p.Enabled {
				disabled[p.Name] = true
				continue
			}
			listed[p.Name] = true
			ch := base
			ch.Platform, ch.Status = p.Name, platform.ChannelConfigured
			if p.EnabledSet {
				ch.Status = platform.ChannelEnabledInConfig
			}
			out = append(out, ch)
		}
		env := filepath.Join(cfg.ConfigDir, ".env")
		for _, n := range f.Nodes {
			if n.Kind != KindCredential {
				continue
			}
			name, ok := hermesTokenPlatforms[n.Service]
			if !ok || disabled[name] || listed[name] || !credentialIn(n, env) {
				continue
			}
			ch := base
			ch.Path, ch.Platform, ch.Status, ch.Origin = env, name, platform.ChannelConfigured, platform.ChannelOriginEnvName
			out = append(out, ch)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out
}

// credentialIn reports a location of cred in file.
func credentialIn(cred Node, file string) bool {
	want := canonicalPath(file)
	for _, f := range credentialFiles(cred) {
		if canonicalPath(f) == want {
			return true
		}
	}
	return false
}

// WakeSource is one source that can wake an agent (BV-251), as the rules
// read it.
type WakeSource struct {
	Resource Node
	// Active: it wakes the agent today — a channel configured, enabled in
	// config or needing a launch flag; a schedule with an enabled task.
	Active bool
}

// WakeSources are the sources with a wakes edge to agent, sorted by id.
func WakeSources(g Graph, agent string) []WakeSource {
	nodes := map[string]Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var out []WakeSource
	for _, e := range g.Edges {
		if e.Type != EdgeWakes || e.To != agent {
			continue
		}
		r, ok := nodes[e.From]
		if !ok {
			continue
		}
		out = append(out, WakeSource{Resource: r, Active: wakeActive(r)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource.ID < out[j].Resource.ID })
	return out
}

func wakeActive(r Node) bool {
	switch r.Type {
	case ResourceInboundChannel:
		switch r.Status {
		case platform.ChannelConfigured, platform.ChannelEnabledInConfig, platform.ChannelLaunchFlagRequired:
			return true
		}
	case ResourceAgentSchedule:
		return r.ActiveCount != nil && *r.ActiveCount > 0
	}
	return false
}

// MemoryOf are agent's agent_memory resources, sorted by path.
func MemoryOf(g Graph, agent string) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if n.Kind == KindResource && n.Type == ResourceAgentMemory && n.Configures == agent {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Path, out[j].Path) < 0 })
	return out
}
