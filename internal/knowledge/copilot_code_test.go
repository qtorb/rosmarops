package knowledge

import "testing"

// BV-140 decision 3: GitHub Copilot has a single product code, github-copilot.
// The old "copilot" code (skills attribution) is gone, so the extension, the
// ai_client and the skills can never be split across two codes — which is what
// would have lost a Copilot agent's history (keyed by display name) or its
// dismissals (keyed by finding id) when Copilot became scored.
func TestGitHubCopilot_SingleProductCode(t *testing.T) {
	if GitHubCopilotProduct != "github-copilot" {
		t.Fatalf("GitHubCopilotProduct = %q, want github-copilot", GitHubCopilotProduct)
	}
	// Every Copilot VS Code extension id maps to the one code and the one name.
	for _, id := range []string{"github.copilot", "github.copilot-chat"} {
		ext, ok := AgentExtensions[id]
		if !ok {
			t.Fatalf("extension %q is not a known agent", id)
		}
		if ext.Product != GitHubCopilotProduct {
			t.Errorf("extension %q product = %q, want %q", id, ext.Product, GitHubCopilotProduct)
		}
		if ext.Name != "GitHub Copilot" {
			t.Errorf("extension %q name = %q, want GitHub Copilot", id, ext.Name)
		}
	}
	// No knowledge base or extension still carries the retired "copilot" code.
	for _, ext := range AgentExtensions {
		if ext.Product == "copilot" {
			t.Errorf("the retired product code %q is still in AgentExtensions", "copilot")
		}
	}
}
