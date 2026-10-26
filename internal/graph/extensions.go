package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// editorVSCode is the editor of the extensions the adapter lists. It is in
// the extension's key so that another editor's extensions (Cursor's, A6)
// can come in without changing ids.
const editorVSCode = "vscode"

// AddExtensions adds to f one extension agent per installed VS Code
// extension the knowledge base marks as an AI agent (decision 2026-10-26,
// agentes y MCP nuevos, A3), with its product code as platform and dir, the
// extensions folder, as config_path. Any other extension gives no node. The
// version does not enter the id: an update is the same agent. It is a pure
// function.
func AddExtensions(f Fragment, exts []platform.VSCodeExtension, dir string, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	b := newFragmentBuilder()
	for _, e := range exts {
		known, ok := knowledge.AgentExtensions[strings.ToLower(e.ID)]
		if !ok {
			continue
		}
		b.node(Node{
			ID: ExtensionID(editorVSCode, e.ID), Kind: KindAgent, Type: AgentExtension,
			Name: known.Name, Platform: known.Product, ConfigPath: dir, DiscoveredAt: seen,
		})
	}
	return Merge(f, b.fragment())
}

// ExtensionID derives an extension agent's id from its natural key (A3):
// "agent_ext_" + sha256("extension", editor, lowercase extension id)[:16].
func ExtensionID(editor, extensionID string) string {
	return "agent_ext_" + keyHash(AgentExtension, editor, strings.ToLower(extensionID))
}
