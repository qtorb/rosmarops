package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSteps(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.steps")
	if err := os.WriteFile(p, []byte("# a comment\nif-asked usage.ask.prompt send n\\r\nexpect console.binary_confirm timeout 90s\nsend s\\r\nexpect-exit 0 timeout 600s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	steps, err := parseSteps(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []step{
		{kind: "if-asked", arg: "usage.ask.prompt", answer: `n\r`, line: 2},
		{kind: "expect", arg: "console.binary_confirm", timeout: 90, line: 3},
		{kind: "send", arg: `s\r`, line: 4},
		{kind: "expect-exit", arg: "0", timeout: 600, line: 5},
	}
	if len(steps) != len(want) {
		t.Fatalf("%+v", steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Errorf("step %d: %+v, want %+v", i, steps[i], want[i])
		}
	}
	if err := os.WriteFile(p, []byte("wait 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSteps(p); err == nil {
		t.Error("an unknown step was accepted")
	}
}

func TestPatternMatchesCatalogTextAcrossLines(t *testing.T) {
	cat := map[string]string{"k": "RosmarOps has changed since the last run. Before: {old}."}
	out := squeeze(plain("\x1b[1mRosmarOps has changed\r\nsince the last run. Before:\x1b[0m 0.15.0."))
	if !pattern("k", cat).MatchString(out) {
		t.Errorf("no match in %q", out)
	}
	if !pattern("plain words", cat).MatchString("some plain words here") {
		t.Error("a literal argument does not match")
	}
}

func TestUnescapeAndCatalog(t *testing.T) {
	if got := unescape(`s\r\n\t`); got != "s\r\n\t" {
		t.Errorf("%q", got)
	}
	cat, err := loadCatalog(filepath.Join("..", "..", "..", "..", "internal", "catalog", "messages"), "es")
	if err != nil || cat["console.binary_confirm"] == "" {
		t.Fatalf("catalog: %v", err)
	}
}
