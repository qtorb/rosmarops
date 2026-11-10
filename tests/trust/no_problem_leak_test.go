package trust_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/spacenet"
)

// BV-210 §7: the problem report that leaves the machine never carries a path,
// a user or computer name, a key or config text — even if the local notebook
// has been hand-edited to stuff those values into every field. The notebook
// keeps only closed-list values (code, step, a known product id, HTTP status,
// day, count), so a tampered line is stripped on read and nothing sensitive can
// reach the sent body.
func TestProblemSendCarriesNoSecrets(t *testing.T) {
	dir := t.TempDir()

	// Sensitive values a tampered or malicious notebook might try to smuggle.
	secretsPlanted := []string{
		`C:\Users\albert\secrets`, // a Windows path with the username
		`/home/albert/.aws/creds`, // a POSIX path with the username
		"albert",                  // the user name on its own
		"DESKTOP-ALBERT",          // the computer name
		"sk-ABCDEF0123456789",     // a fake key
		`{"apiKey":"shh"}`,        // config text
		"192.168.1.42",            // an IP
	}

	// A notebook with the planted values in every field that takes a string,
	// plus two well-formed lines. ValidCode/ValidStep/ValidAgent are closed
	// sets, so the stuffed lines are dropped or stripped on read.
	lines := []string{
		`{"code":"config_unreadable","step":"C:\\Users\\albert\\secrets","agent":"albert","day":"2026-11-09","count":1}`,
		`{"code":"step_failed","step":"albert","agent":"/home/albert/.aws/creds","day":"2026-11-09","count":1}`,
		`{"code":"sk-ABCDEF0123456789","step":"mcp","day":"2026-11-09","count":1}`,
		`{"code":"space_rejected","step":"space_send","agent":"DESKTOP-ALBERT","http_status":409,"day":"2026-11-09","count":2}`,
		`{"code":"config_unreadable","step":"agents","agent":"cline","day":"2026-11-09","count":1}`,
		`{"code":"step_failed","step":"mcp","day":"2026-11-09","count":3}`,
	}
	if err := os.WriteFile(filepath.Join(dir, problems.NotebookName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unsent, err := problems.New(dir).Unsent(problems.MaxPerSend)
	if err != nil {
		t.Fatal(err)
	}
	// os_version carries a planted-looking value too, to prove the envelope is
	// clean even if a platform fed it junk — but it only accepts its pattern.
	sys := problems.System{Lang: "en", Version: "0.10.3", OS: "windows", OSVersion: "10.0.26100", Arch: "amd64"}
	body, err := problems.Marshal(problems.BuildReport(sys, unsent, nil, nil))
	if err != nil {
		t.Fatalf("report should still build and validate: %v", err)
	}
	s := string(body)

	for _, planted := range secretsPlanted {
		if strings.Contains(s, planted) {
			t.Errorf("sent body leaks a planted value %q:\n%s", planted, s)
		}
	}
	// No path separators anywhere in the body (§7).
	for _, sep := range []string{"/", `\`} {
		if strings.Contains(s, sep) {
			t.Errorf("sent body contains a path separator %q:\n%s", sep, s)
		}
	}
	// What survives is only the closed-list values.
	if !strings.Contains(s, `"cline"`) || !strings.Contains(s, `"space_rejected"`) {
		t.Errorf("the well-formed lines should survive:\n%s", s)
	}
}

// BV-210 §7: the problem send can only reach app.rosmarops.com over HTTPS. The
// one client spacenet builds refuses every other host before any dial, so even
// a bug in the send path cannot send a problem report anywhere else.
func TestProblemSendClientRefusesOtherHosts(t *testing.T) {
	c := spacenet.Client()
	for _, url := range []string{
		"https://evil.example.com/api/v1/problems",
		"https://app.rosmarops.com.evil.com/api/v1/problems",
		"http://app.rosmarops.com/api/v1/problems", // wrong scheme
	} {
		resp, err := c.Get(url)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Errorf("client should refuse %q", url)
		}
	}
}
