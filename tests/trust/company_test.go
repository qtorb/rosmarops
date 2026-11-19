package trust_test

// Trust tests of a managed computer (the company configuration): the real
// binary, built with the company folder inside the sandbox (company.testRoot
// and the test owner, set by -ldflags only in test builds), run as a
// scheduled review. They prove what the company can and cannot read and set,
// that only the administrator's list or the Store accepts a binary without
// the person, and that nothing is written to the company folder.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"

	"github.com/qtorb/rosmarops/internal/report"
)

const companyPkg = "github.com/qtorb/rosmarops/internal/company"

// companyFlags are the ldflags of a company-mode test build: the program's
// version, the company folder <root>/machine/rosmarops, owned by the test's
// user standing in for root, and the walk to / stopping at <root>/machine.
// owner, when not empty, is the uid the build trusts instead.
func companyFlags(version, owner, stopSuffix string) func(root string) string {
	return func(root string) string {
		if owner == "" {
			owner = strconv.Itoa(os.Getuid())
		}
		return fmt.Sprintf("-X main.version=%s -X %s.testRoot=%s -X %s.testOwnerUID=%s -X %s.testStop=%s",
			version, companyPkg, companyFolder(root), companyPkg, owner, companyPkg, filepath.Join(root, "machine")+stopSuffix)
	}
}

// skipWithoutCompanyMode skips the black-box company tests off Linux: macOS
// has no company mode, and on Windows the sandboxed run is not implemented;
// there internal/company's tests check the permissions on the runner and the
// company-scripts-windows job runs a real install and review.
func skipWithoutCompanyMode(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the black-box company tests run on Linux")
	}
}

func companyFolder(root string) string { return filepath.Join(root, "machine", "rosmarops") }

// companyJSON is a company configuration with the summary on or off.
func companyJSON(enabled bool) string {
	return fmt.Sprintf(`{"schema_version":1,"summary":{"enabled":%t},"first_run":{"repo_roots":["~/source","~/projects"]}}`, enabled)
}

// newCompanySandbox is the sandbox of a managed computer: the person's
// folder is «fakeuser», the company folder holds company.json, and the
// administrator's list holds the sandbox's binary with its version.
func newCompanySandbox(t *testing.T, version, company string) *sandbox {
	t.Helper()
	skipWithoutCompanyMode(t)
	s := newSandboxWith(t, sandboxOpts{ldflags: companyFlags(version, "", ""), homeName: "fakeuser"})
	dir := companyFolder(s.root)
	mustMkdir(t, dir)
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeCompanyFile(t, s, "company.json", company, 0o644)
	writeCompanyFile(t, s, "binaries.sha256", listLine(t, s.binary, version), 0o644)
	return s
}

func writeCompanyFile(t *testing.T, s *sandbox, name, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(companyFolder(s.root), name)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// listLine is one line of binaries.sha256 for the binary at path.
func listLine(t *testing.T, path, version string) string {
	return fileSHA(t, path) + "  " + version + "\n"
}

// review runs bin as a scheduled review of the sandbox and returns its exit
// code.
func (s *sandbox) review(t *testing.T, bin string) int {
	t.Helper()
	cmd := exec.Command(bin, "run", "--unattended")
	cmd.Dir = s.work
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		t.Logf("review exit %d", exitErr.ExitCode())
		return exitErr.ExitCode()
	}
	t.Fatalf("review: %v\n%s", err, out)
	return -1
}

// initPerson runs `rosmarops init`: the person's own config.json.
func (s *sandbox) initPerson(t *testing.T) {
	t.Helper()
	cmd := exec.Command(s.binary, "init")
	cmd.Dir, cmd.Env = s.work, s.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func (s *sandbox) dataDir() string { return filepath.Join(report.DefaultReportDirIn(s.home), "data") }

func (s *sandbox) summaryPath() string { return filepath.Join(s.dataDir(), report.SummaryFileName) }

// summary reads the summary: one line, and its decoded fields.
func (s *sandbox) summary(t *testing.T) ([]byte, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(s.summaryPath())
	if err != nil {
		t.Fatalf("no summary: %v", err)
	}
	if bytes.Count(raw, []byte("\n")) != 1 || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("summary is not one line:\n%s", raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return raw, m
}

func (s *sandbox) noSummary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(s.summaryPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a summary exists: %v", err)
	}
}

// reportV2 reads report.v2.json.
func (s *sandbox) reportV2(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.dataDir(), "report.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (s *sandbox) stateIntegrity(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.home, ".rosmarops", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Integrity map[string]any `json:"integrity"`
		Engine    string         `json:"engine_version"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	st.Integrity["engine_version"] = st.Engine
	return st.Integrity
}

// validSummary checks raw against summary.v1 with the program's validator
// and with gojsonschema, the oracle.
func validSummary(t *testing.T, raw []byte) {
	t.Helper()
	if err := report.ValidateSummary(raw); err != nil {
		t.Errorf("own validator: %v", err)
	}
	schema, err := os.ReadFile(filepath.Join(repoRoot(t), "schemas", "summary.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gojsonschema.Validate(gojsonschema.NewBytesLoader(schema), gojsonschema.NewBytesLoader(raw))
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if !res.Valid() {
		t.Errorf("oracle: %v", res.Errors())
	}
}

// T1, and T11 for the exit code of an abort.
func TestSummaryValidAgainstSchema(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		s := newCompanySandbox(t, "0.16.0", companyJSON(true))
		if code := s.review(t, s.binary); code == 2 {
			t.Fatalf("first run with the binary in the list aborted")
		}
		raw, m := s.summary(t)
		validSummary(t, raw)
		checkSummaryInvariants(t, raw)
		if m["run"] != "complete" || m["config_source"] != "company" || m["binary"] != "first_run" {
			t.Errorf("summary: %s", raw)
		}
		if _, err := os.Stat(filepath.Join(s.home, ".rosmarops", "config.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the company review created config.json: %v", err)
		}
		s.review(t, s.binary)
		if _, m := s.summary(t); m["binary"] != "unchanged" {
			t.Errorf("second review: binary %v", m["binary"])
		}
	})
	t.Run("integrity abort", func(t *testing.T) {
		s := newCompanySandbox(t, "0.16.0", companyJSON(true))
		s.review(t, s.binary)
		other := buildBinary(t, companyFlags("0.16.1", "", "")(s.root))
		if code := s.review(t, other); code != 2 {
			t.Errorf("a changed binary not in the list: exit %d, want 2", code)
		}
		raw, m := s.summary(t)
		validSummary(t, raw)
		checkSummaryInvariants(t, raw)
		if m["run"] != "integrity_abort" || m["abort_code"] != "binary_hash_changed" || m["status"] != "red" {
			t.Errorf("summary: %s", raw)
		}
	})
	t.Run("run failed", func(t *testing.T) {
		s := newCompanySandbox(t, "0.16.0", companyJSON(true))
		// The person's own configuration, broken: the review cannot finish.
		mustMkdir(t, filepath.Join(s.home, ".rosmarops"))
		mustWrite(t, filepath.Join(s.home, ".rosmarops", "config.json"), "{not json")
		s.review(t, s.binary)
		raw, m := s.summary(t)
		validSummary(t, raw)
		checkSummaryInvariants(t, raw)
		if m["run"] != "failed" || m["abort_code"] != "run_failed" || m["config_source"] != "user" {
			t.Errorf("summary: %s", raw)
		}
	})
}

// reportRules is the rule enum of report.v2: the summary's rules come from it.
func reportRules(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "schemas", "v2", "report.v2.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(any) []any
	walk = func(n any) []any {
		switch v := n.(type) {
		case map[string]any:
			if r, ok := v["rule"].(map[string]any); ok {
				if e, ok := r["enum"].([]any); ok && len(e) > 3 {
					return e
				}
			}
			for _, c := range v {
				if e := walk(c); e != nil {
					return e
				}
			}
		case []any:
			for _, c := range v {
				if e := walk(c); e != nil {
					return e
				}
			}
		}
		return nil
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range walk(schema) {
		out[r.(string)] = true
	}
	if len(out) == 0 {
		t.Fatal("no rule enum in report.v2")
	}
	return out
}

// knownProducts is the program's closed list of products, as the summary
// writes them, plus the editor of an extension and "other".
var knownProducts = map[string]bool{
	"claude_code": true, "cline": true, "cursor": true, "github_copilot": true, "grok_bot": true, "codex": true,
	"grok_build": true, "antigravity": true, "antigravity_cli": true, "gemini_cli": true, "cline_cli": true,
	"copilot_jetbrains": true, "hermes": true, "claude_desktop": true, "n8n": true, "vscode": true, "other": true,
}

// checkSummaryInvariants is T2 on one summary: the rules summary.v1 cannot
// express.
func checkSummaryInvariants(t *testing.T, raw []byte) {
	t.Helper()
	var s report.CompanySummary
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if (s.AbortCode == nil) != (s.Run == report.SummaryComplete) {
		t.Errorf("abort_code %v with run %s", s.AbortCode, s.Run)
	}
	if s.Run == report.SummaryFailed && (s.AbortCode == nil || *s.AbortCode != report.SummaryRunFailed) {
		t.Errorf("a failed run's abort_code is %v", s.AbortCode)
	}
	if s.RedTotal > 0 && s.Status != "red" {
		t.Errorf("red_total %d with status %s", s.RedTotal, s.Status)
	}
	if s.Run != report.SummaryComplete {
		if s.Status != "red" || s.RulesVersion != nil || s.MachineBand != nil || s.MachineIndex != nil || len(s.ByRule) != 0 || len(s.Agents) != 0 {
			t.Errorf("a stopped review's summary: %s", raw)
		}
		return
	}
	red, yellow := 0, 0
	allowed := reportRules(t)
	for _, r := range s.ByRule {
		if !allowed[r.Rule] {
			t.Errorf("rule %q is not a report.v2 rule", r.Rule)
		}
		if r.Severity == "red" {
			red += r.Count
		} else {
			yellow += r.Count
		}
	}
	if red != s.RedTotal || yellow != s.YellowTotal {
		t.Errorf("totals %d/%d, by_rule %d/%d", s.RedTotal, s.YellowTotal, red, yellow)
	}
	seen := map[string]bool{}
	for _, a := range s.Agents {
		k := a.Type + "/" + a.Product
		if seen[k] {
			t.Errorf("agents not grouped: %s twice", k)
		}
		seen[k] = true
		if !knownProducts[a.Product] {
			t.Errorf("product %q is not from the program's list", a.Product)
		}
	}
	codes := map[string]bool{}
	for _, c := range report.NotCheckedCodes {
		codes[c] = true
	}
	for _, c := range s.NotChecked {
		if !codes[c] {
			t.Errorf("not_checked %q is not a NotCheckedCodes code", c)
		}
	}
}

// T2 over a review with agents and findings: the sandbox's agents (Claude
// Code with an MCP server, Cursor, Codex, ...) and a committed key.
func TestSummaryInvariants(t *testing.T) {
	s := newCompanySandbox(t, "0.16.0", companyJSON(true))
	s.review(t, s.binary)
	raw, _ := s.summary(t)
	checkSummaryInvariants(t, raw)
	var sum report.CompanySummary
	if err := json.Unmarshal(raw, &sum); err != nil {
		t.Fatal(err)
	}
	if len(sum.Agents) == 0 {
		t.Fatalf("no agents in the summary of a computer with agents: %s", raw)
	}
}

// T3: with fake secrets, an MCP server named acme-client-db, an extension and
// projects under /…/fakeuser, the summary holds no secret, no preview, no
// user, no path, no server name, no graph id and no graph fingerprint, and
// exactly the keys of summary.v1.
func TestNoSecretOrPathInSummary(t *testing.T) {
	s := newCompanySandbox(t, "0.16.0", companyJSON(true))
	claude := `{"mcpServers":{"acme-client-db":{"command":"node","args":["db.js"],"env":{"ANTHROPIC_API_KEY":"` + fakeAnthropicKey + `"}}},"projects":{` + strconv.Quote(s.repo) + `:{}}}`
	mustWrite(t, filepath.Join(s.home, ".claude.json"), claude)
	mustMkdir(t, filepath.Join(s.home, "source", "acme-app"))
	mustWrite(t, filepath.Join(s.home, "source", "acme-app", ".env"), "GITHUB_TOKEN="+fakeGitHubPAT+"\n")
	s.review(t, s.binary)
	raw, m := s.summary(t)
	text := string(raw)
	for what, v := range map[string]string{
		"the Anthropic key": fakeAnthropicKey, "the GitHub token": fakeGitHubPAT, "the Claude token": fakeClaudeOAuthToken,
		"the user": "fakeuser", "the user folder": s.home, "the server name": "acme",
	} {
		if strings.Contains(text, v) {
			t.Errorf("the summary holds %s", what)
		}
	}
	if regexp.MustCompile(`.{4} \.\.\. .{4}`).MatchString(text) {
		t.Errorf("the summary holds a preview: %s", text)
	}
	v2 := s.reportV2(t)
	graphRaw, err := os.ReadFile(filepath.Join(s.dataDir(), "graph.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(graphRaw, &g); err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if strings.Contains(text, n.ID) {
			t.Errorf("the summary holds the graph id %s", n.ID)
		}
	}
	if prov, ok := v2["provenance"].(map[string]any); ok {
		if sha, ok := prov["graph_sha256"].(string); ok && sha != "" && strings.Contains(text, sha) {
			t.Error("the summary holds graph_sha256")
		}
	}
	// Exactly the keys of the schema.
	want := []string{"agents", "abort_code", "binary", "by_rule", "config_source", "generated_at", "generated_unix", "machine_band", "machine_index", "not_checked", "red_total", "rules_version", "run", "schema_version", "status", "version", "yellow_total"}
	var got []string
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys %v", got)
	}
}

// T4: without a company configuration, or with the summary off, no summary is
// written, and a previous one is removed.
func TestSummaryOffByDefault(t *testing.T) {
	for name, company := range map[string]string{"no company configuration": "", "summary off": companyJSON(false)} {
		t.Run(name, func(t *testing.T) {
			s := newCompanySandbox(t, "0.16.0", companyJSON(true))
			if company == "" {
				if err := os.Remove(filepath.Join(companyFolder(s.root), "company.json")); err != nil {
					t.Fatal(err)
				}
				s.initPerson(t)
			} else {
				writeCompanyFile(t, s, "company.json", company, 0o644)
			}
			mustMkdir(t, s.dataDir())
			mustWrite(t, s.summaryPath(), `{"old":"green"}`+"\n")
			s.review(t, s.binary)
			s.noSummary(t)
		})
	}
}

// scanErrors are report.v2's coverage.scan_errors.
func scanErrors(t *testing.T, v2 map[string]any) []string {
	t.Helper()
	cov, _ := v2["coverage"].(map[string]any)
	var out []string
	for _, e := range cov["scan_errors"].([]any) {
		out = append(out, e.(string))
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// T5 (Linux; Windows ACLs in internal/company): a company configuration that
// anyone but an administrator could change is not used: the review says
// company_config_untrusted and writes no summary.
func TestCompanyConfigTrust(t *testing.T) {
	cases := map[string]func(t *testing.T, s *sandbox){
		"mode 0666": func(t *testing.T, s *sandbox) { writeCompanyFile(t, s, "company.json", companyJSON(true), 0o666) },
		"folder writable by group": func(t *testing.T, s *sandbox) {
			if err := os.Chmod(companyFolder(s.root), 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"parent writable by group": func(t *testing.T, s *sandbox) {
			if err := os.Chmod(filepath.Dir(companyFolder(s.root)), 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"a link": func(t *testing.T, s *sandbox) {
			p := filepath.Join(companyFolder(s.root), "company.json")
			real := filepath.Join(companyFolder(s.root), "real.json")
			if err := os.Rename(p, real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, p); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			s := newCompanySandbox(t, "0.16.0", companyJSON(true))
			s.initPerson(t)
			spoil(t, s)
			s.review(t, s.binary)
			if errs := scanErrors(t, s.reportV2(t)); !contains(errs, "company_config_untrusted") {
				t.Errorf("scan errors %v", errs)
			}
			if c, _ := s.reportV2(t)["company"].(map[string]any); c["state"] != "untrusted" {
				t.Errorf("company %v", c)
			}
			s.noSummary(t)
		})
	}
	t.Run("owned by a normal user", func(t *testing.T) {
		skipWithoutCompanyMode(t)
		s := newSandboxWith(t, sandboxOpts{ldflags: companyFlags("0.16.0", strconv.Itoa(os.Getuid()+1), ""), homeName: "fakeuser"})
		mustMkdir(t, companyFolder(s.root))
		for _, d := range []string{filepath.Dir(companyFolder(s.root)), companyFolder(s.root)} {
			if err := os.Chmod(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeCompanyFile(t, s, "company.json", companyJSON(true), 0o644)
		s.initPerson(t)
		s.review(t, s.binary)
		if errs := scanErrors(t, s.reportV2(t)); !contains(errs, "company_config_untrusted") {
			t.Errorf("scan errors %v", errs)
		}
		s.noSummary(t)
	})
}

// findingIDs are the ids of report.v2's findings.
func findingIDs(t *testing.T, v2 map[string]any) []string {
	t.Helper()
	var out []string
	for _, f := range v2["findings"].([]any) {
		out = append(out, f.(map[string]any)["id"].(string))
	}
	sort.Strings(out)
	return out
}

// T6: a company configuration that tries to hide findings, add secret
// patterns, move the report or reach the space is invalid: the review goes on
// with the person's configuration, says company_config_invalid, and hides no
// finding.
func TestCompanyConfigCannotHide(t *testing.T) {
	base := newCompanySandbox(t, "0.16.0", companyJSON(true))
	if err := os.Remove(filepath.Join(companyFolder(base.root), "company.json")); err != nil {
		t.Fatal(err)
	}
	base.initPerson(t)
	base.review(t, base.binary)
	want := findingIDs(t, base.reportV2(t))
	if len(want) == 0 {
		t.Fatal("the base review has no finding to hide")
	}
	for _, extra := range []string{
		`"allow_list":{"dismissed":[]}`, `"dismissed":[]`, `"extra_secret_patterns":[]`, `"report_directory":"/tmp/elsewhere"`,
		`"space":{"permission":true}`, `"problems":{"send":true}`,
	} {
		t.Run(extra, func(t *testing.T) {
			s := newCompanySandbox(t, "0.16.0", `{"schema_version":1,"summary":{"enabled":true},`+extra+`}`)
			s.initPerson(t)
			s.review(t, s.binary)
			v2 := s.reportV2(t)
			if errs := scanErrors(t, v2); !contains(errs, "company_config_invalid") {
				t.Errorf("scan errors %v", errs)
			}
			if v2["status"] == "green" {
				t.Error("an ignored company configuration left the review green")
			}
			if got := findingIDs(t, v2); len(got) != len(want) {
				t.Errorf("findings %d, without the company configuration %d", len(got), len(want))
			}
			s.noSummary(t)
		})
	}
}

// T7 (and T11): the administrator's fingerprint list. It accepts a newer
// binary listed with its version; it never accepts an untrusted list, a
// binary not listed, an older version, the same version with another
// fingerprint, or a list with a malformed line; and with the company
// configuration and no state, only a listed binary is a first run.
func TestAdminListIntegrity(t *testing.T) {
	s := newCompanySandbox(t, "0.15.0", companyJSON(true))
	if code := s.review(t, s.binary); code == 2 {
		t.Fatal("first run with the listed binary aborted")
	}
	statePath := filepath.Join(s.home, ".rosmarops", "state.json")
	base, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if err := os.WriteFile(statePath, base, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	newer := buildBinary(t, companyFlags("0.16.0", "", "")(s.root))
	older := buildBinary(t, companyFlags("0.14.0", "", "")(s.root))
	// The same version with another fingerprint: the stop folder spelled with
	// a trailing slash changes the binary, not what it does.
	twin := buildBinary(t, companyFlags("0.15.0", "", "/")(s.root))
	if fileSHA(t, twin) == fileSHA(t, s.binary) {
		t.Fatal("the twin build has the same fingerprint")
	}
	list := func(lines ...string) { writeCompanyFile(t, s, "binaries.sha256", strings.Join(lines, ""), 0o644) }

	t.Run("accepted: newer and listed", func(t *testing.T) {
		restore()
		list(listLine(t, s.binary, "0.15.0"), listLine(t, newer, "0.16.0"))
		if code := s.review(t, newer); code == 2 {
			t.Fatal("aborted")
		}
		if in := s.stateIntegrity(t); in["binary_accepted_via"] != "admin_list" || in["engine_version"] != "0.16.0" {
			t.Errorf("state %v", in)
		}
		if _, m := s.summary(t); m["binary"] != "admin_list" {
			t.Errorf("summary binary %v", m["binary"])
		}
		if c, _ := s.reportV2(t)["company"].(map[string]any); c["binary_accepted_via"] != "admin_list" {
			t.Errorf("report company %v", c)
		}
	})
	aborts := map[string]struct {
		bin   string
		setup func()
	}{
		"the list is not trusted": {newer, func() {
			writeCompanyFile(t, s, "binaries.sha256", listLine(t, newer, "0.16.0"), 0o666)
		}},
		"the fingerprint is not listed":         {newer, func() { list(listLine(t, s.binary, "0.15.0")) }},
		"an older version":                      {older, func() { list(listLine(t, older, "0.14.0")) }},
		"the same version, another fingerprint": {twin, func() { list(listLine(t, twin, "0.15.0")) }},
		"a malformed line":                      {newer, func() { list(listLine(t, newer, "0.16.0"), "not a line\n") }},
		"listed with another version":           {newer, func() { list(listLine(t, newer, "0.16.1")) }},
	}
	for name, c := range aborts {
		t.Run("aborts: "+name, func(t *testing.T) {
			restore()
			c.setup()
			if code := s.review(t, c.bin); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			_, m := s.summary(t)
			if m["run"] != "integrity_abort" || m["abort_code"] != "binary_hash_changed" {
				t.Errorf("summary %v", m)
			}
		})
	}
	t.Run("no state: not listed is a lost state", func(t *testing.T) {
		if err := os.Remove(statePath); err != nil {
			t.Fatal(err)
		}
		list(listLine(t, s.binary, "0.15.0"))
		if code := s.review(t, newer); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if _, m := s.summary(t); m["abort_code"] != "state_lost" {
			t.Errorf("summary %v", m)
		}
	})
	t.Run("no state: listed is a first run", func(t *testing.T) {
		_ = os.Remove(statePath)
		list(listLine(t, newer, "0.16.0"))
		if code := s.review(t, newer); code == 2 {
			t.Fatal("aborted")
		}
		if _, m := s.summary(t); m["binary"] != "first_run" {
			t.Errorf("summary %v", m)
		}
	})
}

// T11: a changed binary without the list exits with the integrity code, 2,
// with the summary on as with it off, and the summary says the review stopped.
func TestSummaryAfterAbortExitCode(t *testing.T) {
	for _, on := range []bool{true, false} {
		t.Run(fmt.Sprintf("summary %v", on), func(t *testing.T) {
			s := newCompanySandbox(t, "0.16.0", companyJSON(on))
			s.review(t, s.binary)
			other := buildBinary(t, companyFlags("0.16.1", "", "")(s.root))
			if code := s.review(t, other); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if !on {
				s.noSummary(t)
				return
			}
			_, m := s.summary(t)
			if m["run"] != "integrity_abort" || m["status"] != "red" {
				t.Errorf("summary %v", m)
			}
		})
	}
}

// T9: with the company folder seeded, a managed review writes nothing outside
// ~/.rosmarops and the reports folder, changes nothing in the company folder
// (contents, permissions, new files), and the summary is only in the reports
// data folder.
func TestNoWritesOutOfScopeCompany(t *testing.T) {
	s := newCompanySandbox(t, "0.16.0", companyJSON(true))
	allowed := filepath.Join(s.home, ".rosmarops")
	reports := report.DefaultReportDirIn(s.home)
	before := snapshot(t, s.root)
	s.review(t, s.binary)
	s.review(t, s.binary)
	after := snapshot(t, s.root)
	for _, change := range changedPaths(before, after) {
		_, path, _ := strings.Cut(change, ": ")
		if !pathUnder(path, allowed) && !pathUnder(path, reports) {
			t.Errorf("changed outside %s and %s: %s", allowed, reports, change)
		}
	}
	err := filepath.WalkDir(s.home, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == report.SummaryFileName && filepath.Dir(p) != s.dataDir() {
			t.Errorf("a summary outside the reports data folder: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s.summary(t)
}
