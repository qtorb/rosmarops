package memory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var salt = []byte("0123456789abcdef0123456789abcdef")

func run(day string) Run {
	return Run{Date: day, Report: "report-" + day + "__RED__1R-0Y", RulesVersion: "1", KnowledgeVersion: "k", Agents: []string{"Claude Code"}, Repos: 10, Complete: true}
}

func fnd(id string, creds ...string) Finding {
	return Finding{ID: id, Rule: "cred_read_and_send", Severity: "red", Agent: "agent_client_x", Creds: creds}
}

// M1: new, then still_observed, no_longer_observed, observed_again.
func TestVerdicts(t *testing.T) {
	m, v, root := Update(File{}, run("2026-10-01"), []Finding{fnd("f_a")}, salt)
	if v["f_a"].Verdict != VerdictNew || v["f_a"].FirstSeen != "2026-10-01" || root.Counts[VerdictNew] != 1 {
		t.Fatalf("first run: %+v %+v", v, root)
	}
	m, v, _ = Update(m, run("2026-10-08"), []Finding{fnd("f_a"), fnd("f_b")}, salt)
	if v["f_a"].Verdict != VerdictStillObserved || v["f_a"].OpenSince != "2026-10-01" || v["f_b"].Verdict != VerdictNew {
		t.Fatalf("second run: %+v", v)
	}
	m, _, root = Update(m, run("2026-10-15"), []Finding{fnd("f_b")}, salt)
	if root.Counts[VerdictNoLongerObserved] != 1 || len(root.NoLongerObserved) != 1 ||
		root.NoLongerObserved[0] != (Gone{Rule: "cred_read_and_send", Severity: "red", OpenSince: "2026-10-01", LastReport: "report-2026-10-08__RED__1R-0Y"}) {
		t.Fatalf("third run: %+v", root)
	}
	_, v, root = Update(m, run("2026-10-22"), []Finding{fnd("f_a"), fnd("f_b")}, salt)
	if v["f_a"].Verdict != VerdictObservedAgain || v["f_a"].Reappeared != 1 || v["f_a"].OpenSince != "2026-10-22" || v["f_a"].FirstSeen != "2026-10-01" {
		t.Fatalf("fourth run: %+v %+v", v, root)
	}
}

// M4: other rules or knowledge, or another scope, make new and
// no_longer_observed not_comparable; still_observed stays.
func TestNotComparable(t *testing.T) {
	base, _, _ := Update(File{}, run("2026-10-01"), []Finding{fnd("f_a"), fnd("f_gone")}, salt)
	for name, mod := range map[string]func(*Run){
		ReasonRulesChanged: func(r *Run) { r.RulesVersion = "2" },
		ReasonScopeChanged: func(r *Run) { r.Repos = 5 },
	} {
		r := run("2026-10-08")
		mod(&r)
		_, v, root := Update(base, r, []Finding{fnd("f_a"), fnd("f_new")}, salt)
		if root.Reason != name || v["f_a"].Verdict != VerdictStillObserved || v["f_new"].Verdict != VerdictNotComparable ||
			root.Counts[VerdictNotComparable] != 2 || len(root.NoLongerObserved) != 0 {
			t.Errorf("%s: %+v %+v", name, v, root)
		}
	}
}

// M4: an incomplete run leaves the absent findings not_checked and open; the
// next complete run compares with the last checked one.
func TestNotChecked(t *testing.T) {
	base, _, _ := Update(File{}, run("2026-10-01"), []Finding{fnd("f_a")}, salt)
	r := run("2026-10-08")
	r.Complete = false
	m, _, root := Update(base, r, nil, salt)
	if root.Counts[VerdictNotChecked] != 1 || len(root.NoLongerObserved) != 0 || m.LastChecked.Date != "2026-10-01" {
		t.Fatalf("incomplete: %+v %+v", root, m.LastChecked)
	}
	_, v, _ := Update(m, run("2026-10-15"), []Finding{fnd("f_a")}, salt)
	if v["f_a"].Verdict != VerdictStillObserved || v["f_a"].OpenSince != "2026-10-01" {
		t.Errorf("after the gap: %+v", v)
	}
}

// §1.3: a gone and a new of one rule, one agent and a credential in common
// are one moved line, not in the no-longer-observed list.
func TestMoved(t *testing.T) {
	base, _, _ := Update(File{}, run("2026-10-01"), []Finding{fnd("f_old", "cred_1", "cred_2"), fnd("f_other", "cred_9")}, salt)
	_, _, root := Update(base, run("2026-10-08"), []Finding{fnd("f_new", "cred_2", "cred_3")}, salt)
	if len(root.Moved) != 1 || root.Moved[0].FindingID != "f_new" || root.Moved[0].OpenSince != "2026-10-01" || len(root.NoLongerObserved) != 1 {
		t.Errorf("moved %+v, gone %+v", root.Moved, root.NoLongerObserved)
	}
}

// §1.2: closed entries go after 26 runs or 180 days; open ones stay; at most
// 1,000, the oldest closed first.
func TestRetention(t *testing.T) {
	m, _, _ := Update(File{}, run("2026-01-01"), []Finding{fnd("f_gone"), fnd("f_open")}, salt)
	m, _, _ = Update(m, run("2026-01-02"), []Finding{fnd("f_open")}, salt)
	for i := 0; i < KeepRuns-1; i++ {
		m, _, _ = Update(m, run("2026-01-03"), []Finding{fnd("f_open")}, salt)
	}
	if n := len(m.Entries); n != 1 {
		t.Errorf("after %d runs: %d entries", KeepRuns, n)
	}
	m, _, _ = Update(File{}, run("2026-01-01"), []Finding{fnd("f_gone"), fnd("f_open")}, salt)
	m, _, _ = Update(m, run("2026-01-02"), []Finding{fnd("f_open")}, salt)
	if m, _, _ = Update(m, run("2026-07-01"), []Finding{fnd("f_open")}, salt); len(m.Entries) != 1 {
		t.Errorf("after 180 days: %d entries", len(m.Entries))
	}
	var many []Finding
	for i := 0; i < MaxEntries+20; i++ {
		many = append(many, fnd("f_"+strconv.Itoa(i)))
	}
	m, _, _ = Update(File{}, run("2026-01-01"), many, salt)
	if len(m.Entries) != MaxEntries {
		t.Errorf("cap: %d", len(m.Entries))
	}
}

// M2: the file validates, holds no raw id; an invalid one is set aside as
// .invalid, never over another.
func TestReadWrite(t *testing.T) {
	dir := t.TempDir()
	m, _, _ := Update(File{}, run("2026-10-01"), []Finding{fnd("f_secret_path_hash", "cred_abc")}, salt)
	if err := Write(dir, m); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, never := range []string{"f_secret_path_hash", "cred_abc", "agent_client_x"} {
		if strings.Contains(string(raw), never) {
			t.Errorf("memory holds %q", never)
		}
	}
	back, reset, err := Read(dir)
	if err != nil || reset || len(back.Entries) != 1 {
		t.Fatalf("read: %+v %v %v", back, reset, err)
	}
	for i, want := range []string{FileName + ".invalid", FileName + ".1.invalid"} {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"schema_version":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		f, reset, err := Read(dir)
		if err != nil || !reset || len(f.Entries) != 0 {
			t.Errorf("invalid %d: %v %v", i, reset, err)
		}
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("not set aside as %s", want)
		}
	}
}
