package knowledge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Paperclip is the knowledge base of the Paperclip orchestrator (CTO-75,
// CTO-76): which agents it starts and how, which of its files hold secrets,
// its defaults, and the version that fixes its known flaws. Its shape is not a
// capability base's: Paperclip is an orchestrator, so what matters is how it
// treats the agents it launches, not what it can do itself. Every fact keeps
// its source (a URL at a pinned commit) and the date it was checked.

// PaperclipProduct is Paperclip's product code.
const PaperclipProduct = "paperclip"

// PaperclipVersion is the version of the embedded Paperclip base
// (paperclip.json).
const PaperclipVersion = "2026-11-20"

//go:embed paperclip.json
var paperclipJSON []byte

// PaperclipBase is paperclip.json.
type PaperclipBase struct {
	Product      string             `json:"product"`
	Version      string             `json:"version"`
	SourceCommit string             `json:"source_commit"`
	FixedVersion PaperclipFixed     `json:"fixed_version"`
	Adapters     []PaperclipAdapter `json:"adapters"`
	Skills       []PaperclipSkills  `json:"skills_written"`
	PathSecrets  []PaperclipSecret  `json:"path_secrets"`
	Defaults     []PaperclipDefault `json:"defaults"`
}

// PaperclipFixed is the first version without the known serious flaws.
type PaperclipFixed struct {
	Version    string   `json:"version"`
	Advisories []string `json:"advisories"`
	SourceURL  string   `json:"source_url"`
	Verified   string   `json:"verified"`
}

// PaperclipAdapter is how Paperclip starts one agent product: whether it
// overrides the agent's approvals (skips_all, keeps_agent_settings, unknown)
// and whether the agent gets Paperclip's environment.
type PaperclipAdapter struct {
	ID               string `json:"id"`
	Agent            string `json:"agent"`
	ApprovalOverride string `json:"approval_override"`
	EnvPassthrough   bool   `json:"env_passthrough"`
	SourceURL        string `json:"source_url"`
	Verified         string `json:"verified"`
}

// PaperclipSkills is an agent's skills folder Paperclip writes, relative to
// the user's home.
type PaperclipSkills struct {
	Agent     string `json:"agent"`
	Path      string `json:"path"`
	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

// PaperclipSecret is a file of an instance, or a key in its config.json
// (Pointer), that holds a secret: known by where it is, never opened.
type PaperclipSecret struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	Pointer   string `json:"pointer"`
	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

// PaperclipDefault is one of Paperclip's default values.
type PaperclipDefault struct {
	ID        string `json:"id"`
	Value     string `json:"value"`
	SourceURL string `json:"source_url"`
	Verified  string `json:"verified"`
}

var (
	isoDate          = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	paperclipVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// LoadPaperclip returns the embedded Paperclip base, decoded strictly: an
// unknown field, an empty source or date, a duplicate id or an unknown
// approval value is an error.
func LoadPaperclip() (PaperclipBase, error) {
	return decodePaperclip(paperclipJSON)
}

func decodePaperclip(raw []byte) (PaperclipBase, error) {
	var b PaperclipBase
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: trailing data")
	}
	if b.Product != PaperclipProduct || b.Version != PaperclipVersion {
		return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: product %q version %q, want %q %q", b.Product, b.Version, PaperclipProduct, PaperclipVersion)
	}
	if b.SourceCommit == "" {
		return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: no source commit")
	}
	sourced := func(what, url, verified string) error {
		if !strings.HasPrefix(url, "https://") || !isoDate.MatchString(verified) {
			return fmt.Errorf("knowledge base paperclip: %s needs an https source and a YYYY-MM-DD date", what)
		}
		return nil
	}
	if !paperclipVersion.MatchString(b.FixedVersion.Version) {
		return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: fixed version %q", b.FixedVersion.Version)
	}
	if err := sourced("fixed_version", b.FixedVersion.SourceURL, b.FixedVersion.Verified); err != nil {
		return PaperclipBase{}, err
	}
	seen := map[string]bool{}
	unique := func(kind, id string) error {
		if id == "" || seen[kind+"/"+id] {
			return fmt.Errorf("knowledge base paperclip: empty or duplicate %s %q", kind, id)
		}
		seen[kind+"/"+id] = true
		return nil
	}
	for _, a := range b.Adapters {
		if err := unique("adapter", a.ID); err != nil {
			return PaperclipBase{}, err
		}
		switch a.ApprovalOverride {
		case "skips_all", "keeps_agent_settings", "unknown":
		default:
			return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: adapter %s approval %q", a.ID, a.ApprovalOverride)
		}
		if a.Agent == "" {
			return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: adapter %s without agent", a.ID)
		}
		if err := sourced("adapter "+a.ID, a.SourceURL, a.Verified); err != nil {
			return PaperclipBase{}, err
		}
	}
	for _, s := range b.Skills {
		if err := unique("skills", s.Agent); err != nil {
			return PaperclipBase{}, err
		}
		if s.Path == "" {
			return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: skills of %s without path", s.Agent)
		}
		if err := sourced("skills "+s.Agent, s.SourceURL, s.Verified); err != nil {
			return PaperclipBase{}, err
		}
	}
	for _, s := range b.PathSecrets {
		if err := unique("secret", s.ID); err != nil {
			return PaperclipBase{}, err
		}
		if s.File == "" || (s.Pointer != "" && !strings.HasPrefix(s.Pointer, "/")) {
			return PaperclipBase{}, fmt.Errorf("knowledge base paperclip: secret %s file %q pointer %q", s.ID, s.File, s.Pointer)
		}
		if err := sourced("secret "+s.ID, s.SourceURL, s.Verified); err != nil {
			return PaperclipBase{}, err
		}
	}
	for _, d := range b.Defaults {
		if err := unique("default", d.ID); err != nil {
			return PaperclipBase{}, err
		}
		if err := sourced("default "+d.ID, d.SourceURL, d.Verified); err != nil {
			return PaperclipBase{}, err
		}
	}
	return b, nil
}

// Adapter returns how Paperclip starts the agent product, if it starts it.
func (b PaperclipBase) Adapter(agent string) (PaperclipAdapter, bool) {
	for _, a := range b.Adapters {
		if a.Agent == agent {
			return a, true
		}
	}
	return PaperclipAdapter{}, false
}

// Default returns one of Paperclip's default values.
func (b PaperclipBase) Default(id string) (PaperclipDefault, bool) {
	for _, d := range b.Defaults {
		if d.ID == id {
			return d, true
		}
	}
	return PaperclipDefault{}, false
}

// Ref is the evidence reference of a fact of this base:
// kb:paperclip/<id>@<version>.
func (b PaperclipBase) Ref(id string) string {
	return "kb:" + PaperclipProduct + "/" + id + "@" + b.Version
}

// VersionBefore reports whether Paperclip version v (YYYY.MDD.P, as
// install.json and npm give it) is older than fixed. A version that does not
// parse is not before: RosmarOps never claims a flaw it cannot place.
func VersionBefore(v, fixed string) bool {
	a, ok1 := splitVersion(v)
	f, ok2 := splitVersion(fixed)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != f[i] {
			return a[i] < f[i]
		}
	}
	return false
}

func splitVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if !paperclipVersion.MatchString(v) {
		return out, false
	}
	parts := strings.Split(v, ".")
	for i, p := range parts {
		n := 0
		for _, c := range p {
			n = n*10 + int(c-'0')
			if n > 1<<30 {
				return out, false
			}
		}
		out[i] = n
	}
	return out, true
}
