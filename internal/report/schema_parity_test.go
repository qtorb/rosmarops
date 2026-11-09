package report_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// TestSchemaEnumsCoverCodeConstants (BV-202, CTO-28) is the parity test that
// would have caught BV-201 and this not_evaluated_reason failure: for every
// report.v2 / space-view.v1 enum that has a closed list of constants in Go, it
// checks that every value the code can emit is a member of the schema enum. A
// value the schema does not admit fails the test here instead of failing a
// user's report at write time. Adding a value to a schema enum needs Albert's
// signature, so a new failure means STOP and ask — not edit the schema.
//
// Pairs covered (code list -> schema enum):
//
//	report.v2:
//	  rules.AllRules              -> definitions/finding/properties/rule
//	  rules.AllClasses            -> definitions/finding/properties/params/properties/class
//	  rules.AllActions            -> definitions/action/properties/code
//	  rules.AllCompleteness       -> definitions/action/properties/completeness
//	  rules.AllPostures           -> definitions/posture/properties/code
//	  rules.AllOverviewRows       -> definitions/overviewCell/properties/row
//	  rules.AllScopes             -> definitions/overviewCell/properties/scope
//	  rules.AllOrigins            -> definitions/overviewCell/properties/origin
//	  rules.AllDeciders           -> definitions/overviewDecider
//	  rules.AllNoFindingReasons   -> definitions/overviewCell/properties/no_finding_reason/oneOf/1
//	  rules.AllPendingReasons     -> definitions/overviewCell/properties/pending/oneOf/1
//	  rules.AllNotEvaluatedReasons-> definitions/overviewAgent/properties/not_evaluated_reason/oneOf/1
//	  rules.AllRecipients         -> definitions/overviewData/properties/recipient
//	  rules.AllExcluded           -> definitions/overviewData/properties/excluded/items
//	space-view.v1:
//	  the five overview verbs      -> $defs/verbs/items
func TestSchemaEnumsCoverCodeConstants(t *testing.T) {
	// The space view shows the five verbs that have a real decider; model_provider
	// is always not_applicable (overview.go) so it never reaches the space view.
	svVerbs := make([]string, 0, len(rules.AllOverviewRows))
	for _, r := range rules.AllOverviewRows {
		if r != rules.RowModelProvider {
			svVerbs = append(svVerbs, r)
		}
	}

	cases := []struct {
		name   string
		code   []string
		schema []byte
		path   []any
	}{
		{"finding.rule", rules.AllRules, schemas.ReportV2Schema, []any{"definitions", "finding", "properties", "rule"}},
		{"finding.params.class", rules.AllClasses, schemas.ReportV2Schema, []any{"definitions", "finding", "properties", "params", "properties", "class"}},
		{"action.code", rules.AllActions, schemas.ReportV2Schema, []any{"definitions", "action", "properties", "code"}},
		{"action.completeness", rules.AllCompleteness, schemas.ReportV2Schema, []any{"definitions", "action", "properties", "completeness"}},
		{"posture.code", rules.AllPostures, schemas.ReportV2Schema, []any{"definitions", "posture", "properties", "code"}},
		{"overviewCell.row", rules.AllOverviewRows, schemas.ReportV2Schema, []any{"definitions", "overviewCell", "properties", "row"}},
		{"overviewCell.scope", rules.AllScopes, schemas.ReportV2Schema, []any{"definitions", "overviewCell", "properties", "scope"}},
		{"overviewCell.origin", rules.AllOrigins, schemas.ReportV2Schema, []any{"definitions", "overviewCell", "properties", "origin"}},
		{"overviewDecider", rules.AllDeciders, schemas.ReportV2Schema, []any{"definitions", "overviewDecider"}},
		{"overviewCell.no_finding_reason", rules.AllNoFindingReasons, schemas.ReportV2Schema, []any{"definitions", "overviewCell", "properties", "no_finding_reason", "oneOf", 1}},
		{"overviewCell.pending", rules.AllPendingReasons, schemas.ReportV2Schema, []any{"definitions", "overviewCell", "properties", "pending", "oneOf", 1}},
		{"overviewAgent.not_evaluated_reason", rules.AllNotEvaluatedReasons, schemas.ReportV2Schema, []any{"definitions", "overviewAgent", "properties", "not_evaluated_reason", "oneOf", 1}},
		{"overviewData.recipient", rules.AllRecipients, schemas.ReportV2Schema, []any{"definitions", "overviewData", "properties", "recipient"}},
		{"overviewData.excluded", rules.AllExcluded, schemas.ReportV2Schema, []any{"definitions", "overviewData", "properties", "excluded", "items"}},
		{"space-view verbs", svVerbs, schemas.SpaceViewSchema, []any{"$defs", "verbs", "items"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enum, err := schemaEnum(c.schema, c.path)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if len(enum) == 0 {
				t.Fatalf("%s: no enum found at the schema path", c.name)
			}
			set := map[string]bool{}
			for _, v := range enum {
				set[v] = true
			}
			for _, v := range c.code {
				if !set[v] {
					t.Errorf("%s: code emits %q but the schema enum does not admit it (needs Albert's signature to add)", c.name, v)
				}
			}
		})
	}
}

// schemaEnum navigates the JSON schema by the given steps (string map keys or
// int array indices) and returns the string enum at the destination. The
// destination is either an object with an "enum" array or an enum array itself.
func schemaEnum(raw []byte, path []any) ([]string, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	cur := root
	for _, step := range path {
		switch s := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("step %q: not an object", s)
			}
			cur, ok = m[s]
			if !ok {
				return nil, fmt.Errorf("step %q: missing", s)
			}
		case int:
			a, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("index %d: not an array", s)
			}
			if s < 0 || s >= len(a) {
				return nil, fmt.Errorf("index %d: out of range", s)
			}
			cur = a[s]
		default:
			return nil, fmt.Errorf("bad step %v", step)
		}
	}
	// cur is either {"enum": [...]} or the enum array itself.
	if m, ok := cur.(map[string]any); ok {
		cur = m["enum"]
	}
	arr, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("no enum array at the path")
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}
