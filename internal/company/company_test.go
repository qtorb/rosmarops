package company

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseList(t *testing.T) {
	got, err := ParseList([]byte("# RosmarOps accepted binaries\n" + sha + "  0.16.0\n" + strings.Repeat("a", 64) + "  0.15.1-rc.1\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{sha, "0.16.0"}, {strings.Repeat("a", 64), "0.15.1-rc.1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if !Lists(got, sha, "0.16.0") || Lists(got, sha, "0.16.1") || Lists(got, strings.Repeat("b", 64), "0.16.0") {
		t.Error("Lists matches on fingerprint and version together")
	}
	for name, bad := range map[string]string{
		"one space":       sha + " 0.16.0\n",
		"uppercase hex":   strings.ToUpper(sha) + "  0.16.0\n",
		"short hash":      sha[:63] + "  0.16.0\n",
		"no version":      sha + "\n",
		"not semver":      sha + "  0.16\n",
		"trailing space":  sha + "  0.16.0 \n",
		"blank line":      sha + "  0.16.0\n\n",
		"one bad of many": sha + "  0.16.0\nnot a line\n",
	} {
		if _, err := ParseList([]byte(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]byte(`{"schema_version":1,"summary":{"enabled":true},"first_run":{"repo_roots":["~/source","~/projects"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	l := Loaded{State: Trusted, Config: cfg}
	if !l.SummaryEnabled() || !reflect.DeepEqual(l.RepoRoots(), []string{"~/source", "~/projects"}) {
		t.Fatalf("got %+v", cfg)
	}
	if (Loaded{State: Untrusted, Config: cfg}).SummaryEnabled() {
		t.Error("an untrusted configuration turns the summary on")
	}
	// The company cannot hide findings or move the report: every key outside
	// company.v1 makes the file invalid.
	for _, extra := range []string{
		`"allow_list":{}`, `"dismissed":[]`, `"extra_secret_patterns":[]`, `"report_directory":"/tmp"`,
		`"space":{}`, `"problems":{}`, `"lang":"es"`,
	} {
		raw := `{"schema_version":1,"summary":{"enabled":true},` + extra + `}`
		if _, err := parseConfig([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", extra)
		}
	}
	for _, bad := range []string{
		`{"schema_version":2,"summary":{"enabled":true}}`,
		`{"schema_version":1}`,
		`{"schema_version":1,"summary":{"enabled":true},"first_run":{"repo_roots":["/abs"]}}`,
		`{"schema_version":1,"summary":{"enabled":true,"x":1}}`,
	} {
		if _, err := parseConfig([]byte(bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

func TestResolveRoots(t *testing.T) {
	home := filepath.FromSlash("/home/fakeuser")
	got, err := ResolveRoots(home, []string{"~/source", "~/work/projects"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, "source"), filepath.Join(home, "work", "projects")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{"~/../other", "~/a/../../b", `~/a\..\..\b`, "/etc"} {
		if _, err := ResolveRoots(home, []string{"~/ok", bad}); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

func TestNoCompanyFolder(t *testing.T) {
	if l := LoadConfig(""); l.State != Absent {
		t.Errorf("no folder: %+v", l)
	}
	if _, ok := LoadList(""); ok {
		t.Error("no folder: a list")
	}
	if l := LoadConfig(t.TempDir()); l.State != Absent {
		t.Errorf("no file: %+v", l)
	}
}
