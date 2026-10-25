package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
)

// AddInstructions adds to f one agent_instructions resource per existing
// instruction file and agent that loads it, configuring that ai_client (decision
// 2026-10-23, Q7), and the files that could not be checked to coverage. Only
// agents already in f get resources: an instruction file alone does not make
// an agent. Nothing is read from the files. It is a pure function.
func AddInstructions(f Fragment, scan platform.InstructionScan, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	clients := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient {
			clients[n.ID] = true
		}
	}
	b := newFragmentBuilder()
	for _, file := range scan.Files {
		client := clientID(file.Product, file.ConfigDir)
		if !clients[client] {
			continue
		}
		id := instructionsResourceID(file.Path)
		if file.Shared {
			id = sharedInstructionsResourceID(file.Path, client)
		}
		b.node(Node{
			ID: id, Kind: KindResource, Type: ResourceAgentInstructions,
			Path: file.Path, Configures: client, DiscoveredAt: seen,
		})
	}
	for _, fail := range scan.Failed {
		if clients[clientID(fail.Product, fail.ConfigDir)] {
			b.failed = append(b.failed, SourceFailed{Path: fail.Path, Reason: fail.Reason})
		}
	}
	return Merge(f, b.fragment())
}

// sharedInstructionsResourceID derives the id of a file several agents load
// (AGENTS.md), one resource per agent: type + canonical path + agent id
// (decision BV-29, D4).
func sharedInstructionsResourceID(path, agent string) string {
	return "res_agent_instructions_" + keyHash(ResourceAgentInstructions, canonicalPath(path), agent)
}

// instructionsResourceID derives an agent_instructions resource id from its
// natural key: type + canonical path.
func instructionsResourceID(path string) string {
	return "res_agent_instructions_" + keyHash(ResourceAgentInstructions, canonicalPath(path))
}
