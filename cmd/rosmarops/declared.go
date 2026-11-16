package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// declaredAnswers are the state's declared answers as the pipeline takes them
// (BV-248): product → question → option.
func declaredAnswers(st state.State) map[string]map[string]string {
	if len(st.Declared) == 0 {
		return nil
	}
	out := map[string]map[string]string{}
	for p, d := range st.Declared {
		out[p] = d.Answers
	}
	return out
}

// declaredAt is when each product's answers were given.
func declaredAt(st state.State) map[string]string {
	if len(st.Declared) == 0 {
		return nil
	}
	out := map[string]string{}
	for p, d := range st.Declared {
		out[p] = d.AnsweredAt
	}
	return out
}

// declaredContext is what decides which questions a review asks (BV-248):
// Cursor evaluated, the facts read of Cursor and Grok Bot.
type declaredContext struct {
	cursor      bool
	cursorFacts declared.CursorFacts
	grokBot     declared.GrokBotFacts
}

// cursorEvaluated reports Cursor among the evaluated agents of the report.
func cursorEvaluated(v report.V2) bool {
	for _, a := range v.AgentsOverview {
		if a.Product == knowledge.CursorProduct && a.Evaluated {
			return true
		}
	}
	return false
}

// knowledgeVersionOf is the version of a product's knowledge base, kept with
// the answers: another version asks again (CTO-56 §5.1).
func knowledgeVersionOf(product string) string {
	switch product {
	case declared.Cursor:
		return knowledge.CursorVersion
	case declared.GrokBot:
		return knowledge.GrokBotVersion
	}
	return ""
}

// agentName is the product's name in the questions.
func agentName(product string) string { return graph.ClientDisplayName(product) }

// askDeclared asks, in the console, the questions about the settings
// RosmarOps cannot read at rest (BV-248): for Cursor when it is evaluated, and
// for Grok Bot. With answers already stored for this knowledge version, it
// shows them and asks whether they are still the same (Enter keeps them, «c»
// changes them). It changes st.Declared only, and reports whether it did.
func askDeclared(in io.Reader, out io.Writer, cat catalog.Catalog, st *state.State, dc declaredContext, now time.Time) bool {
	r := bufio.NewReader(in)
	ui.question() // BV-151: the window and taskbar show a pending question
	defer ui.running()
	changed := false
	for _, p := range declared.Products {
		if p == declared.Cursor && !dc.cursor {
			continue
		}
		prev, had := st.Declared[p]
		if had && prev.KnowledgeVersion == knowledgeVersionOf(p) {
			summary := report.DeclaredSummary(cat, p, prev.Answers, nil)
			if summary != "" {
				if !askSame(r, out, cat, p, prev, summary) {
					continue
				}
			}
		}
		answers, ok := askProduct(r, out, cat, p, dc)
		if !ok {
			return changed // the input ended: keep what is stored
		}
		if st.Declared == nil {
			st.Declared = map[string]state.Declared{}
		}
		if len(answers) == 0 {
			if had {
				delete(st.Declared, p)
				changed = true
			}
			continue
		}
		st.Declared[p] = state.Declared{Answers: answers, AnsweredAt: now.UTC().Format(time.RFC3339), KnowledgeVersion: knowledgeVersionOf(p)}
		changed = true
	}
	if len(st.Declared) == 0 {
		st.Declared = nil
	}
	return changed
}

// askSame shows the stored answers and reports whether the person wants to
// change them: Enter keeps them, «c» changes them; anything else asks again.
func askSame(r *bufio.Reader, out io.Writer, cat catalog.Catalog, product string, prev state.Declared, summary string) bool {
	for {
		fmt.Fprintf(out, "\n%s ", cat.Text("decl.again", map[string]string{
			"agent": agentName(product), "date": declaredDayLabel(cat, prev.AnsweredAt), "summary": summary,
		}))
		line, err := r.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return false
		case "c":
			return true
		}
		if err != nil {
			return false
		}
	}
}

// askProduct asks a product's questions in order, each only when it applies
// with the answers given so far; ok is false when the input ended.
func askProduct(r *bufio.Reader, out io.Writer, cat catalog.Catalog, product string, dc declaredContext) (map[string]string, bool) {
	answers := map[string]string{}
	applies := func() map[string]bool {
		if product == declared.Cursor {
			return declared.ResolveCursor(dc.cursorFacts, answers).Applies
		}
		st := declared.ResolveGrokBot(dc.grokBot, answers)
		if !st.Present {
			return map[string]bool{declared.B0Uses: true}
		}
		return st.Applies
	}
	fmt.Fprintf(out, "\n%s\n", cat.Text("decl.entry", map[string]string{"agent": agentName(product), "n": strconv.Itoa(maxQuestions(product, dc))}))
	for _, q := range declared.Questions(product) {
		if !applies()[q.ID] {
			continue
		}
		code, ok := askOne(r, out, cat, q)
		if !ok {
			return answers, false
		}
		if code != "" {
			answers[q.ID] = code
		}
		if q.ID == declared.B0Uses && code != declared.Yes {
			break
		}
	}
	return answers, true
}

// maxQuestions is the {n} of the entry: the questions that apply on the
// widest path (Cursor in Auto-review with a list that reads files; Grok Bot
// in use).
func maxQuestions(product string, dc declaredContext) int {
	if product == declared.Cursor {
		return len(declared.ResolveCursor(dc.cursorFacts, map[string]string{
			declared.C1RunMode: declared.ModeAutoReview, declared.C2Allowlist: declared.ListReads,
		}).Applies)
	}
	n := len(declared.ResolveGrokBot(declared.GrokBotFacts{Present: true, LocalRead: dc.grokBot.LocalRead, AutoReviewRead: dc.grokBot.AutoReviewRead}, nil).Applies)
	if !dc.grokBot.Present {
		n++ // B0
	}
	return n
}

// askOne asks one question with its numbered options and «0 · I don't know»:
// a number is that option, 0 unknown, Enter no answer (""); anything else asks
// again.
func askOne(r *bufio.Reader, out io.Writer, cat catalog.Catalog, q declared.Question) (string, bool) {
	for {
		fmt.Fprintf(out, "\n%s\n", cat.Text(declared.QuestionKey(q.ID), nil))
		for i, o := range q.Options {
			fmt.Fprintf(out, "  %d · %s\n", i+1, cat.Text(declared.LabelKey(q.ID, o), nil))
		}
		fmt.Fprintf(out, "  0 · %s\n", cat.Text(declared.LabelKey(q.ID, declared.Unknown), nil))
		line, err := r.ReadString('\n')
		s := strings.TrimSpace(line)
		if s == "" {
			return "", err == nil || line != ""
		}
		if n, e := strconv.Atoi(s); e == nil && n >= 0 && n <= len(q.Options) {
			if n == 0 {
				return declared.Unknown, true
			}
			return q.Options[n-1], true
		}
		if err != nil {
			return "", false
		}
	}
}

// declaredDayLabel is an RFC 3339 time as the catalog's day label, local.
func declaredDayLabel(cat catalog.Catalog, at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	return cat.DayLabel(t.Local())
}
