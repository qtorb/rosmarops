package knowledge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The knowledge base of the orchestrators of CTO-76 read by BV-268: Paseo,
// Claude Squad and Orca. For each, which agents it starts, in which of its
// modes it skips their approvals, whether they get its environment, and its
// secrets known by where they are. Every fact keeps its source, a URL at the
// tool's latest release when it was checked, and that date.

// Product codes.
const (
	PaseoProduct       = "paseo"
	ClaudeSquadProduct = "claude-squad"
	OrcaProduct        = "orca"
)

// OrchestratorsVersion is the version of orchestrators.json.
const OrchestratorsVersion = "2026-11-20"

//go:embed orchestrators.json
var orchestratorsJSON []byte

// OrchestratorsBase is orchestrators.json.
type OrchestratorsBase struct {
	Version  string                `json:"version"`
	Products []OrchestratorProduct `json:"products"`
}

// OrchestratorProduct is one orchestrator at one release.
type OrchestratorProduct struct {
	Product      string               `json:"product"`
	Release      string               `json:"release"`
	SourceCommit string               `json:"source_commit"`
	Launches     []OrchestratorLaunch `json:"launches"`
	PathSecrets  []PaperclipSecret    `json:"path_secrets"`
	Defaults     []PaperclipDefault   `json:"defaults"`
}

// OrchestratorLaunch is how the orchestrator starts one agent product.
// SkipsModes are its own mode values (Paseo's modeId, Claude Squad's
// auto_yes) in which the agent's approvals are skipped; DefaultOverride is
// the approval override of its factory default, for an orchestrator whose
// settings are not readable at rest (Orca). ProgramMatch is how Claude
// Squad compares its program string to the agent (exact or prefix).
type OrchestratorLaunch struct {
	ID              string   `json:"id"`
	Agent           string   `json:"agent"`
	SkipsModes      []string `json:"skips_modes,omitempty"`
	DefaultOverride string   `json:"default_override,omitempty"`
	ProgramMatch    string   `json:"program_match,omitempty"`
	EnvPassthrough  bool     `json:"env_passthrough"`
	SourceURL       string   `json:"source_url"`
	Verified        string   `json:"verified"`
}

// Skips reports whether mode is one in which l skips the agent's approvals.
func (l OrchestratorLaunch) Skips(mode string) bool {
	for _, m := range l.SkipsModes {
		if m == mode {
			return true
		}
	}
	return false
}

// LoadOrchestrators returns the embedded base, decoded strictly.
func LoadOrchestrators() (OrchestratorsBase, error) {
	return decodeOrchestrators(orchestratorsJSON)
}

func decodeOrchestrators(raw []byte) (OrchestratorsBase, error) {
	var b OrchestratorsBase
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return OrchestratorsBase{}, fmt.Errorf("knowledge base orchestrators: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return OrchestratorsBase{}, fmt.Errorf("knowledge base orchestrators: trailing data")
	}
	if b.Version != OrchestratorsVersion {
		return OrchestratorsBase{}, fmt.Errorf("knowledge base orchestrators: version %q, want %q", b.Version, OrchestratorsVersion)
	}
	fail := func(f string, a ...any) (OrchestratorsBase, error) {
		return OrchestratorsBase{}, fmt.Errorf("knowledge base orchestrators: "+f, a...)
	}
	products := map[string]bool{}
	for _, p := range b.Products {
		switch p.Product {
		case PaseoProduct, ClaudeSquadProduct, OrcaProduct:
		default:
			return fail("unknown product %q", p.Product)
		}
		if products[p.Product] || p.Release == "" || p.SourceCommit == "" {
			return fail("%s: duplicate, or without release or commit", p.Product)
		}
		products[p.Product] = true
		sourced := func(what, url, verified string) error {
			if !strings.Contains(url, "/blob/"+p.Release+"/") || !strings.HasPrefix(url, "https://github.com/") || !isoDate.MatchString(verified) {
				return fmt.Errorf("knowledge base orchestrators: %s %s needs a source at %s and a YYYY-MM-DD date", p.Product, what, p.Release)
			}
			return nil
		}
		ids := map[string]bool{}
		for _, l := range p.Launches {
			if l.ID == "" || l.Agent == "" || ids[l.ID] {
				return fail("%s: empty or duplicate launch %q", p.Product, l.ID)
			}
			ids[l.ID] = true
			switch l.DefaultOverride {
			case "", "skips_all", "keeps_agent_settings", "unknown":
			default:
				return fail("%s/%s: default override %q", p.Product, l.ID, l.DefaultOverride)
			}
			switch l.ProgramMatch {
			case "", "exact", "prefix":
			default:
				return fail("%s/%s: program match %q", p.Product, l.ID, l.ProgramMatch)
			}
			if err := sourced("launch "+l.ID, l.SourceURL, l.Verified); err != nil {
				return OrchestratorsBase{}, err
			}
		}
		for _, s := range p.PathSecrets {
			if s.ID == "" || ids["secret/"+s.ID] || s.File == "" || (s.Pointer != "" && !strings.HasPrefix(s.Pointer, "/")) {
				return fail("%s: secret %q", p.Product, s.ID)
			}
			ids["secret/"+s.ID] = true
			if err := sourced("secret "+s.ID, s.SourceURL, s.Verified); err != nil {
				return OrchestratorsBase{}, err
			}
		}
		for _, d := range p.Defaults {
			if d.ID == "" || ids["default/"+d.ID] {
				return fail("%s: default %q", p.Product, d.ID)
			}
			ids["default/"+d.ID] = true
			if err := sourced("default "+d.ID, d.SourceURL, d.Verified); err != nil {
				return OrchestratorsBase{}, err
			}
		}
	}
	return b, nil
}

// Product returns one product's entry.
func (b OrchestratorsBase) Product(product string) (OrchestratorProduct, bool) {
	for _, p := range b.Products {
		if p.Product == product {
			return p, true
		}
	}
	return OrchestratorProduct{}, false
}

// Launch is the product's entry for agent (a RosmarOps agent product code).
func (p OrchestratorProduct) Launch(agent string) (OrchestratorLaunch, bool) {
	for _, l := range p.Launches {
		if l.Agent == agent {
			return l, true
		}
	}
	return OrchestratorLaunch{}, false
}

// LaunchByID is the product's entry by its own id (Paseo's provider, Claude
// Squad's program).
func (p OrchestratorProduct) LaunchByID(id string) (OrchestratorLaunch, bool) {
	for _, l := range p.Launches {
		if l.ID == id {
			return l, true
		}
	}
	return OrchestratorLaunch{}, false
}

// Ref is the kb: source of one of the product's facts.
func (p OrchestratorProduct) Ref(id string) string {
	return "kb:" + p.Product + "/" + id + "@" + OrchestratorsVersion
}
