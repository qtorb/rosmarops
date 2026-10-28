// Package intel holds the threat-data snapshot embedded in the binary and the
// local, no-network cross against what the scan reads (BV-100, F0a). The
// snapshot is validated against intel.v1 at load time; an invalid snapshot is
// a hard error, so a build can never report green with broken threat data.
package intel

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// Ecosystems.
const (
	EcosystemPyPI = "pypi"
	EcosystemNPM  = "npm"
)

//go:embed data/intel.json
var embedded []byte

// Snapshot is a whole intel.v1 document.
type Snapshot struct {
	Version     string    `json:"version"`
	PublishedAt string    `json:"published_at"`
	Packages    []Package `json:"packages"`
}

// Package is one malicious-package entry.
type Package struct {
	ID        string   `json:"id"`
	Ecosystem string   `json:"ecosystem"`
	Package   string   `json:"package"`
	Versions  []string `json:"versions"`
	Artifacts []string `json:"artifacts,omitempty"`
	Aliases   []string `json:"aliases,omitempty"`
	Source    Source   `json:"source"`
}

// Source is where an entry was verified.
type Source struct {
	URL        string `json:"url"`
	AccessedAt string `json:"accessed_at"`
}

// Match is a confirmed hit: the entry and the normalized package name and
// exact version that matched. It carries no value read from the machine.
type Match struct {
	IntelID   string
	Ecosystem string
	Package   string // normalized
	Version   string
}

var compiledSchema *schemaval.Schema

func schema() (*schemaval.Schema, error) {
	if compiledSchema != nil {
		return compiledSchema, nil
	}
	s, err := schemaval.Compile(schemas.IntelSchema)
	if err != nil {
		return nil, fmt.Errorf("compile intel schema: %w", err)
	}
	compiledSchema = s
	return compiledSchema, nil
}

// Load parses and validates the embedded snapshot. It fails on any schema
// violation (fail-secure: the caller aborts rather than report green with
// broken threat data).
func Load() (*Snapshot, error) {
	return parse(embedded)
}

func parse(raw []byte) (*Snapshot, error) {
	s, err := schema()
	if err != nil {
		return nil, err
	}
	res, err := s.Validate(raw)
	if err != nil {
		return nil, fmt.Errorf("validate intel snapshot: %w", err)
	}
	if !res.Valid() {
		var b strings.Builder
		for i, e := range res.Errors() {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(e.String())
		}
		return nil, fmt.Errorf("intel snapshot invalid: %s", b.String())
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode intel snapshot: %w", err)
	}
	return &snap, nil
}

// Matcher crosses package coordinates against a snapshot. It is built once and
// is safe for concurrent reads.
type Matcher struct {
	version     string
	publishedAt string
	// byKey maps "<ecosystem>\x00<normalized name>\x00<version>" to the entry.
	byKey map[string]Package
	// artifacts maps "<ecosystem>\x00<normalized name>" to the entry's
	// artifact file names.
	artifacts map[string][]string
}

// NewMatcher builds a Matcher from a snapshot.
func NewMatcher(snap *Snapshot) *Matcher {
	m := &Matcher{
		version:     snap.Version,
		publishedAt: snap.PublishedAt,
		byKey:       map[string]Package{},
		artifacts:   map[string][]string{},
	}
	for _, p := range snap.Packages {
		name := Normalize(p.Ecosystem, p.Package)
		for _, v := range p.Versions {
			m.byKey[p.Ecosystem+"\x00"+name+"\x00"+v] = p
		}
		if len(p.Artifacts) > 0 {
			m.artifacts[p.Ecosystem+"\x00"+name] = p.Artifacts
		}
	}
	return m
}

// Version is the snapshot version (for the report stamp).
func (m *Matcher) Version() string { return m.version }

// PublishedAt is the snapshot publication date (for the report stamp).
func (m *Matcher) PublishedAt() string { return m.publishedAt }

// Match reports whether ecosystem+name+version is a known malicious release.
// name is normalized for its ecosystem; version must match exactly. ok is
// false when the version is empty (an unpinned install is never a match).
func (m *Matcher) Match(ecosystem, name, version string) (Match, bool) {
	if m == nil || version == "" {
		return Match{}, false
	}
	norm := Normalize(ecosystem, name)
	p, ok := m.byKey[ecosystem+"\x00"+norm+"\x00"+version]
	if !ok {
		return Match{}, false
	}
	return Match{IntelID: p.ID, Ecosystem: ecosystem, Package: norm, Version: version}, true
}

// Artifacts are the artifact file names a matched entry drops, for presence
// checks alongside a dist-info match. Empty when none.
func (m *Matcher) Artifacts(ecosystem, name string) []string {
	if m == nil {
		return nil
	}
	return m.artifacts[ecosystem+"\x00"+Normalize(ecosystem, name)]
}

var pep503 = regexp.MustCompile(`[-_.]+`)

// Normalize is the comparison form of a package name for its ecosystem. PyPI
// follows PEP 503 (lowercase, runs of - _ . collapse to a single -). npm names
// are lowercased (the public registry is case-insensitive on case only).
func Normalize(ecosystem, name string) string {
	name = strings.TrimSpace(name)
	switch ecosystem {
	case EcosystemPyPI:
		return pep503.ReplaceAllString(strings.ToLower(name), "-")
	case EcosystemNPM:
		return strings.ToLower(name)
	default:
		return name
	}
}
