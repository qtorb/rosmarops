// Package agents is the minimal product registry of BV-208 (CTO-24 E1, minimal
// version): one entry per scored product with the configuration sources that
// bear on its permissions, each with its layer and format. It feeds the
// complexity axis (6) of the exposure potential (CTO-26 §3, §6). It is not the
// full CTO-24 E1 registry (readers, detection, graph spec): only what the
// complexity measure needs.
//
// Anti-inflation (CTO-26 §3): a source is declared only when the reader opens
// it on the full machine (E8). tests/integration checks the declared count
// against what TestFullMachinePipeline actually reads, so the complexity figure
// cannot be padded with sources the engine never opens.
package agents

import "sort"

// Layer is where a configuration source sits (CTO-26 §3).
type Layer string

const (
	LayerSystem  Layer = "system"
	LayerManaged Layer = "managed"
	LayerUser    Layer = "user"
	LayerProject Layer = "project"
	LayerIDE     Layer = "ide"
	LayerEnv     Layer = "env"
	LayerFlag    Layer = "flag"
)

// Format is a configuration source's on-disk format (CTO-26 §3).
type Format string

const (
	FormatJSON     Format = "json"
	FormatJSONC    Format = "jsonc"
	FormatTOML     Format = "toml"
	FormatYAML     Format = "yaml"
	FormatEnv      Format = "env"
	FormatRegistry Format = "registry"
	FormatSQLite   Format = "sqlite"
)

// ConfigSource is one declared configuration source of a product.
type ConfigSource struct {
	Layer              Layer
	Format             Format
	AffectsPermissions bool
}

// Entry is a product's registry entry: its configuration sources. Complete is
// true only when Sources lists every source the product's official
// documentation gives that decides permissions (CTO-31 M1, §a); until then the
// complexity axis has no data (registry_missing), never a level computed from
// a partial list.
type Entry struct {
	Product  string
	Sources  []ConfigSource
	Complete bool
}

// registry is the nine scored products. Each source listed is one the reader
// opens on the full-machine fixture today (grounded in TestFullMachinePipeline's
// coverage.sources_read); the complexity it yields grows as that fixture
// exercises more of each product's layers. Cursor opens no config file on the
// current full machine, so it declares none yet. No entry is Complete yet
// (CTO-31 M1): the lists are the fixture's, not the products' documented
// sources, so every complexity is «no data» until the full registry (§a).
var registry = []Entry{
	{Product: "claude-code", Sources: []ConfigSource{
		{LayerUser, FormatJSON, true}, // ~/.claude/settings.json
	}},
	{Product: "cursor", Sources: nil},
	{Product: "codex", Sources: []ConfigSource{
		{LayerUser, FormatTOML, true},    // ~/.codex/config.toml
		{LayerProject, FormatTOML, true}, // <repo>/.codex/config.toml
	}},
	{Product: "github-copilot", Sources: []ConfigSource{
		{LayerUser, FormatJSONC, true}, // <VS Code user>/settings.json
	}},
	{Product: "gemini-cli", Sources: []ConfigSource{
		{LayerUser, FormatJSON, true}, // ~/.gemini/settings.json
	}},
	{Product: "cline", Sources: []ConfigSource{
		{LayerUser, FormatJSON, true}, // ~/.cline/data/globalState.json
	}},
	{Product: "grok-build", Sources: []ConfigSource{
		{LayerUser, FormatTOML, true}, // ~/.grok/config.toml
	}},
	{Product: "hermes", Sources: []ConfigSource{
		{LayerUser, FormatYAML, true}, // %LOCALAPPDATA%/hermes/config.yaml
	}},
	{Product: "antigravity-cli", Sources: []ConfigSource{
		{LayerUser, FormatJSON, true}, // ~/.gemini/antigravity-cli/settings.json
	}},
}

// All returns the registry entries, one per scored product.
func All() []Entry { return registry }

// Entry returns the entry for a product and whether it exists.
func EntryOf(product string) (Entry, bool) {
	for _, e := range registry {
		if e.Product == product {
			return e, true
		}
	}
	return Entry{}, false
}

// AffectsSources are the product's configuration sources that bear on its
// permissions — the only ones the complexity counts.
func (e Entry) AffectsSources() []ConfigSource {
	var out []ConfigSource
	for _, s := range e.Sources {
		if s.AffectsPermissions {
			out = append(out, s)
		}
	}
	return out
}

// Complexity is the product's complexity points and its 0–3 level (CTO-26 §3):
// points = (sources that affect permissions) + (distinct layers) + (distinct
// formats), counted over the affects-permissions sources; cuts ≤3→0, 4–6→1,
// 7–9→2, ≥10→3.
func (e Entry) Complexity() (points, level int) {
	srcs := e.AffectsSources()
	layers := map[Layer]bool{}
	formats := map[Format]bool{}
	for _, s := range srcs {
		layers[s.Layer] = true
		formats[s.Format] = true
	}
	points = len(srcs) + len(layers) + len(formats)
	switch {
	case points <= 3:
		level = 0
	case points <= 6:
		level = 1
	case points <= 9:
		level = 2
	default:
		level = 3
	}
	return points, level
}

// Products lists the registry's product codes, sorted.
func Products() []string {
	out := make([]string, 0, len(registry))
	for _, e := range registry {
		out = append(out, e.Product)
	}
	sort.Strings(out)
	return out
}
