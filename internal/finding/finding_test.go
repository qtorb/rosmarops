package finding_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
)

func TestReducer_AllGreen(t *testing.T) {
	status := finding.Reducer(nil, 0)
	if status != finding.StatusGreen {
		t.Errorf("empty findings, 0 errors: got %q, want %q", status, finding.StatusGreen)
	}
}

func TestReducer_InfoOnlyIsGreen(t *testing.T) {
	findings := []finding.Finding{
		{Severity: finding.SeverityInfo},
	}
	status := finding.Reducer(findings, 0)
	if status != finding.StatusGreen {
		t.Errorf("info-only: got %q, want %q", status, finding.StatusGreen)
	}
}

func TestReducer_OneYellow(t *testing.T) {
	findings := []finding.Finding{
		{Severity: finding.SeverityYellow},
	}
	status := finding.Reducer(findings, 0)
	if status != finding.StatusYellow {
		t.Errorf("one yellow: got %q, want %q", status, finding.StatusYellow)
	}
}

func TestReducer_OneRed(t *testing.T) {
	findings := []finding.Finding{
		{Severity: finding.SeverityRed},
	}
	status := finding.Reducer(findings, 0)
	if status != finding.StatusRed {
		t.Errorf("one red: got %q, want %q", status, finding.StatusRed)
	}
}

func TestReducer_RedTrumpsYellow(t *testing.T) {
	findings := []finding.Finding{
		{Severity: finding.SeverityYellow},
		{Severity: finding.SeverityRed},
		{Severity: finding.SeverityInfo},
	}
	status := finding.Reducer(findings, 0)
	if status != finding.StatusRed {
		t.Errorf("red+yellow: got %q, want %q", status, finding.StatusRed)
	}
}

func TestReducer_ScanErrorForcesYellow(t *testing.T) {
	// No findings at all, but a scan error → yellow (fail-secure).
	status := finding.Reducer(nil, 1)
	if status != finding.StatusYellow {
		t.Errorf("0 findings, 1 scan error: got %q, want %q", status, finding.StatusYellow)
	}
}

func TestReducer_ScanErrorDoesNotOverrideRed(t *testing.T) {
	findings := []finding.Finding{
		{Severity: finding.SeverityRed},
	}
	status := finding.Reducer(findings, 5)
	if status != finding.StatusRed {
		t.Errorf("red+errors: got %q, want %q", status, finding.StatusRed)
	}
}

func TestReducer_GreenRequiresZeroErrors(t *testing.T) {
	// Green is only possible with zero findings AND zero errors.
	// Even with zero findings, a non-zero error count forces yellow.
	for _, errCount := range []int{1, 2, 100} {
		status := finding.Reducer([]finding.Finding{}, errCount)
		if status != finding.StatusYellow {
			t.Errorf("0 findings, %d errors: got %q, want yellow", errCount, status)
		}
	}
}

func TestNewFinding_FieldsSet(t *testing.T) {
	f := finding.NewFinding("binary_hash_changed:abcd1234abcd1234", finding.SeverityRed, "binary_hash_changed", "Binary hash changed")
	if f.ID != "binary_hash_changed:abcd1234abcd1234" {
		t.Errorf("ID: got %q", f.ID)
	}
	if f.Severity != finding.SeverityRed {
		t.Errorf("Severity: got %q", f.Severity)
	}
	if f.FirstSeen == "" {
		t.Error("FirstSeen must be set by NewFinding")
	}
	if !f.IsNewSinceLast {
		t.Error("IsNewSinceLast must be true for new findings")
	}
}
