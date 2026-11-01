package schemaval_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

// quietLocale neutralises gojsonschema's error-description formatting (BV-129).
// Every format string is a constant with no template field, so
// formatErrorDescription never interpolates an error detail and can never grow
// a message without bound. gojsonschema is only the differential test's oracle
// (decision 2026-10-25, G2-G3), never linked into the shipped binary; the test
// reads its verdict (Valid) and each error's Type and Field, never the human
// description, so a constant description changes nothing it checks. It only
// stops the Windows "out of memory" (a ~768 GiB VirtualAlloc) where
// gojsonschema's own error aggregation over a grown array — a 1001-item
// mutation of an array whose schema bounds it at maxItems 1000 — blew up while
// building the message. Set once in init below; see verdicts().
type quietLocale struct{}

func (quietLocale) False() string                        { return "invalid" }
func (quietLocale) Required() string                     { return "invalid" }
func (quietLocale) InvalidType() string                  { return "invalid" }
func (quietLocale) NumberAnyOf() string                  { return "invalid" }
func (quietLocale) NumberOneOf() string                  { return "invalid" }
func (quietLocale) NumberAllOf() string                  { return "invalid" }
func (quietLocale) NumberNot() string                    { return "invalid" }
func (quietLocale) MissingDependency() string            { return "invalid" }
func (quietLocale) Internal() string                     { return "invalid" }
func (quietLocale) Const() string                        { return "invalid" }
func (quietLocale) Enum() string                         { return "invalid" }
func (quietLocale) ArrayNotEnoughItems() string          { return "invalid" }
func (quietLocale) ArrayNoAdditionalItems() string       { return "invalid" }
func (quietLocale) ArrayMinItems() string                { return "invalid" }
func (quietLocale) ArrayMaxItems() string                { return "invalid" }
func (quietLocale) Unique() string                       { return "invalid" }
func (quietLocale) ArrayContains() string                { return "invalid" }
func (quietLocale) ArrayMinProperties() string           { return "invalid" }
func (quietLocale) ArrayMaxProperties() string           { return "invalid" }
func (quietLocale) AdditionalPropertyNotAllowed() string { return "invalid" }
func (quietLocale) InvalidPropertyPattern() string       { return "invalid" }
func (quietLocale) InvalidPropertyName() string          { return "invalid" }
func (quietLocale) StringGTE() string                    { return "invalid" }
func (quietLocale) StringLTE() string                    { return "invalid" }
func (quietLocale) DoesNotMatchPattern() string          { return "invalid" }
func (quietLocale) DoesNotMatchFormat() string           { return "invalid" }
func (quietLocale) MultipleOf() string                   { return "invalid" }
func (quietLocale) NumberGTE() string                    { return "invalid" }
func (quietLocale) NumberGT() string                     { return "invalid" }
func (quietLocale) NumberLTE() string                    { return "invalid" }
func (quietLocale) NumberLT() string                     { return "invalid" }
func (quietLocale) RegexPattern() string                 { return "invalid" }
func (quietLocale) GreaterThanZero() string              { return "invalid" }
func (quietLocale) MustBeOfA() string                    { return "invalid" }
func (quietLocale) MustBeOfAn() string                   { return "invalid" }
func (quietLocale) CannotBeUsedWithout() string          { return "invalid" }
func (quietLocale) CannotBeGT() string                   { return "invalid" }
func (quietLocale) MustBeOfType() string                 { return "invalid" }
func (quietLocale) MustBeValidRegex() string             { return "invalid" }
func (quietLocale) MustBeValidFormat() string            { return "invalid" }
func (quietLocale) MustBeGTEZero() string                { return "invalid" }
func (quietLocale) KeyCannotBeGreaterThan() string       { return "invalid" }
func (quietLocale) KeyItemsMustBeOfType() string         { return "invalid" }
func (quietLocale) KeyItemsMustBeUnique() string         { return "invalid" }
func (quietLocale) ReferenceMustBeCanonical() string     { return "invalid" }
func (quietLocale) NotAValidType() string                { return "invalid" }
func (quietLocale) Duplicated() string                   { return "invalid" }
func (quietLocale) HttpBadStatus() string                { return "invalid" }
func (quietLocale) ParseError() string                   { return "invalid" }
func (quietLocale) ConditionThen() string                { return "invalid" }
func (quietLocale) ConditionElse() string                { return "invalid" }
func (quietLocale) ErrorFormat() string                  { return "invalid" }

// TestMain sets gojsonschema's package-global Locale exactly once, before any
// test runs, and no test writes it again (BV-133). gojsonschema.Locale is
// shared mutable state of the oracle: setting it in TestMain (not an init, not
// a test body) keeps every later gojsonschema access a read of a value fixed
// before m.Run starts, so no test can write this global while another is using
// the oracle. The go test -race gate on the Linux CI job guards that property.
func TestMain(m *testing.M) {
	gojsonschema.Locale = quietLocale{}
	os.Exit(m.Run())
}

// TestQuietLocaleBoundsOracle is the BV-129 regression guard: the oracle still
// rejects a 1001-item array that its schema bounds at maxItems 1000 (the
// mutation that blew up on Windows), and every error description is the
// constant quietLocale returns, so gojsonschema's message building cannot grow
// without bound. If the locale is ever dropped, the descriptions stop being
// constant and this test fails before CI can out-of-memory again.
func TestQuietLocaleBoundsOracle(t *testing.T) {
	schema := gojsonschema.NewBytesLoader([]byte(`{"type":"array","maxItems":1000,"items":{"type":"integer"}}`))
	doc := gojsonschema.NewStringLoader("[" + strings.Repeat("0,", 1000) + "0]") // 1001 items
	res, err := gojsonschema.Validate(schema, doc)
	if err != nil {
		t.Fatalf("oracle validate: %v", err)
	}
	if res.Valid() {
		t.Fatal("oracle accepted a 1001-item array over maxItems 1000")
	}
	for _, e := range res.Errors() {
		if e.Description() != "invalid" {
			t.Errorf("error description not neutralised by quietLocale: %q", e.Description())
		}
	}
}
