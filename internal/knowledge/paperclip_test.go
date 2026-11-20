package knowledge

import (
	"strings"
	"testing"
)

func TestPaperclipLoadsStrictly(t *testing.T) {
	b, err := LoadPaperclip()
	if err != nil {
		t.Fatal(err)
	}
	if b.FixedVersion.Version != "2026.416.0" {
		t.Errorf("fixed version %q, want 2026.416.0 (CTO-75 §4)", b.FixedVersion.Version)
	}
	if a, ok := b.Adapter("claude-code"); !ok || a.ApprovalOverride != "skips_all" || !a.EnvPassthrough {
		t.Errorf("claude-local adapter: %+v %v", a, ok)
	}
	if a, ok := b.Adapter("codex"); !ok || a.ApprovalOverride != "unknown" {
		t.Errorf("codex-local adapter: %+v %v (it skips approvals only when created through the API)", a, ok)
	}
	if d, ok := b.Default("telemetry_enabled"); !ok || d.Value != "true" {
		t.Errorf("telemetry default: %+v %v (CTO-76 §4)", d, ok)
	}
	if got := b.Ref("claude-local"); got != "kb:paperclip/claude-local@"+PaperclipVersion {
		t.Errorf("ref %q", got)
	}
}

func TestPaperclipRejectsUnknownFieldsAndUnsourcedFacts(t *testing.T) {
	good := string(paperclipJSON)
	for name, raw := range map[string]string{
		"unknown field":  strings.Replace(good, `"source_commit"`, `"extra": 1, "source_commit"`, 1),
		"no source":      strings.Replace(good, `"source_url": "https://github.com/paperclipai/paperclip/blob/575ef12/packages/adapters/claude-local`, `"source_url": "x/claude-local`, 1),
		"bad approval":   strings.Replace(good, `"approval_override": "skips_all"`, `"approval_override": "maybe"`, 1),
		"trailing data":  good + "{}",
		"other version":  strings.Replace(good, `"version": "`+PaperclipVersion+`"`, `"version": "2026-01-01"`, 1),
		"duplicate id":   strings.Replace(good, `"id": "codex-local"`, `"id": "claude-local"`, 1),
		"relative point": strings.Replace(good, `"pointer": "/llm/apiKey"`, `"pointer": "llm/apiKey"`, 1),
	} {
		if _, err := decodePaperclip([]byte(raw)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

func TestPaperclipVersionBefore(t *testing.T) {
	for _, c := range []struct {
		v    string
		want bool
	}{
		{"2026.415.0", true},
		{"2026.416.0", false},
		{"2026.1117.0", false},
		{"v2026.410.3", true},
		{"0.3.1", true},
		{"latest", false},
		{"", false},
	} {
		if got := VersionBefore(c.v, "2026.416.0"); got != c.want {
			t.Errorf("VersionBefore(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}
