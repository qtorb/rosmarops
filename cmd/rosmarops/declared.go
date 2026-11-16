package main

import (
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
