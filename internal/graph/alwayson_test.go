package graph

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-251 (CTO-53 §1.1 option C): a Telegram token in Hermes's .env is a
// configured channel (origin env_name) unless config.yaml disables the
// platform; gateway platforms are enabled_in_config or configured; every
// channel has a wakes edge to its client; approvers follow approvals.approvers.
func TestAddAlwaysOnHermes(t *testing.T) {
	dir := "/h/.hermes"
	client := clientID(knowledge.HermesProduct, dir)
	f := Fragment{Nodes: []Node{
		{ID: client, Kind: KindAgent, Type: AgentAIClient, Platform: knowledge.HermesProduct, ConfigPath: dir},
		{ID: "cred_tg", Kind: KindCredential, Service: "telegram", Locations: []string{dir + "/.env:3"}},
		{ID: "cred_dc", Kind: KindCredential, Service: "discord", Locations: []string{dir + "/.env:4"}},
	}}
	cfg := platform.HermesConfig{
		Path: dir + "/config.yaml", ConfigDir: dir, Status: platform.MCPSourceRead,
		GatewayPlatforms: []platform.HermesPlatform{{Name: "discord", EnabledSet: true}, {Name: "slack", Enabled: true, EnabledSet: true}},
		ApproversSet:     true, ApproversCount: 1,
	}
	out := AddAlwaysOn(f, platform.HermesScan{Configs: []platform.HermesConfig{cfg}}, platform.AlwaysOnScan{}, time.Unix(0, 0))
	got := map[string]Node{}
	wakes := 0
	for _, n := range out.Nodes {
		if n.Type == ResourceInboundChannel {
			got[n.Platform] = n
		}
	}
	for _, e := range out.Edges {
		if e.Type == EdgeWakes && e.To == client {
			wakes++
		}
	}
	if _, ok := got["discord"]; ok {
		t.Errorf("discord disabled in config.yaml is a channel: %+v", got["discord"])
	}
	if s := got["slack"]; s.Status != platform.ChannelEnabledInConfig || s.Origin != platform.ChannelOriginConfig || s.Approvers != platform.ApproversListed {
		t.Errorf("slack: %+v", s)
	}
	if tg := got["telegram"]; tg.Status != platform.ChannelConfigured || tg.Origin != platform.ChannelOriginEnvName {
		t.Errorf("telegram from .env: %+v", tg)
	}
	if wakes != 2 {
		t.Errorf("%d wakes edges, want 2", wakes)
	}
}
