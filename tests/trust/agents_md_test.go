package trust_test

// BV-150: AGENTS.md at the repository root holds the review guidelines the
// external PR reviewer (Codex) reads. Guard it so it is not deleted or emptied
// by accident: it must exist and still carry its "## Review guidelines" heading.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsMarkdownHasReviewGuidelines(t *testing.T) {
	path := filepath.Join("..", "..", "AGENTS.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("AGENTS.md at the repository root is missing: %v", err)
	}
	if !strings.Contains(string(b), "## Review guidelines") {
		t.Error("AGENTS.md no longer contains the \"## Review guidelines\" section")
	}
}
