package tomlsubset

import (
	"reflect"
	"testing"
)

func TestParse_CodexApproveAll(t *testing.T) {
	m, err := Parse([]byte("approval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m["approval_policy"] != "never" || m["sandbox_mode"] != "danger-full-access" {
		t.Errorf("got %#v", m)
	}
}

func TestParse_QuotedTableWithBackslashes(t *testing.T) {
	// Codex case A: [projects.'C:\Users\a\repo'] trust_level = "trusted".
	src := "[projects.'C:\\Users\\a\\repo']\ntrust_level = \"trusted\"\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	projects, ok := m["projects"].(Map)
	if !ok {
		t.Fatalf("projects not a table: %#v", m)
	}
	repo, ok := projects[`C:\Users\a\repo`].(Map)
	if !ok {
		t.Fatalf("repo key not found (literal string keeps backslashes): %#v", projects)
	}
	if repo["trust_level"] != "trusted" {
		t.Errorf("trust_level = %#v", repo["trust_level"])
	}
}

func TestParse_BasicStringKeyUnescapes(t *testing.T) {
	// Grok trusted_folders.toml: [folders."C:\\src\\app"] trusted = true.
	src := "[folders.\"C:\\\\src\\\\app\"]\ntrusted = true\ndecided_at = 1730000000\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	folders := m["folders"].(Map)
	app, ok := folders[`C:\src\app`].(Map)
	if !ok {
		t.Fatalf("basic-string key not unescaped: %#v", folders)
	}
	if app["trusted"] != true {
		t.Errorf("trusted = %#v", app["trusted"])
	}
	if app["decided_at"] != int64(1730000000) {
		t.Errorf("decided_at = %#v", app["decided_at"])
	}
}

func TestParse_McpInlineTableAndArray(t *testing.T) {
	// Codex case D: mcp_servers with args array and env inline table.
	src := `[mcp_servers.github]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = { GITHUB_PERSONAL_ACCESS_TOKEN = "ghp_FAKEFAKEFAKEFAKE1234" }
`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	gh := m["mcp_servers"].(Map)["github"].(Map)
	if gh["command"] != "npx" {
		t.Errorf("command = %#v", gh["command"])
	}
	args, ok := gh["args"].(Array)
	if !ok || len(args) != 2 || args[0] != "-y" || args[1] != "@modelcontextprotocol/server-github" {
		t.Errorf("args = %#v", gh["args"])
	}
	env, ok := gh["env"].(Map)
	if !ok || env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp_FAKEFAKEFAKEFAKE1234" {
		t.Errorf("env = %#v", gh["env"])
	}
}

func TestParse_DottedKeyAndTopLevelArray(t *testing.T) {
	src := "project_doc_fallback_filenames = [\"CONTEXT.md\", \"NOTES.md\"]\nshell_environment_policy.ignore_default_excludes = true\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	arr := m["project_doc_fallback_filenames"].(Array)
	if !reflect.DeepEqual(arr, Array{"CONTEXT.md", "NOTES.md"}) {
		t.Errorf("array = %#v", arr)
	}
	pol := m["shell_environment_policy"].(Map)
	if pol["ignore_default_excludes"] != true {
		t.Errorf("dotted key = %#v", pol)
	}
}

func TestParse_WindowsSandboxTableAndComments(t *testing.T) {
	src := "# a comment\n[windows]\nsandbox = \"mxc\"  # inline comment, not part of value\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m["windows"].(Map)["sandbox"] != "mxc" {
		t.Errorf("sandbox = %#v", m["windows"])
	}
}

func TestParse_HashInsideStringKept(t *testing.T) {
	m, err := Parse([]byte(`token = "ab#cd"` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m["token"] != "ab#cd" {
		t.Errorf("token = %#v", m["token"])
	}
}

// BV-193: arrays of tables ([[a.b.c]]) for Codex's computer_use.windows.exes.
func TestParse_ArrayOfTables(t *testing.T) {
	m, err := Parse([]byte("[computer_use.windows]\n[[computer_use.windows.exes]]\nbinary_name = \"mspaint.exe\"\naccess = \"allow\"\n[[computer_use.windows.exes]]\nbinary_name = \"notepad.exe\"\naccess = \"deny\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exes := m["computer_use"].(Map)["windows"].(Map)["exes"].(Array)
	if len(exes) != 2 || exes[0].(Map)["binary_name"] != "mspaint.exe" || exes[1].(Map)["access"] != "deny" {
		t.Errorf("exes: %#v", exes)
	}
}

func TestParse_Refusals(t *testing.T) {
	cases := map[string]string{
		"array of tables over a table": "[t]\n[[t]]\n", // a table then an array of tables at the same path
		"duplicate key":                "a = 1\na = 2\n",
		"duplicate table":              "[t]\n[t]\n",
		"leaf then table":              "a = 1\n[a]\n",
		"multiline basic":              "a = \"\"\"x\n",
		"unterminated string":          "a = \"x\n",
		"unterminated array":           "a = [1, 2\n",
		"bad value":                    "a = 2026-01-01\n",
		"empty header":                 "[]\n",
		"no equals":                    "just some words\n",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error, got none", name)
		}
	}
}

func TestParse_FloatAndNegativeInt(t *testing.T) {
	m, err := Parse([]byte("timeout = 1.5\nretries = -3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m["timeout"] != 1.5 || m["retries"] != int64(-3) {
		t.Errorf("got %#v", m)
	}
}

func TestParse_OverMaxBytes(t *testing.T) {
	big := make([]byte, MaxBytes+1)
	for i := range big {
		big[i] = ' '
	}
	if _, err := Parse(big); err == nil {
		t.Error("expected size error")
	}
}

// BV-237: Grok Build's [permission] rules as its documentation writes them,
// an array of inline tables over several lines, with comments and a trailing
// comma.
func TestParse_MultiLineArrayOfInlineTables(t *testing.T) {
	m, err := Parse([]byte("[permission]\nrules = [\n  { action = \"deny\", tool = \"edit\" }, # no edits\n  { action = \"allow\", tool = \"bash\", pattern = \"git [s]tatus\" },\n]\n[ui]\npermission_mode = \"ask\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	rules := m["permission"].(Map)["rules"].(Array)
	if len(rules) != 2 || rules[1].(Map)["pattern"] != "git [s]tatus" || rules[0].(Map)["tool"] != "edit" {
		t.Errorf("rules = %v", rules)
	}
	if m["ui"].(Map)["permission_mode"] != "ask" {
		t.Errorf("ui = %v", m["ui"])
	}
	if _, err := Parse([]byte("a = [\n 1,\n")); err == nil {
		t.Error("an unterminated multi-line array parsed")
	}
}
