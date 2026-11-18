package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/company"
)

// Only a trusted company configuration, used because the person has no
// config.json, marks the company's group task as expected. An untrusted or
// invalid file, or a person's own config.json, leaves it reported.
func TestCompanyInUse(t *testing.T) {
	home := t.TempDir()
	missing := filepath.Join(home, ".rosmarops", "config.json")
	own := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(own, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		state, cfgPath string
		want           bool
	}{
		"trusted, no config.json":       {company.Trusted, missing, true},
		"untrusted, no config.json":     {company.Untrusted, missing, false},
		"invalid, no config.json":       {company.Invalid, missing, false},
		"no company file":               {company.Absent, missing, false},
		"trusted, person's config.json": {company.Trusted, own, false},
	} {
		comp := &companyRun{loaded: company.Loaded{State: c.state, Path: "company.json"}, source: configSourceUser}
		_, _, _ = resolveRunConfig(c.cfgPath, home, comp)
		if got := comp.inUse(); got != c.want {
			t.Errorf("%s: inUse = %v, want %v", name, got, c.want)
		}
	}
	var none *companyRun
	if none.inUse() {
		t.Error("nil companyRun in use")
	}
}
