// Package declared holds the questions RosmarOps asks about the settings it
// cannot read at rest (BV-248, CTO-56): Cursor's C1–C4 and Grok Bot's B0–B4,
// their options as stable codes, the factory answer and the least controlled
// case of each, and which questions apply. The answers live in state.json
// (state.v2 declared); the engine reads the effective answer of each question:
// the person's, or the least controlled case when there is none or it is
// «I don't know» (CTO-56 §4). The texts are in the catalog (decl.*).
package declared

import (
	"sort"
	"strings"
)

// Products with declared settings.
const (
	Cursor  = "cursor"
	GrokBot = "grok-bot"
)

// Unknown is «I don't know», stored as such (CTO-56 §5.1).
const Unknown = "unknown"

// Question codes.
const (
	C1RunMode        = "c1_run_mode"
	C2Allowlist      = "c2_allowlist"
	C3ReadAccess     = "c3_read_access"
	C4ExternalFileOn = "c4_external_file_protection"

	B0Uses           = "b0_uses"
	B1LocalExecution = "b1_local_execution"
	B2AutoReview     = "b2_auto_review"
	B3AskFirst       = "b3_ask_first"
	B4SendTools      = "b4_send_tools"
)

// Option codes.
const (
	ModeAskEveryTime      = "ask_every_time"
	ModeAllowlist         = "allowlist"
	ModeAutoReview        = "auto_review"
	ModeAutoReviewSandbox = "auto_review_sandbox"
	ModeAllowlistSandbox  = "allowlist_sandbox"
	ModeRunEverything     = "run_everything"

	ListEmpty   = "empty"
	ListNoReads = "no_reads"
	ListReads   = "reads_files"
	ListNetwork = "network"
	ListRunCode = "runs_code"

	ReadWorkspace = "workspace"
	ReadSystem    = "system"

	On  = "on"
	Off = "off"
	Yes = "yes"
	No  = "no"

	LocalAsk    = "ask_every_time"
	LocalAlways = "always_allow"
	LocalNever  = "never_allow"

	AskFirstAll  = "all_four"
	AskFirstSome = "some"
	AskFirstNone = "none"
)

// Question is one question: its options in display order (1, 2, …; 0 is
// Unknown), the answer a fresh install has (CTO-56 §1.2, §2.2) and the least
// controlled one (CTO-56 §4).
type Question struct {
	ID      string
	Options []string
	Factory string
	Least   string
}

// CursorQuestions are C1–C4 (CTO-56 §1.2), in order.
var CursorQuestions = []Question{
	{C1RunMode, []string{ModeAskEveryTime, ModeAllowlist, ModeAutoReview, ModeAutoReviewSandbox, ModeAllowlistSandbox, ModeRunEverything}, ModeAutoReviewSandbox, ModeRunEverything},
	{C2Allowlist, []string{ListEmpty, ListNoReads, ListReads, ListNetwork, ListRunCode}, ListEmpty, ListRunCode},
	{C3ReadAccess, []string{ReadWorkspace, ReadSystem}, ReadSystem, ReadSystem},
	{C4ExternalFileOn, []string{On, Off}, On, Off},
}

// GrokBotQuestions are B0–B4 (CTO-56 §2.2), in order.
var GrokBotQuestions = []Question{
	{B0Uses, []string{Yes, No}, Yes, ""},
	{B1LocalExecution, []string{LocalAsk, LocalAlways, LocalNever}, LocalAsk, LocalAlways},
	{B2AutoReview, []string{On, Off}, On, Off},
	{B3AskFirst, []string{AskFirstAll, AskFirstSome, AskFirstNone}, AskFirstNone, AskFirstNone},
	{B4SendTools, []string{Yes, No}, No, Yes},
}

// Questions are the product's questions.
func Questions(product string) []Question {
	switch product {
	case Cursor:
		return CursorQuestions
	case GrokBot:
		return GrokBotQuestions
	}
	return nil
}

// Products are the products with questions, in the order they are asked.
var Products = []string{Cursor, GrokBot}

// Question is the product's question id.
func QuestionOf(product, id string) (Question, bool) {
	for _, q := range Questions(product) {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}

// Valid reports an option code of the question (Unknown included).
func (q Question) Valid(code string) bool {
	if code == Unknown {
		return true
	}
	for _, o := range q.Options {
		if o == code {
			return true
		}
	}
	return false
}

// Answer is the effective answer of one question: the option that counts and
// whether the person gave it (false: no answer or «I don't know», the least
// controlled case is assumed).
type Answer struct {
	Code     string
	Declared bool
}

// CursorFacts are what RosmarOps reads at rest that decides which Cursor
// questions apply (CTO-56 §1.1, §1.2).
type CursorFacts struct {
	// ApprovalMode is permissions.json's approvalMode (allowlist,
	// unrestricted, manual), "" when absent or not one of them.
	ApprovalMode string
	// TerminalAllowlist: permissions.json carries terminalAllowlist.
	TerminalAllowlist bool
	// ReadBoundaryFixed: sandbox.json fixes readBoundary (the user's, or
	// every repository's).
	ReadBoundaryFixed bool
}

// ApprovalModeRunMode is the C1 option a permissions.json approvalMode stands
// for (CTO-56 §1.1): it replaces the question.
func ApprovalModeRunMode(m string) (string, bool) {
	switch m {
	case "allowlist":
		return ModeAllowlist, true
	case "unrestricted":
		return ModeRunEverything, true
	case "manual":
		return ModeAskEveryTime, true
	}
	return "", false
}

// IsAutoReview reports the two Auto-review modes.
func IsAutoReview(mode string) bool {
	return mode == ModeAutoReview || mode == ModeAutoReviewSandbox
}

// IsAllowlist reports the two Allowlist modes.
func IsAllowlist(mode string) bool {
	return mode == ModeAllowlist || mode == ModeAllowlistSandbox
}

// effective is the answer of q from stored: the stored option, or its least
// controlled case.
func effective(q Question, stored map[string]string) Answer {
	if v, ok := stored[q.ID]; ok && v != Unknown && q.Valid(v) {
		return Answer{Code: v, Declared: true}
	}
	return Answer{Code: q.Least}
}

// CursorState is Cursor's effective answers: the mode (read or answered), and
// each question that applies.
type CursorState struct {
	Mode       Answer // the run mode: approvalMode when read (Declared false, Read true), else C1
	ModeRead   bool   // the mode comes from permissions.json, not from C1
	Applies    map[string]bool
	Answers    map[string]Answer // the effective answer of each question that applies
	AnyStored  bool              // some answer is stored
	AllStored  bool              // every question that applies has a stored answer (unknown included)
	StoredSome bool              // some applying question has a stored answer
}

// ResolveCursor are Cursor's effective answers for the facts read and the
// stored answers (nil: none). A question applies (CTO-56 §1.2):
//   - C1 when approvalMode is not in permissions.json;
//   - C2 when the mode is Allowlist or Auto-review and permissions.json has
//     no terminalAllowlist;
//   - C3 when the mode is Auto-review, or C2 is «a command that reads
//     files», and sandbox.json does not fix readBoundary;
//   - C4 always.
func ResolveCursor(f CursorFacts, stored map[string]string) CursorState {
	s := CursorState{Applies: map[string]bool{}, Answers: map[string]Answer{}}
	qs := CursorQuestions
	if m, ok := ApprovalModeRunMode(f.ApprovalMode); ok {
		s.Mode, s.ModeRead = Answer{Code: m}, true
	} else {
		s.Applies[C1RunMode] = true
		s.Mode = effective(qs[0], stored)
		s.Answers[C1RunMode] = s.Mode
	}
	if (IsAllowlist(s.Mode.Code) || IsAutoReview(s.Mode.Code)) && !f.TerminalAllowlist {
		s.Applies[C2Allowlist] = true
		s.Answers[C2Allowlist] = effective(qs[1], stored)
	}
	if (IsAutoReview(s.Mode.Code) || s.Answers[C2Allowlist].Code == ListReads) && !f.ReadBoundaryFixed {
		s.Applies[C3ReadAccess] = true
		s.Answers[C3ReadAccess] = effective(qs[2], stored)
	}
	s.Applies[C4ExternalFileOn] = true
	s.Answers[C4ExternalFileOn] = effective(qs[3], stored)
	s.AllStored = true
	for id := range s.Applies {
		_, ok := stored[id]
		s.StoredSome = s.StoredSome || ok
		s.AllStored = s.AllStored && ok
	}
	s.AnyStored = len(stored) > 0
	return s
}

// GrokBotFacts are what RosmarOps reads of Grok Bot at rest (BV-248, CTO-57):
// the app folder, and from ~/.grokbot/settings.json the least controlled
// local tool permission (B1) and whether an account has Auto-review off (B2).
type GrokBotFacts struct {
	Present        bool   // the app folder exists: B0 is not asked
	LocalRead      bool   // settings.json read: B1 is measured
	Local          string // B1's option when read (ask when the file has no key)
	AutoReviewRead bool   // settings.json read with at least one account: B2 is measured
	AutoReview     string // B2's option when read
}

// GrokBotState is Grok Bot's effective answers. Present when its app folder
// exists or B0 is «yes» (BV-248 decision 4 and Albert 16-11 point 3): without
// either there is no Grok Bot agent.
type GrokBotState struct {
	Present   bool
	B0        string // the stored B0 ("" when none)
	Answers   map[string]Answer
	Measured  map[string]bool // B1, B2 read from settings.json: not asked
	Applies   map[string]bool // the questions asked
	AllStored bool
}

// ResolveGrokBot are Grok Bot's effective answers for the facts read and the
// stored answers.
func ResolveGrokBot(f GrokBotFacts, stored map[string]string) GrokBotState {
	s := GrokBotState{B0: stored[B0Uses], Answers: map[string]Answer{}, Measured: map[string]bool{}, Applies: map[string]bool{}}
	if !f.Present {
		s.Applies[B0Uses] = true
	}
	if !f.Present && s.B0 != Yes {
		return s
	}
	s.Present, s.AllStored = true, true
	for _, q := range GrokBotQuestions[1:] {
		switch {
		case q.ID == B1LocalExecution && f.LocalRead:
			s.Answers[q.ID], s.Measured[q.ID] = Answer{Code: f.Local}, true
			continue
		case q.ID == B2AutoReview && f.AutoReviewRead:
			s.Answers[q.ID], s.Measured[q.ID] = Answer{Code: f.AutoReview}, true
			continue
		}
		s.Applies[q.ID] = true
		s.Answers[q.ID] = effective(q, stored)
		_, ok := stored[q.ID]
		s.AllStored = s.AllStored && ok
	}
	return s
}

// LeastLocal is the least controlled of Grok Bot's local tool permission
// values (always > ask > never), as B1; ask without one (the app's own
// default, CTO-57 §3).
func LeastLocal(values []string) string {
	rank := map[string]int{"never": 1, "ask": 2, "always": 3}
	best := "ask"
	if len(values) > 0 {
		best = values[0]
		for _, v := range values[1:] {
			if rank[v] > rank[best] {
				best = v
			}
		}
	}
	switch best {
	case "always":
		return LocalAlways
	case "never":
		return LocalNever
	}
	return LocalAsk
}

// Factory are the factory answers of a product (every question), as a fresh
// install has them: the factory pass and the tests use them.
func Factory(product string) map[string]string {
	out := map[string]string{}
	for _, q := range Questions(product) {
		out[q.ID] = q.Factory
	}
	return out
}

// Status of a product's answers in the report (report.v2 declared.status).
const (
	StatusAnswered   = "answered"
	StatusPartial    = "partial"
	StatusUnanswered = "unanswered"
)

// StatusOf is the status of the answers to the questions that apply: answered
// when each has a stored answer other than unknown, unanswered when none has,
// partial otherwise.
func StatusOf(applies map[string]bool, stored map[string]string) string {
	n, got := 0, 0
	for id := range applies {
		n++
		if v, ok := stored[id]; ok && v != Unknown {
			got++
		}
	}
	switch {
	case n > 0 && got == n:
		return StatusAnswered
	case got == 0:
		return StatusUnanswered
	}
	return StatusPartial
}

// Applied is the stored answers restricted to the questions that apply, in
// question order (the report's answers and summary).
func Applied(product string, applies map[string]bool, stored map[string]string) [][2]string {
	var out [][2]string
	for _, q := range Questions(product) {
		if v, ok := stored[q.ID]; ok && applies[q.ID] {
			out = append(out, [2]string{q.ID, v})
		}
	}
	return out
}

// Clean keeps only valid question and option codes of the product: a state
// edited by hand never puts an unknown code in a report.
func Clean(product string, stored map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range stored {
		if q, ok := QuestionOf(product, k); ok && q.Valid(v) {
			out[k] = v
		}
	}
	return out
}

// Keys are the question ids of m, sorted (for stable output).
func Keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LabelKey is the catalog key of an option's label: decl.unknown for Unknown.
func LabelKey(question, option string) string {
	if option == Unknown {
		return "decl.unknown"
	}
	return "decl." + strings.TrimSpace(question) + "." + option
}

// QuestionKey is the catalog key of a question's text.
func QuestionKey(question string) string { return "decl." + question + ".q" }
