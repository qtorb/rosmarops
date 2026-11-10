package integration_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agents"
	"github.com/qtorb/rosmarops/internal/pipeline"
)

// productOfSource maps a config source path to the product whose reader opens
// it, by the product's configuration-folder marker. antigravity-cli is checked
// before gemini-cli (its path is under ~/.gemini/antigravity-cli).
func productOfSource(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	switch {
	case strings.Contains(p, "/antigravity-cli/"):
		return "antigravity-cli"
	case strings.Contains(p, "/.claude/"):
		return "claude-code"
	case strings.Contains(p, "/.codex/"):
		return "codex"
	case strings.Contains(p, "/.grok/"):
		return "grok-build"
	case strings.Contains(p, "/.gemini/"):
		return "gemini-cli"
	case strings.Contains(p, "/.cline/"):
		return "cline"
	case strings.Contains(p, "/hermes/"):
		return "hermes"
	case strings.Contains(p, "/Code/User/"):
		return "github-copilot"
	case strings.Contains(p, "/.cursor/"):
		return "cursor"
	}
	return ""
}

// TestRegistrySourcesOpenedOnFullMachine is the anti-inflation gate of the
// complexity axis (BV-208 pt3, CTO-26 §3): every configuration source a product
// declares in internal/agents must be one its reader actually opens on the full
// machine (E8). The complexity figure therefore cannot be padded with sources
// the engine never opens. It checks each product's declared affects-permissions
// count against the number of sources TestFullMachinePipeline's assembly read
// for that product.
func TestRegistrySourcesOpenedOnFullMachine(t *testing.T) {
	in, _ := fullMachineInput(true)
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatalf("pipeline.Assemble: %v", err)
	}
	opened := map[string]int{}
	for _, path := range res.Graph.Coverage.SourcesRead {
		if p := productOfSource(path); p != "" {
			opened[p]++
		}
	}
	for _, e := range agents.All() {
		declared := len(e.AffectsSources())
		if declared > opened[e.Product] {
			t.Errorf("%s declares %d permission-affecting config sources but the full machine opened only %d — a declared source is not read (complexity would be inflated)",
				e.Product, declared, opened[e.Product])
		}
	}
}
