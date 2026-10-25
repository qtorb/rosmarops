package rules

import (
	"reflect"
	"testing"
)

func act(id, completeness string, magnitude, points, files int) Action {
	return Action{ID: id, Code: "c", Completeness: completeness, Magnitude: magnitude, points: points, files: files}
}

// §2.2 with A1: the largest reduction per magnitude, then the action that
// touches more findings, then a single file, then the larger reduction,
// then the id. Deterministic: the same input gives the same pick.
func TestPickToday_TieBreaks(t *testing.T) {
	radius := act("a_radius", CompletenessRadiusOnly, 2, 2, 1) // A1: radius for any_project = 2 points, 2/2
	ask := act("a_ask", CompletenessPartial, 1, 1, 1)          // 1/1
	mode := act("a_mode", CompletenessFull, 3, 2, 1)           // 2/3
	remove := act("a_remove", CompletenessFull, 3, 2, 2)       // 2/3, two files

	f := Finding{ID: "f_1", Severity: SeverityRed, Radius: RadiusAnyProject, Actions: []Action{ask, radius, mode, remove}}
	other := Finding{ID: "f_2", Severity: SeverityRed, Actions: []Action{radius}}

	// Equal score: radius touches two findings, ask one.
	if got := pickToday([]Finding{f, other}); got.ActionID != "a_radius" {
		t.Errorf("touches: got %s", got.ActionID)
	}
	// Equal score and touches: the larger reduction (radius, 2 points).
	if got := pickToday([]Finding{f}); got.ActionID != "a_radius" || got.Remains == nil || got.Remains.Radius != RadiusSameProject {
		t.Errorf("points: got %+v", got)
	}
	// Without A1 (radius 1 point): ask's 1/1 beats radius's 1/2.
	weak := act("a_radius", CompletenessRadiusOnly, 2, 1, 1)
	if got := pickToday([]Finding{{ID: "f_1", Actions: []Action{weak, ask}}}); got.ActionID != "a_ask" {
		t.Errorf("score: got %s", got.ActionID)
	}
	// Equal score, touches and points: a single file beats two.
	if got := pickToday([]Finding{{ID: "f_1", Actions: []Action{remove, mode}}}); got.ActionID != "a_mode" || got.Remains != nil {
		t.Errorf("single file: got %+v", got)
	}
	// Everything equal: the id.
	x, y := act("a_x", CompletenessFull, 3, 2, 1), act("a_y", CompletenessFull, 3, 2, 1)
	for _, order := range [][]Action{{x, y}, {y, x}} {
		if got := pickToday([]Finding{{ID: "f_1", Actions: order}}); got.ActionID != "a_x" {
			t.Errorf("id: got %s", got.ActionID)
		}
	}
	if got := pickToday(nil); got.Code != TodayNothingUrgent {
		t.Errorf("nothing: got %+v", got)
	}
}

// §2.1: severity, impact class, confidence, radius, group size, id.
func TestSortFindings(t *testing.T) {
	mk := func(id, rule, class, sev, conf, radius string, count int) Finding {
		return Finding{ID: id, Rule: rule, class: class, Severity: sev, Confidence: conf, Radius: radius, Group: Group{Count: count}}
	}
	fs := []Finding{
		mk("f_9", RuleOwnInstructionsWritable, "", SeverityYellow, ConfidenceHigh, "", 15),
		mk("f_8", RuleCredReadAndSend, ClassMedium, SeverityYellow, ConfidenceHigh, RadiusAnyProject, 1),
		mk("f_7", RuleCredReadable, ClassHigh, SeverityYellow, ConfidenceHigh, RadiusAnyProject, 1),
		mk("f_6", RuleForeignAgentInstructionsWritable, "", SeverityYellow, ConfidenceHigh, "", 1),
		mk("f_5", RuleOwnConfigWritable, "", SeverityRed, ConfidenceHigh, "", 1),
		mk("f_4", RuleForeignAgentConfigWritable, "", SeverityRed, ConfidenceMedium, "", 1),
		mk("f_3", RuleCredReadAndSend, ClassHigh, SeverityRed, ConfidenceMedium, RadiusSameProject, 9),
		mk("f_2", RuleCredReadAndSend, ClassHigh, SeverityRed, ConfidenceMedium, RadiusAnyProject, 1),
		mk("f_1", RuleCredReadAndSend, ClassHigh, SeverityRed, ConfidenceHigh, RadiusSameProject, 1),
		mk("f_0", RuleCredReadAndSend, ClassHigh, SeverityRed, ConfidenceHigh, RadiusSameProject, 2),
	}
	sortFindings(fs)
	var got []string
	for _, f := range fs {
		got = append(got, f.ID)
	}
	want := []string{"f_0", "f_1", "f_2", "f_3", "f_4", "f_5", "f_7", "f_6", "f_9", "f_8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
}

// A template is .env.example, .env.sample, .env.template or a name ending in
// .example (A2).
func TestIsTemplate(t *testing.T) {
	for f, want := range map[string]bool{
		"/r/.env.example": true, `C:\r\.env.sample`: true, "/r/.env.template": true, "/r/config.json.example": true,
		"/r/.ENV.EXAMPLE": true, "/r/.env": false, "/r/.env.local": false, "/r/example.env": false, "/r/.env.examples": false,
	} {
		if isTemplate(f) != want {
			t.Errorf("%s: %v", f, !want)
		}
	}
}

func TestRulePath(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\Albert\webapp\dashboard\.env.local`: "//c/Users/Albert/webapp/dashboard/.env.local",
		"/home/albert/repo/.env":                      "//home/albert/repo/.env",
	} {
		if got, ok := rulePath(in); got != want || !ok {
			t.Errorf("%s: %s %v, want %s", in, got, ok, want)
		}
	}
	// BV-38: no rule for a path with a pattern character.
	for _, in := range []string{"/src/a[1]/.env", "/src/x*.env", "/src/what?/.env", "/src/b]/.env"} {
		if got, ok := rulePath(in); ok {
			t.Errorf("%s: rule path %q", in, got)
		}
	}
}

// M12, §4: the total order of the title, one level at a time.
func TestTitleBefore(t *testing.T) {
	mk := func(class, typ, service, scope string, observed bool, id string) Finding {
		return Finding{class: class, credType: typ, observed: observed, credID: id, Params: Params{Service: service, Scope: scope}}
	}
	for _, c := range []struct {
		name string
		a, b Finding
	}{
		{"class", mk(ClassHigh, "api_key", "x", "", false, "z"), mk(ClassMedium, "api_key", "a", "", true, "a")},
		{"level A over B", mk(ClassHigh, "db_credential", "unknown", "", false, "z"), mk(ClassHigh, "api_key", "aws", "", true, "a")},
		{"level B over C", mk(ClassUnclassified, "api_key", "github", "", false, "z"), mk(ClassUnclassified, "api_key", "npm", "", true, "a")},
		{"observed over inferred", mk(ClassHigh, "api_key", "supabase", "service_role", true, "z"), mk(ClassHigh, "api_key", "supabase", "secret", false, "a")},
		{"service", mk(ClassHigh, "api_key", "a", "", false, "z"), mk(ClassHigh, "api_key", "b", "", false, "a")},
		{"scope", mk(ClassHigh, "api_key", "supabase", "secret", false, "z"), mk(ClassHigh, "api_key", "supabase", "service_role", false, "a")},
		{"id", mk(ClassHigh, "api_key", "s", "x", false, "a"), mk(ClassHigh, "api_key", "s", "x", false, "b")},
	} {
		if !titleBefore(c.a, c.b) || titleBefore(c.b, c.a) {
			t.Errorf("%s: order not as §4", c.name)
		}
	}
}
