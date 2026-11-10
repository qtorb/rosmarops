package problems

import "testing"

// TestAgentNameCoversEveryKnownAgent is CTO-32 N3: every id ValidAgent accepts
// has a product display name, so the prompt never shows a raw id. The sent
// value stays the id; only the display uses the name.
func TestAgentNameCoversEveryKnownAgent(t *testing.T) {
	for _, id := range KnownAgents() {
		name := AgentName(id)
		if name == "" || name == id {
			t.Errorf("agent %q has no display name (got %q)", id, name)
		}
	}
	// An unknown id falls back to itself, never empty.
	if got := AgentName("not-a-product"); got != "not-a-product" {
		t.Errorf("unknown id should fall back to itself, got %q", got)
	}
}
