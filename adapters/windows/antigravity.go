package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// ListAntigravity reads the Antigravity CLI's configuration (BV-140, CTO-16
// Antigravity §4), read only: ~/.gemini/antigravity-cli/settings.json, the user
// and project hooks, and the credential files by fingerprint. The MCP servers
// are read by ListMCPServers (client antigravity-cli).
func (w *WindowsPlatform) ListAntigravity(ctx context.Context, repoRoots []string) (platform.AntigravityScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.AntigravityScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadAntigravity(home, repoRoots), nil
}

// ReadAntigravity is ListAntigravity with the home folder and repo roots given,
// for tests. It is a pure function.
func ReadAntigravity(home string, repoRoots []string) platform.AntigravityScan {
	gemini := agentdirs.AntigravityDir(home)
	scan := platform.AntigravityScan{ConfigDir: gemini}

	// settings.json (CLI): the permission posture.
	scan.Settings = readAntigravitySettings(filepath.Join(agentdirs.AntigravityCLIDir(home), "settings.json"))

	// Hooks: the user's ~/.gemini/config/hooks.json and each repo's
	// <root>/.agents/hooks.json (an agent_executable).
	if p := filepath.Join(gemini, "config", "hooks.json"); fileExists(p) {
		scan.Hooks = append(scan.Hooks, platform.AntigravityHook{Path: p})
	}
	for _, root := range repoRoots {
		if p := filepath.Join(root, ".agents", "hooks.json"); fileExists(p) {
			scan.Hooks = append(scan.Hooks, platform.AntigravityHook{Path: p, Root: root})
		}
	}

	// Credentials by fingerprint: the MCP OAuth tokens and the CLI sandbox proxy
	// password (CTO-16 Antigravity §3). Never the value.
	scan.Creds = antigravityCreds(home, scan.Settings)

	// Present (the scored CLI): its data folder, its settings.json, or the shared
	// config it reads (CTO-16 Antigravity §1–§2).
	scan.Present = isDirExists(agentdirs.AntigravityCLIDir(home)) ||
		scan.Settings.Status == platform.MCPSourceRead ||
		fileExists(filepath.Join(gemini, "config", "mcp_config.json"))
	return scan
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDirExists(p string) bool { info, err := os.Stat(p); return err == nil && info.IsDir() }

// readAntigravitySettings reads the CLI settings.json (JSON), bounded and
// strict (invariant 6).
func readAntigravitySettings(path string) platform.AntigravitySettings {
	s := platform.AntigravitySettings{Path: path}
	raw, status, reason := readBounded(path)
	s.Status, s.Reason = status, reason
	if status != platform.MCPSourceRead {
		return s
	}
	var doc struct {
		ToolPermission          string `json:"toolPermission"`
		AgentMode               string `json:"agentMode"`
		ArtifactReviewPolicy    string `json:"artifactReviewPolicy"`
		AllowNonWorkspaceAccess bool   `json:"allowNonWorkspaceAccess"`
		Permissions             struct {
			Allow []string `json:"allow"`
			Ask   []string `json:"ask"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		s.Status, s.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return s
	}
	s.ToolPermission, s.AgentMode, s.ArtifactReviewPolicy = doc.ToolPermission, doc.AgentMode, doc.ArtifactReviewPolicy
	s.AllowNonWorkspaceAccess = doc.AllowNonWorkspaceAccess
	s.Allow, s.Ask, s.Deny = doc.Permissions.Allow, doc.Permissions.Ask, doc.Permissions.Deny
	return s
}

// antigravityCreds reads the CLI's plaintext credentials by fingerprint only
// (CTO-16 Antigravity §3): ~/.gemini/antigravity/mcp_oauth_tokens.json (every
// string leaf) and settings.json's sandboxProxy.password.
func antigravityCreds(home string, settings platform.AntigravitySettings) []platform.EnvVar {
	var out []platform.EnvVar
	tokens := filepath.Join(agentdirs.AntigravityAppDataDir(home), "mcp_oauth_tokens.json")
	if raw, status, _ := readBounded(tokens); status == platform.MCPSourceRead {
		var doc any
		if json.Unmarshal(raw, &doc) == nil {
			keys := map[string]string{}
			antigravityWalkJSON("", doc, keys)
			names := make([]string, 0, len(keys))
			for k := range keys {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				out = append(out, platform.EnvVar{Key: "mcp_oauth_tokens.json:" + k, Value: secrets.NewSecretValue(keys[k])})
			}
		}
	}
	// settings.json sandboxProxy.password (read again as a map: the typed decode
	// above does not carry it).
	if settings.Status == platform.MCPSourceRead {
		if raw, status, _ := readBounded(settings.Path); status == platform.MCPSourceRead {
			var m struct {
				SandboxProxy struct {
					Password string `json:"password"`
				} `json:"sandboxProxy"`
			}
			if json.Unmarshal(raw, &m) == nil && strings.TrimSpace(m.SandboxProxy.Password) != "" {
				out = append(out, platform.EnvVar{Key: "settings.json:sandboxProxy.password", Value: secrets.NewSecretValue(m.SandboxProxy.Password)})
			}
		}
	}
	return out
}

func antigravityWalkJSON(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) != "" {
			out[prefix] = t
		}
	case map[string]any:
		for k, val := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			antigravityWalkJSON(p, val, out)
		}
	}
}
