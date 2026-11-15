package pipeline

import (
	"slices"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Factory is in with every agent at its product defaults (BV-235, R1; CTO-48
// §3.2): the same computer with the person's settings taken out.
//
// Taken out, for every agent: permission rules, modes, the env variables that
// are settings (Claude Code's model provider), the managed policy, VS Code's
// Claude Code mode, Cursor's sandbox.json, terminalAllowlist, mcpAllowlist and
// cli-config.json, and the approval, sandbox and rule settings of Codex, Grok
// Build, GitHub Copilot, Gemini CLI, Antigravity CLI, Cline and Hermes Agent.
//
// Kept: the credentials found (env blocks, MCP server env and headers, API
// keys), the repositories and roots, the MCP servers and their .mcp.json
// approvals, the extensions, hooks and other programs the settings run, the
// instruction files, and the project trust.
//
// Factory never changes in: every slice it edits is a copy.
func Factory(in Input) Input {
	out := in
	out.Settings = FactorySettings(in.Settings)
	out.Cursor = factoryCursor(in.Cursor)
	out.Hermes = FactoryHermes(in.Hermes)
	out.Codex = FactoryCodex(in.Codex)
	out.Grok = FactoryGrok(in.Grok)
	out.Copilot = factoryCopilot(in.Copilot)
	out.Antigravity = factoryAntigravity(in.Antigravity)
	out.Gemini = factoryGemini(in.Gemini)
	out.Cline = FactoryCline(in.Cline)
	return out
}

// FactorySettings is Claude Code's settings scan at its product defaults:
// every file keeps its place, status, env credentials, programs and .mcp.json
// approvals, without permissions; the managed sources keep only their env
// (the credentials, which the engine takes from every admin source) and carry
// no policy; VS Code's mode is gone.
func FactorySettings(s platform.ClaudeSettingsScan) platform.ClaudeSettingsScan {
	providers := rules.ProviderEnvKeys()
	file := func(f platform.ClaudeSettingsFile) platform.ClaudeSettingsFile {
		f.Permissions = platform.ClaudePermissions{}
		f.AllowManagedPermissionRulesOnly = false
		f.ManagedSourcesBehavior = ""
		f.HasPolicyKey = false
		var env []platform.EnvVar
		for _, ev := range f.Env {
			if !slices.Contains(providers, ev.Key) {
				env = append(env, ev)
			}
		}
		f.Env = env
		return f
	}
	out := s
	out.Files = make([]platform.ClaudeSettingsFile, len(s.Files))
	for i, f := range s.Files {
		out.Files[i] = file(f)
	}
	out.Managed = make([]platform.ManagedSource, len(s.Managed))
	for i, m := range s.Managed {
		files := make([]platform.ClaudeSettingsFile, len(m.Files))
		for j, f := range m.Files {
			f = file(f)
			f.Executables, f.HookScripts, f.OwnHooks = nil, nil, nil // policy programs go with the policy
			files[j] = f
		}
		out.Managed[i] = platform.ManagedSource{Kind: m.Kind, Files: files}
	}
	if len(s.Managed) == 0 {
		out.Managed = s.Managed
	}
	out.VSCode = nil
	return out
}

func factoryCursor(s platform.CursorScan) platform.CursorScan {
	out := s
	out.Files = make([]platform.CursorFile, len(s.Files))
	for i, f := range s.Files {
		f.TerminalAllowlist, f.HasTerminalAllowlist = nil, false
		f.MCPAllowlist, f.HasMCPAllowlist = nil, false
		f.SandboxType, f.ReadBoundary, f.AdditionalReadwritePaths = "", "", nil
		f.CLIAllow, f.CLIDeny = nil, nil
		out.Files[i] = f
	}
	if s.Files == nil {
		out.Files = nil
	}
	return out
}

// FactoryHermes is the Hermes Agent scan without its approval mode, its
// write-protected instructions setting and its terminal backend.
func FactoryHermes(s platform.HermesScan) platform.HermesScan {
	out := s
	out.Configs = make([]platform.HermesConfig, len(s.Configs))
	for i, c := range s.Configs {
		c.ApprovalMode, c.ProtectedInstructionFiles, c.TerminalBackend = "", nil, ""
		out.Configs[i] = c
	}
	if s.Configs == nil {
		out.Configs = nil
	}
	return out
}

// FactoryCodex is the Codex scan without the requirements (managed) layer and
// without the approval, sandbox, rule and computer-use settings of the other
// layers; project trust, MCP servers and bearer tokens stay.
func FactoryCodex(s platform.CodexScan) platform.CodexScan {
	out := s
	out.Layers = nil
	for _, l := range s.Layers {
		if l.Kind == platform.CodexLayerRequirements {
			continue
		}
		l.ApprovalPolicy, l.HasApprovalPolicy, l.ApprovalsReviewer = "", false, ""
		l.SandboxMode, l.DefaultPermissions, l.NetworkAccess = "", "", nil
		l.WindowsSandbox, l.HasWindowsSandbox = "", false
		l.IgnoreDefaultExcludes, l.HasRulesFiles = nil, false
		l.ComputerUse, l.CUFeatureDisabled, l.CUPersistentApprovalDisabled = nil, false, false
		out.Layers = append(out.Layers, l)
	}
	return out
}

// FactoryGrok is the Grok Build scan without the managed and requirements
// layers and without the permission, sandbox and compat settings of the
// others; the trusted folders, MCP servers and keys stay.
func FactoryGrok(s platform.GrokScan) platform.GrokScan {
	out := s
	out.FolderTrustOff = false
	out.Layers = nil
	for _, l := range s.Layers {
		if l.Kind == platform.GrokLayerManaged || l.Kind == platform.GrokLayerRequirements {
			continue
		}
		l.PermissionMode, l.HasPermissionMode, l.Yolo = "", false, false
		l.SandboxProfile = ""
		l.Allow, l.Deny, l.Ask = nil, nil, nil
		l.Compat, l.ClaudeImported, l.DisableBypass = nil, false, false
		l.FolderTrustEnabled = nil
		l.Rules = nil
		out.Layers = append(out.Layers, l)
	}
	return out
}

func factoryCopilot(s platform.CopilotScan) platform.CopilotScan {
	out := s
	out.Layers = nil
	for _, l := range s.Layers {
		if l.Kind == platform.CopilotLayerManaged {
			continue
		}
		out.Layers = append(out.Layers, platform.CopilotLayer{Kind: l.Kind, Path: l.Path, Root: l.Root, Status: l.Status, Reason: l.Reason})
	}
	return out
}

func factoryAntigravity(s platform.AntigravityScan) platform.AntigravityScan {
	out := s
	st := s.Settings
	out.Settings = platform.AntigravitySettings{Path: st.Path, Status: st.Status, Reason: st.Reason}
	return out
}

func factoryGemini(s platform.GeminiScan) platform.GeminiScan {
	out := s
	out.Layers = nil
	for _, l := range s.Layers {
		if l.Kind == platform.GeminiLayerSystem || l.Kind == platform.GeminiLayerSystemDefaults {
			// The system layers are the managed policy; their MCP servers stay.
			if len(l.MCP) == 0 {
				continue
			}
			l = platform.GeminiLayer{Kind: l.Kind, Path: l.Path, Root: l.Root, Status: l.Status, Reason: l.Reason, MCP: l.MCP}
			out.Layers = append(out.Layers, l)
			continue
		}
		l.DefaultApprovalMode, l.ToolsAllowed, l.DisableYoloMode = "", nil, false
		out.Layers = append(out.Layers, l)
	}
	return out
}

// FactoryCline is the Cline scan without its auto-approve and YOLO settings.
func FactoryCline(s platform.ClineScan) platform.ClineScan {
	out := s
	out.HasAutoApproval = false
	out.ReadFiles, out.EditFiles, out.ExecuteSafeCommands, out.UseBrowser, out.UseMcp = false, false, false, false, false
	out.Yolo, out.YoloKey = false, ""
	return out
}
