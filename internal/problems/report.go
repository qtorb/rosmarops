package problems

import (
	"encoding/json"
	"fmt"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// Format is the problems.v1 format tag (§11).
const Format = "problems.v1"

// MaxBodyBytes caps the JSON body the endpoint accepts (§11: at most 32 KB).
const MaxBodyBytes = 32 * 1024

// Report is the problems.v1 payload sent, only with the person's permission, to
// app.rosmarops.com (§11). Comment and email are required but nullable, so they
// are pointers: nil encodes as JSON null. The report carries no account, no
// space and nothing that identifies the machine.
type Report struct {
	Format           string          `json:"format"`
	Lang             string          `json:"lang"`
	RosmaropsVersion string          `json:"rosmarops_version"`
	OS               string          `json:"os"`
	OSVersion        string          `json:"os_version"`
	Arch             string          `json:"arch"`
	Problems         []ReportProblem `json:"problems"`
	Comment          *string         `json:"comment"`
	Email            *string         `json:"email"`
}

// ReportProblem is one problem in a report: a notebook line without its local
// bookkeeping (no sent flag). Agent and HTTPStatus are omitted when absent.
type ReportProblem struct {
	Code       Code   `json:"code"`
	Step       string `json:"step"`
	Agent      string `json:"agent,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Day        string `json:"day"`
	Count      int    `json:"count"`
}

// System is the machine facts the report carries: the UI language, the program
// version, and the OS/version/arch. None identifies the person or the machine.
type System struct {
	Lang      string
	Version   string
	OS        string
	OSVersion string
	Arch      string
}

// BuildReport assembles a problems.v1 report from the given unsent entries and
// system facts. comment and email are optional (nil → null). Entries beyond the
// problems.v1 limit of 20 are dropped by the caller (Unsent(MaxPerSend)).
func BuildReport(sys System, entries []Entry, comment, email *string) Report {
	ps := make([]ReportProblem, 0, len(entries))
	for _, e := range entries {
		ps = append(ps, ReportProblem{
			Code: e.Code, Step: e.Step, Agent: e.Agent,
			HTTPStatus: e.HTTPStatus, Day: e.Day, Count: e.Count,
		})
	}
	return Report{
		Format:           Format,
		Lang:             sys.Lang,
		RosmaropsVersion: sys.Version,
		OS:               sys.OS,
		OSVersion:        sys.OSVersion,
		Arch:             sys.Arch,
		Problems:         ps,
		Comment:          comment,
		Email:            email,
	}
}

// Marshal encodes a report as compact JSON and validates it against the
// embedded problems.v1 schema. A report that does not validate is never
// returned, so nothing that leaves the machine was not first checked against
// its schema (§2, §7).
func Marshal(r Report) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("problems: marshal report: %w", err)
	}
	if err := Validate(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Validate checks raw against the embedded problems.v1 schema. The error names
// only how many violations there were, never a field value.
func Validate(raw []byte) error {
	schema, err := schemaval.Compile(schemas.ProblemsSchema)
	if err != nil {
		return fmt.Errorf("problems: compile schema: %w", err)
	}
	res, err := schema.Validate(raw)
	if err != nil {
		return fmt.Errorf("problems: validate report: %w", err)
	}
	if !res.Valid() {
		return fmt.Errorf("problems: report fails problems.v1 schema validation (%d errors)", len(res.Errors()))
	}
	return nil
}
