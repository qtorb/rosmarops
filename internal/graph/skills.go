package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
)

// AddSkills adds to f the skills of scan (decision 2026-10-28, skills y
// herramientas de agentes, K1), one resource per file and agent that loads
// it, configuring that ai_client: every SKILL.md as agent_instructions and
// every file under scripts/ as agent_executable, with the shared ids, since a
// skills folder (.agents\skills) can be loaded by several agents. Only agents
// already in f get resources, and only their failures go to coverage. Nothing
// is read from the files. It is a pure function.
func AddSkills(f Fragment, scan platform.SkillScan, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	clients := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient {
			clients[n.ID] = true
		}
	}
	b := newFragmentBuilder()
	for _, file := range scan.Instructions {
		if client := clientID(file.Product, file.ConfigDir); clients[client] {
			b.node(Node{
				ID: sharedInstructionsResourceID(file.Path, client), Kind: KindResource, Type: ResourceAgentInstructions,
				Path: file.Path, Configures: client, DiscoveredAt: seen,
			})
		}
	}
	for _, file := range scan.Executables {
		if client := clientID(file.Product, file.ConfigDir); clients[client] {
			b.node(Node{
				ID: sharedExecutableResourceID(file.Path, client), Kind: KindResource, Type: ResourceAgentExecutable,
				Path: file.Path, Configures: client, DiscoveredAt: seen,
			})
		}
	}
	for _, fail := range scan.Failed {
		if clients[clientID(fail.Product, fail.ConfigDir)] {
			b.failed = append(b.failed, SourceFailed{Path: fail.Path, Reason: fail.Reason})
		}
	}
	return Merge(f, b.fragment())
}

// sharedExecutableResourceID derives the id of an executable an agent runs,
// one resource per agent: type + canonical path + agent id, as
// sharedInstructionsResourceID (decision K1, item 2). A stdio MCP server's
// command has the same id.
func sharedExecutableResourceID(path, agent string) string {
	return "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(path), agent)
}
