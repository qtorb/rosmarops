// Package usage builds the usage statistics package usage.v1 (BV-256): a short,
// anonymous summary of a review that the program sends to app.rosmarops.com
// only while the state records that the person turned usage statistics on.
//
// Build is the only function that decides what leaves. Every field is a fixed
// enum, a closed list or a number: there is no free text. A value outside its
// list, or a package that does not validate against the embedded usage.v1
// schema (a byte-for-byte copy of the app's), is an error and nothing is sent.
package usage

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

const (
	// SchemaName is the package's schema field and contract name.
	SchemaName = "usage.v1"
	// TextVersion is the version of the question's text (usage.ask.*): a state
	// whose answer is older is asked again.
	TextVersion = 1
	// LastFileName is the copy of the last package sent or tried, in the
	// reports' data folder, overwritten each time (§B.6).
	LastFileName = "rosmarops-usage-last.json"
	// SendsLabel is the package's name in rosmarops-sends.log.
	SendsLabel = "rosmarops-usage.v1"
)

// The install channels (§B.1).
const (
	ChannelStore    = "store"
	ChannelHomebrew = "homebrew"
	ChannelOther    = "other"
)

// Payload is one usage.v1 package, field for field.
type Payload struct {
	Schema          string         `json:"schema"`
	InstallID       string         `json:"install_id"`
	Day             string         `json:"day"`
	Version         string         `json:"version"`
	OS              string         `json:"os"`
	Arch            string         `json:"arch"`
	Channel         string         `json:"channel"`
	Agents          []string       `json:"agents"`
	Score           *int           `json:"score"`
	Class           *string        `json:"class"`
	Status          string         `json:"status"`
	Findings        map[string]int `json:"findings"`
	SpaceConnected  bool           `json:"space_connected"`
	PluginInstalled bool           `json:"plugin_installed"`
}

// Input is what the review knows; Build keeps only what usage.v1 allows.
type Input struct {
	InstallID string
	Now       time.Time
	Version   string
	OS, Arch  string
	Channel   string
	// Agents are the product ids found; only the known ones are kept.
	Agents []string
	// Score is the review's score, nil without one.
	Score *int
	// Class is the review's class A-D, "" without one.
	Class  string
	Status string
	// Findings are the review's findings, by rule code.
	Findings        map[string]int
	SpaceConnected  bool
	PluginInstalled bool
}

var (
	installIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	versionPattern   = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,40}$`)
	osValues         = map[string]bool{"windows": true, "darwin": true, "linux": true}
	archValues       = map[string]bool{"amd64": true, "arm64": true}
	channelValues    = map[string]bool{ChannelStore: true, ChannelHomebrew: true, ChannelOther: true}
	classValues      = map[string]bool{"A": true, "B": true, "C": true, "D": true}
	statusValues     = map[string]bool{"green": true, "yellow": true, "red": true}
)

// Build returns the usage.v1 package of a review. It is pure: it reads only in.
// Agents outside the closed list of product ids and finding codes outside the
// rules' closed list are left out; any other value outside its list is an
// error, and so is a package that does not validate.
func Build(in Input) (Payload, error) {
	switch {
	case !installIDPattern.MatchString(in.InstallID):
		return Payload{}, fmt.Errorf("usage: install_id is not a UUID v4")
	case !versionPattern.MatchString(in.Version):
		return Payload{}, fmt.Errorf("usage: version does not match its pattern")
	case !osValues[in.OS]:
		return Payload{}, fmt.Errorf("usage: os %q is not sent", in.OS)
	case !archValues[in.Arch]:
		return Payload{}, fmt.Errorf("usage: arch %q is not sent", in.Arch)
	case !channelValues[in.Channel]:
		return Payload{}, fmt.Errorf("usage: channel %q is not sent", in.Channel)
	case !statusValues[in.Status]:
		return Payload{}, fmt.Errorf("usage: status %q is not sent", in.Status)
	case in.Class != "" && !classValues[in.Class]:
		return Payload{}, fmt.Errorf("usage: class %q is not sent", in.Class)
	case in.Score != nil && (*in.Score < 0 || *in.Score > 100):
		return Payload{}, fmt.Errorf("usage: score out of range")
	}
	p := Payload{
		Schema: SchemaName, InstallID: in.InstallID, Day: in.Now.UTC().Format("2006-01-02"),
		Version: in.Version, OS: in.OS, Arch: in.Arch, Channel: in.Channel,
		Agents: []string{}, Status: in.Status, Findings: map[string]int{},
		SpaceConnected: in.SpaceConnected, PluginInstalled: in.PluginInstalled,
	}
	if in.Score != nil {
		s := *in.Score
		p.Score = &s
	}
	if in.Class != "" {
		c := in.Class
		p.Class = &c
	}
	seen := map[string]bool{}
	for _, a := range in.Agents {
		if problems.ValidAgent(a) && !seen[a] {
			seen[a] = true
			p.Agents = append(p.Agents, a)
		}
	}
	sort.Strings(p.Agents)
	for code, n := range in.Findings {
		if slices.Contains(rules.AllRules, code) && n > 0 {
			p.Findings[code] = min(n, 100000)
		}
	}
	if _, err := Marshal(p); err != nil {
		return Payload{}, err
	}
	return p, nil
}

// Marshal is the package's JSON, validated against the embedded usage.v1
// schema. The error names only how many violations there were, never a value.
func Marshal(p Payload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("usage: marshal: %w", err)
	}
	schema, err := schemaval.Compile(schemas.UsageSchema)
	if err != nil {
		return nil, fmt.Errorf("usage: compile schema: %w", err)
	}
	res, err := schema.Validate(b)
	if err != nil {
		return nil, fmt.Errorf("usage: validate: %w", err)
	}
	if !res.Valid() {
		return nil, fmt.Errorf("usage: package fails usage.v1 schema validation (%d errors)", len(res.Errors()))
	}
	return b, nil
}

// NewInstallID returns a random UUID v4, the install id sent with the
// statistics: random, never derived from the machine.
func NewInstallID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("usage: install id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
