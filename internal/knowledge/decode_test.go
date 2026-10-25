package knowledge

import "testing"

func TestDecode_RejectsUnknownFieldsAndGaps(t *testing.T) {
	cases := map[string]string{
		"unknown root field":  `{"product":"p","version":"v","entries":[],"extra":1}`,
		"unknown entry field": `{"product":"p","version":"v","entries":[{"id":"a","type":"read_files","approval":"allow","target_scope":"*","source_url":"u","verified":"v","note":"x"}]}`,
		"empty entry field":   `{"product":"p","version":"v","entries":[{"id":"a","type":"","approval":"allow","target_scope":"*","source_url":"u","verified":"v"}]}`,
		"duplicate id":        `{"product":"p","version":"v","entries":[{"id":"a","type":"read_files","approval":"allow","target_scope":"*","source_url":"u","verified":"v"},{"id":"a","type":"read_files","approval":"allow","target_scope":"*","source_url":"u","verified":"v"}]}`,
		"no version":          `{"product":"p","entries":[]}`,
		"trailing data":       `{"product":"p","version":"v","entries":[]} {}`,
	}
	for name, raw := range cases {
		if _, err := decode([]byte(raw)); err == nil {
			t.Errorf("%s: got nil error", name)
		}
	}
}

func TestDecode_PathListValidatedStrictly(t *testing.T) {
	const head = `{"product":"p","version":"v","entries":[{"id":"a","type":"t","approval":"x","target_scope":"s","source_url":"u","verified":"d",`
	for name, tail := range map[string]string{
		"empty path":            `"paths":[""]}]}`,
		"padded path":           `"paths":[" .git/"]}]}`,
		"duplicate path":        `"paths":[".git/",".git/"]}]}`,
		"exceptions only":       `"exceptions":[".claude/x/"]}]}`,
		"backslash":             `"paths":["a\\b"]}]}`,
		"path list not strings": `"paths":[1]}]}`,
	} {
		if _, err := decode([]byte(head + tail)); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
	if _, err := decode([]byte(head + `"paths":[".git/"],"exceptions":[".git/x/"]}]}`)); err != nil {
		t.Errorf("valid path list: %v", err)
	}
}

// A mitigation with an unknown way or a value that is not JSON is rejected.
func TestDecode_RejectsBadMitigation(t *testing.T) {
	for _, m := range []string{
		`{"id":"x","setting":"permissions.y","completeness":"full","cuts":["nowhere"],"narrows":[],"magnitude":1,"source_url":"u","verified":"v"}`,
		`{"id":"x","setting":"permissions.y","value":"tru","completeness":"full","cuts":[],"narrows":[],"magnitude":1,"source_url":"u","verified":"v"}`,
		`{"id":"x","setting":"permissions.ask","completeness":"full","cuts":[],"narrows":[],"magnitude":1,"source_url":"u","verified":"v"}`,
	} {
		raw := `{"product":"p","version":"v","entries":[],"mitigations":[` + m + `],"code_launchers":[]}`
		if _, err := decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", m)
		}
	}
}
