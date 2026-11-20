package knowledge

import (
	"strings"
	"testing"
)

func TestOrchestratorsLoadStrictly(t *testing.T) {
	b, err := LoadOrchestrators()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := b.Product(PaseoProduct)
	if l, found := p.LaunchByID("claude"); !ok || !found || !l.Skips("bypassPermissions") || l.Skips("auto") || l.Agent != "claude-code" {
		t.Errorf("Paseo's claude: %+v", l)
	}
	s, _ := b.Product(ClaudeSquadProduct)
	if l, _ := s.LaunchByID("codex"); l.Skips("auto_yes") {
		t.Error("Claude Squad's auto_yes answers no Codex prompt (tmux.go:253-260 at v1.0.20)")
	}
	o, _ := b.Product(OrcaProduct)
	if l, _ := o.Launch("claude-code"); l.DefaultOverride != "skips_all" {
		t.Errorf("Orca's default for Claude Code: %+v", l)
	}
	if got := o.Ref("claude"); got != "kb:orca/claude@"+OrchestratorsVersion {
		t.Errorf("ref %q", got)
	}
}

func TestOrchestratorsRejectBadFacts(t *testing.T) {
	good := string(orchestratorsJSON)
	for name, raw := range map[string]string{
		"unknown field":   strings.Replace(good, `"release": "v0.16.2"`, `"release": "v0.16.2", "x": 1`, 1),
		"not at release":  strings.Replace(good, "paseo/blob/v0.16.2/packages/protocol", "paseo/blob/main/packages/protocol", 1),
		"unknown product": strings.Replace(good, `"product": "orca"`, `"product": "paradev"`, 1),
		"bad override":    strings.Replace(good, `"default_override": "skips_all"`, `"default_override": "maybe"`, 1),
		"trailing":        good + "{}",
		"duplicate":       strings.Replace(good, `"id": "codex"`, `"id": "claude"`, 1),
	} {
		if raw == good {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if _, err := decodeOrchestrators([]byte(raw)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}
