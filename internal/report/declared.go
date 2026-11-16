package report

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/rules"
)

// declaredDay is the day of an RFC 3339 time as the report writes days
// (BV-248, {date} of declared_by_you and of the answers line): the catalog's
// day label in the local zone, or YYYY-MM-DD without a catalog.
func declaredDay(cat *catalog.Catalog, at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	t = t.Local()
	if cat == nil {
		return t.Format("2006-01-02")
	}
	return cat.DayLabel(t)
}

// DeclaredSummary is the {summary} of the answers (BV-248): the labels of the
// options chosen, in question order, separated by « · ».
func DeclaredSummary(cat catalog.Catalog, product string, answers map[string]string, applies map[string]bool) string {
	var parts []string
	for _, q := range declared.Questions(product) {
		v, ok := answers[q.ID]
		if !ok || applies != nil && !applies[q.ID] {
			continue
		}
		parts = append(parts, cat.Text(declared.LabelKey(q.ID, v), nil))
	}
	return strings.Join(parts, " · ")
}

// declaredLine is an agent's «Your answers (date): summary.» line, empty
// without answers.
func declaredLine(cat catalog.Catalog, a rules.OverviewAgent) string {
	d := a.Declared
	if d == nil || len(d.Answers) == 0 {
		return ""
	}
	return cat.Text("decl.line", map[string]string{
		"date": declaredDay(&cat, d.AnsweredAt), "summary": DeclaredSummary(cat, a.Product, d.Answers, nil),
	})
}

// DeclaredKeys are the catalog codes of the questions (the console) and of
// the answers line (the report), BV-248.
func DeclaredKeys() []string {
	keys := []string{"decl.entry", "decl.again", "decl.line", "decl.unknown"}
	for _, p := range declared.Products {
		for _, q := range declared.Questions(p) {
			keys = append(keys, declared.QuestionKey(q.ID))
			for _, o := range q.Options {
				keys = append(keys, declared.LabelKey(q.ID, o))
			}
		}
	}
	return keys
}
