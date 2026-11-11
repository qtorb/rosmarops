package notify

import (
	"strings"
	"testing"
)

// testPFN is a stand-in for the package family name GetCurrentPackageFamilyName
// returns when RosmarOps runs packaged (Albert's Store identity).
const testPFN = "AlbertGarciaPujadas.RosmarOps_2a9trf99pxwv0"

// TestBV213AUMID: with a (simulated) package name the toast's AUMID is the
// package's, <PackageFamilyName>!RosmarOps; without a package it is today's,
// Windows PowerShell's identity.
func TestBV213AUMID(t *testing.T) {
	if got := AUMIDFor(""); got != AppID {
		t.Errorf("AUMIDFor(\"\") = %q, want PowerShell AppID %q", got, AppID)
	}
	want := testPFN + "!RosmarOps"
	if got := AUMIDFor(testPFN); got != want {
		t.Errorf("AUMIDFor(%q) = %q, want %q", testPFN, got, want)
	}
	if AppName != "RosmarOps" {
		t.Errorf("AppName = %q, want RosmarOps (the AppxManifest <Application Id>)", AppName)
	}
}

// TestBV213ScriptUsesAUMID: the notifier is created with the selected AUMID —
// the package's when packaged, PowerShell's (today's) when not.
func TestBV213ScriptUsesAUMID(t *testing.T) {
	pkg := AUMIDFor(testPFN)
	if s := scriptFor(pkg); !strings.Contains(s, "CreateToastNotifier('"+pkg+"')") {
		t.Errorf("packaged script does not create the notifier with the package AUMID %q:\n%s", pkg, s)
	}
	if !strings.Contains(Script, "CreateToastNotifier('"+AppID+"')") {
		t.Errorf("default script does not use PowerShell's AppID %q:\n%s", AppID, Script)
	}
	if a := argsFor(AUMIDFor("")); len(a) != 4 || a[3] != Script {
		t.Errorf("argsFor(unpackaged) = %v, want 4 args whose last is Script", a)
	}
}

// TestBV213ScriptSingleQuoteSafe: a single quote in the AUMID is doubled, so it
// can never break out of the PowerShell single-quoted literal.
func TestBV213ScriptSingleQuoteSafe(t *testing.T) {
	if got := psSingleQuote("a'b"); got != "a''b" {
		t.Errorf("psSingleQuote(%q) = %q, want %q", "a'b", got, "a''b")
	}
	if s := scriptFor("x'y!RosmarOps"); !strings.Contains(s, "CreateToastNotifier('x''y!RosmarOps')") {
		t.Errorf("script did not escape the single quote in the AUMID:\n%s", s)
	}
}
