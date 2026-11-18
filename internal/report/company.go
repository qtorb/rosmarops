package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// Company is report.v2's company object: the company configuration of this
// review, when the computer has one.
type Company struct {
	// State is trusted, untrusted or invalid.
	State string `json:"state"`
	// SHA256 is the fingerprint of the trusted company.json read.
	SHA256 string `json:"company_config_sha256,omitempty"`
	// Changed: the company configuration in use changed since the last
	// review (recorded and reported, never an abort).
	Changed bool `json:"company_config_changed,omitempty"`
	// SummaryEnabled: the company reads this review's summary.
	SummaryEnabled bool `json:"summary_enabled"`
	// ReportDirOverridden: the person had chosen another reports folder, and
	// the company configuration put the report back in the usual one.
	ReportDirOverridden bool `json:"report_directory_overridden,omitempty"`
	// ConfigSource is user (the person's config.json) or company.
	ConfigSource string `json:"config_source"`
	// BinaryAcceptedVia is how this review accepted its binary: unchanged,
	// first_run, console, admin_list or store.
	BinaryAcceptedVia string `json:"binary_accepted_via"`
}

// SummaryFileName is the company summary in the reports data folder.
const SummaryFileName = "rosmarops-summary.json"

// The run values of a summary.
const (
	SummaryComplete       = "complete"
	SummaryIntegrityAbort = "integrity_abort"
	SummaryFailed         = "failed"
)

// SummaryRunFailed is the abort code of a review that did not finish.
const SummaryRunFailed = "run_failed"

// CompanySummary mirrors schemas/summary.v1.json: what a company agent may read
// about one review. Counts and codes only: no path, preview, file content,
// name chosen by the person, or identifier derived from a path.
type CompanySummary struct {
	SchemaVersion int                   `json:"schema_version"`
	GeneratedAt   string                `json:"generated_at"`
	GeneratedUnix int64                 `json:"generated_unix"`
	Version       string                `json:"version"`
	RulesVersion  *string               `json:"rules_version"`
	Run           string                `json:"run"`
	AbortCode     *string               `json:"abort_code"`
	Status        string                `json:"status"`
	RedTotal      int                   `json:"red_total"`
	YellowTotal   int                   `json:"yellow_total"`
	MachineBand   *string               `json:"machine_band"`
	MachineIndex  *int                  `json:"machine_index"`
	ByRule        []CompanySummaryRule  `json:"by_rule"`
	Agents        []CompanySummaryAgent `json:"agents"`
	NotChecked    []string              `json:"not_checked"`
	ConfigSource  string                `json:"config_source"`
	Binary        string                `json:"binary"`
}

// CompanySummaryRule is one rule's findings of one severity.
type CompanySummaryRule struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

// CompanySummaryAgent is the agents of one type and product: how many, their
// findings and their worst band.
type CompanySummaryAgent struct {
	Type    string  `json:"type"`
	Product string  `json:"product"`
	Count   int     `json:"count"`
	Red     int     `json:"red"`
	Yellow  int     `json:"yellow"`
	Band    *string `json:"band"`
}

// SummaryOf is the summary of a review that finished: v's status, its v2
// findings by rule, the computer's score, and the agents of g grouped by type
// and product. notChecked are the report's not-checked codes.
func SummaryOf(v V2, g *graph.Graph, version string, notChecked []string, configSource, binary string, now time.Time) CompanySummary {
	s := baseSummary(version, configSource, binary, now)
	s.Run, s.Status = SummaryComplete, v.Status
	if rv := v.Provenance.RulesVersion; rv != "" {
		s.RulesVersion = &rv
	}
	if m := v.Index.Machine; m.Value != nil {
		val, band := *m.Value, m.Band
		s.MachineIndex = &val
		if band != "" {
			s.MachineBand = &band
		}
	}
	type key struct{ rule, sev string }
	byRule := map[key]int{}
	for _, f := range v.Findings {
		if f.Severity != rules.SeverityRed && f.Severity != rules.SeverityYellow {
			continue
		}
		byRule[key{f.Rule, f.Severity}]++
		if f.Severity == rules.SeverityRed {
			s.RedTotal++
		} else {
			s.YellowTotal++
		}
	}
	for k, n := range byRule {
		s.ByRule = append(s.ByRule, CompanySummaryRule{Rule: k.rule, Severity: k.sev, Count: n})
	}
	sort.Slice(s.ByRule, func(i, j int) bool {
		a, b := s.ByRule[i], s.ByRule[j]
		if a.Severity != b.Severity {
			return a.Severity == rules.SeverityRed
		}
		return a.Rule < b.Rule
	})
	if s.RedTotal > 0 {
		s.Status = "red"
	}
	if g != nil {
		s.Agents = summaryAgents(v, *g)
	}
	for _, c := range NotCheckedCodes {
		for _, n := range notChecked {
			if n == c {
				s.NotChecked = append(s.NotChecked, c)
				break
			}
		}
	}
	return s
}

// AbortSummary is the summary of a review that stopped: an integrity abort
// (with its code: binary_hash_changed, config_hash_changed or state_lost) or
// a review that did not finish (run_failed). Its status is red; it carries
// no score and no findings by rule. An integrity abort's minimal red report
// has one finding, which red_total counts.
func AbortSummary(run, code, version, configSource string, now time.Time) CompanySummary {
	s := baseSummary(version, configSource, "unchanged", now)
	s.Run, s.Status, s.AbortCode = run, "red", &code
	if run == SummaryIntegrityAbort {
		s.RedTotal = 1
	}
	return s
}

func baseSummary(version, configSource, binary string, now time.Time) CompanySummary {
	if configSource == "" {
		configSource = "user"
	}
	now = now.UTC()
	return CompanySummary{
		SchemaVersion: 1, GeneratedAt: now.Format(time.RFC3339), GeneratedUnix: now.Unix(),
		Version: version, ByRule: []CompanySummaryRule{}, Agents: []CompanySummaryAgent{}, NotChecked: []string{},
		ConfigSource: configSource, Binary: binary,
	}
}

// summaryProduct is an agent's product code from the program's closed list
// of products, as summary.v1 writes it (claude-code is claude_code). An
// extension is counted under its editor. A product outside the list is
// "other": never a name the person wrote.
func summaryProduct(n graph.Node) string {
	if n.Type == graph.AgentExtension {
		return "vscode"
	}
	if !graph.KnownProduct(n.Platform) {
		return "other"
	}
	return strings.ReplaceAll(n.Platform, "-", "_")
}

// summaryAgents groups the graph's agents by type and product. An agent's
// findings are those whose evidence names it; the band is the worst band of
// the scored agents of the group, null when none is scored.
func summaryAgents(v V2, g graph.Graph) []CompanySummaryAgent {
	type key struct{ typ, product string }
	groups := map[key]*CompanySummaryAgent{}
	var order []key
	bands := map[string]string{}
	for _, a := range v.Index.Agents {
		if a.Value != nil {
			bands[a.AgentID] = a.Band
		}
	}
	rank := map[string]int{index.BandContained: 1, index.BandOpenGaps: 2, index.BandExposed: 3}
	for _, n := range g.Nodes {
		if n.Kind != graph.KindAgent {
			continue
		}
		k := key{n.Type, summaryProduct(n)}
		sa := groups[k]
		if sa == nil {
			sa = &CompanySummaryAgent{Type: k.typ, Product: k.product}
			groups[k] = sa
			order = append(order, k)
		}
		sa.Count++
		for _, f := range v.Findings {
			if !nodeIn(n.ID, f.Evidence.Nodes) {
				continue
			}
			switch f.Severity {
			case rules.SeverityRed:
				sa.Red++
			case rules.SeverityYellow:
				sa.Yellow++
			}
		}
		if b, ok := bands[n.ID]; ok && (sa.Band == nil || rank[b] > rank[*sa.Band]) {
			band := b
			sa.Band = &band
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].typ != order[j].typ {
			return order[i].typ < order[j].typ
		}
		return order[i].product < order[j].product
	})
	out := make([]CompanySummaryAgent, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

func nodeIn(id string, ids []string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

var compiledSummary *schemaval.Schema

// ValidateSummary checks raw against summary.v1.
func ValidateSummary(raw []byte) error {
	if compiledSummary == nil {
		sch, err := schemaval.Compile(schemas.SummaryV1Schema)
		if err != nil {
			return fmt.Errorf("compile summary schema: %w", err)
		}
		compiledSummary = sch
	}
	res, err := compiledSummary.Validate(raw)
	if err != nil {
		return fmt.Errorf("validate summary: %w", err)
	}
	if !res.Valid() {
		var msgs []string
		for _, e := range res.Errors() {
			msgs = append(msgs, e.String())
		}
		return fmt.Errorf("summary fails schema validation: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// MarshalSummary is s as written: one compact JSON line and a newline,
// checked against summary.v1.
func MarshalSummary(s CompanySummary) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("encode summary: %w", err)
	}
	if err := ValidateSummary(raw); err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// PrettySummary is s indented, for the report to show as it is.
func PrettySummary(s CompanySummary) string {
	raw, err := MarshalSummary(s)
	if err != nil {
		return ""
	}
	var b bytes.Buffer
	if err := json.Indent(&b, bytes.TrimSpace(raw), "", "  "); err != nil {
		return ""
	}
	return b.String()
}

// WriteSummary writes s to dataDir/rosmarops-summary.json atomically: a
// .tmp file in the same folder, then a rename. dataDir is inside the
// reports directory.
func WriteSummary(dataDir string, s CompanySummary) (string, error) {
	raw, err := MarshalSummary(s)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, SummaryFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", fmt.Errorf("write summary: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename summary: %w", err)
	}
	return path, nil
}

// RemoveSummary deletes a previous summary in dataDir, so the company never
// reads an old one once it reads none. No summary is not an error.
func RemoveSummary(dataDir string) error {
	for _, name := range []string{SummaryFileName, SummaryFileName + ".tmp"} {
		if err := os.Remove(filepath.Join(dataDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove summary: %w", err)
		}
	}
	return nil
}
