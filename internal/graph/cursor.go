package graph

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Rules and target scopes of Cursor's configured capabilities (decision
// 2026-10-25, Cursor, §4). The rule names the key and keeps the entry
// literal; terminalAllowlist and mcpAllowlist entries are prefixes
// (https://cursor.com/docs/reference/permissions.md).
const (
	cursorTerminalRule     = "terminalAllowlist:"
	cursorMCPRule          = "mcpAllowlist:"
	cursorReadBoundaryRule = "readBoundary=workspace"
	cursorInsecureNoneRule = "type=insecure_none"
	cursorReadwriteRule    = "additionalReadwritePaths="

	// ScopeSandboxNone is the target of a sandbox.json "type":
	// "insecure_none", which counts as no sandbox
	// (https://cursor.com/docs/reference/sandbox.md).
	ScopeSandboxNone = "sandbox_insecure_none"

	// CLI layers: the CLI's rules are recorded, not inferred (K10).
	cursorCLILayer = "cli:"
)

// cliTools maps the tools of the Cursor CLI's permission rules
// (https://cursor.com/docs/cli/reference/permissions.md) to capability
// types.
var cliTools = map[string]string{
	"Shell": CapExecuteShell, "Read": CapReadFiles, "Write": CapWriteFiles, "WebFetch": CapNetwork, "Mcp": CapInvokeMCPTool,
}

// FromCursor builds Cursor's fragment (decision 2026-10-25, Cursor, K10 and
// annex 2): its ai_client when ~/.cursor exists; one agent_config resource
// per permissions, sandbox and CLI file read and one agent_executable per
// hooks.json; a configured capability per terminalAllowlist and
// mcpAllowlist entry, per sandbox setting RosmarOps uses and per CLI rule;
// and the hooks of Claude Code's settings as agent_executable resources of
// Cursor too, one resource per agent that loads them (K7). Failures go to
// coverage. It is a pure function.
func FromCursor(scan platform.CursorScan, settings platform.ClaudeSettingsScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Exists {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	patterns := secrets.BuiltinPatterns()
	client := clientID(knowledge.CursorProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.CursorProduct),
		Platform: knowledge.CursorProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})
	for _, f := range scan.Files {
		switch f.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: f.Path, Reason: f.Reason})
			continue
		case platform.MCPSourceMissing:
			continue
		}
		b.read[canonicalPath(f.Path)] = f.Path
		if f.Kind == platform.CursorHooks {
			b.node(Node{
				ID: "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(f.Path)), Kind: KindResource,
				Type: ResourceAgentExecutable, Path: f.Path, Configures: client, DiscoveredAt: seen,
			})
			continue
		}
		b.node(Node{ID: configResourceID(f.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: f.Path, Configures: client, DiscoveredAt: seen})
		layer := cursorLayer(f)
		c := capContext{b: b, client: client, file: f.Path, appliesIn: layer}
		switch f.Kind {
		case platform.CursorPermissions:
			for i, e := range f.TerminalAllowlist {
				c.add(Node{Type: CapExecuteShell, Approval: ApprovalAllow, TargetScope: secrets.RedactMatches(e, patterns), InForce: true},
					cursorTerminalRule+e, "/terminalAllowlist/"+strconv.Itoa(i), patterns)
			}
			for i, e := range f.MCPAllowlist {
				c.add(Node{Type: CapInvokeMCPTool, Approval: ApprovalAllow, TargetScope: secrets.RedactMatches(e, patterns), InForce: true},
					cursorMCPRule+e, "/mcpAllowlist/"+strconv.Itoa(i), patterns)
			}
		case platform.CursorSandbox:
			if f.ReadBoundary == "workspace" {
				c.add(Node{Type: CapReadFiles, Approval: ApprovalDeny, TargetScope: outsideWorkingDirectoryScope, InForce: true},
					cursorReadBoundaryRule, "/readBoundary", patterns)
			}
			if f.SandboxType == "insecure_none" {
				c.add(Node{Type: CapWriteFiles, Approval: ApprovalAllow, TargetScope: ScopeSandboxNone, InForce: true},
					cursorInsecureNoneRule, "/type", patterns)
			}
			for i, p := range f.AdditionalReadwritePaths {
				c.add(Node{Type: CapWriteFiles, Approval: ApprovalAllow, TargetScope: secrets.RedactMatches(p, patterns), InForce: true},
					cursorReadwriteRule+p, "/additionalReadwritePaths/"+strconv.Itoa(i), patterns)
			}
		case platform.CursorCLI:
			cli := capContext{b: b, client: client, file: f.Path, appliesIn: cursorCLILayer + layer}
			for _, list := range []struct {
				approval string
				key      string
				rules    []string
			}{{ApprovalAllow, "allow", f.CLIAllow}, {ApprovalDeny, "deny", f.CLIDeny}} {
				for i, raw := range list.rules {
					rule := secrets.RedactMatches(raw, patterns)
					tool, spec, parens, ok := splitRule(rule)
					typ, known := cliTools[tool]
					if !ok || !known {
						continue
					}
					target := scopeAll
					if parens {
						target = spec
					}
					cli.add(Node{Type: typ, Approval: list.approval, TargetScope: target, InForce: true}, rule,
						"/permissions/"+list.key+"/"+strconv.Itoa(i), patterns)
				}
			}
		}
	}
	// Claude Code's hooks are Cursor's executables too (K7; third-party
	// imports, annex): user, project and local settings.
	for _, file := range settings.Files {
		if file.Status != platform.MCPSourceRead || file.Scope == platform.MCPScopeManaged {
			continue
		}
		for _, ptr := range file.Executables {
			if !strings.HasPrefix(ptr, "/hooks/") {
				continue
			}
			b.node(Node{
				ID:   CursorImportedHookID(file.ConfigPath+"#"+ptr, client),
				Kind: KindResource, Type: ResourceAgentExecutable,
				Path: file.ConfigPath + "#" + ptr, Configures: client, DiscoveredAt: seen,
			})
		}
	}
	return b.fragment()
}

// CursorImportedHookID is the id of a Claude Code hook as Cursor's
// executable: keyed by path and agent, as AGENTS.md is per agent.
func CursorImportedHookID(path, client string) string {
	return "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(path), client)
}

// cursorLayer is the applies_in of a Cursor file: user, managed for the
// enterprise hooks file, or project:<root>.
func cursorLayer(f platform.CursorFile) string {
	switch f.Scope {
	case platform.CursorScopeUser:
		return platform.MCPScopeUser
	case platform.CursorScopeEnterprise:
		return platform.MCPScopeManaged
	}
	return platform.MCPScopeProject + ":" + canonicalPath(f.Root)
}

// cursorEnvRef is a Cursor value that names an environment variable: it is
// not resolved at rest, and holds no secret (K10).
var cursorEnvRef = regexp.MustCompile(`^\$\{env:[^}]*\}$`)
