package windows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-203 A1 (CTO-25): the bundled Copilot Chat extension with default settings
// and no ~/.copilot was not detected. It now is, through the per-extension
// globalStorage folder VS Code leaves — existence only, no database read.

func noEnv(string) (string, bool) { return "", false }

func TestCopilotDetectedByGlobalStorage(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	userDir := filepath.Join(tmp, "Code", "User") // a VS Code user dir
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// No settings layers, no MCP, no creds, no ~/.copilot: nothing but the
	// globalStorage folder.
	scan := platform.CopilotScan{ConfigDir: userDir}
	if copilotPresent(scan, home, agentdirs.Env(noEnv)) {
		t.Fatal("Copilot reported present before any signal exists")
	}

	// The extension's globalStorage folder appears once it has run.
	if err := os.MkdirAll(filepath.Join(userDir, "globalStorage", "github.copilot-chat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !copilotPresent(scan, home, agentdirs.Env(noEnv)) {
		t.Error("Copilot not detected by its globalStorage folder (A1)")
	}
}

// CTO-30 B5: the globalStorage folder survives turning the AI off, so it does
// not count as a present, scored Copilot when a read settings.json layer sets
// chat.disableAIFeatures: true.
func TestCopilotGlobalStorageIgnoredWhenAIDisabled(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	userDir := filepath.Join(tmp, "Code", "User")
	if err := os.MkdirAll(filepath.Join(userDir, "globalStorage", "github.copilot-chat"), 0o755); err != nil {
		t.Fatal(err)
	}
	disabled := true
	// The folder is present, but a read user layer disables the Copilot AI.
	scan := platform.CopilotScan{
		ConfigDir: userDir,
		Layers: []platform.CopilotLayer{{
			Kind: platform.CopilotLayerUser, Status: platform.MCPSourceRead,
			DisableAIFeatures: &disabled,
		}},
	}
	if copilotPresent(scan, home, agentdirs.Env(noEnv)) {
		t.Error("Copilot counted as present with chat.disableAIFeatures: true (B5)")
	}

	// With the AI enabled again (false), the folder counts as before.
	enabled := false
	scan.Layers[0].DisableAIFeatures = &enabled
	if !copilotPresent(scan, home, agentdirs.Env(noEnv)) {
		t.Error("Copilot not detected by its globalStorage folder when the AI is enabled")
	}
}

func TestCopilotGlobalStoragePresent(t *testing.T) {
	tmp := t.TempDir()
	if copilotGlobalStoragePresent(tmp) {
		t.Error("empty user dir reported a Copilot globalStorage folder")
	}
	if copilotGlobalStoragePresent("") {
		t.Error("empty path reported present")
	}
	// A plain file (not a dir) at the path must not count.
	gs := filepath.Join(tmp, "globalStorage")
	if err := os.MkdirAll(gs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gs, "github.copilot"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if copilotGlobalStoragePresent(tmp) {
		t.Error("a file named github.copilot counted as the extension folder")
	}
	if err := os.MkdirAll(filepath.Join(gs, "github.copilot-chat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !copilotGlobalStoragePresent(tmp) {
		t.Error("github.copilot-chat folder not detected")
	}
}
