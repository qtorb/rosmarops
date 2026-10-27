package firstrun

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// U3 b): only a local file:// URI is a folder; remote, WSL and UNC are not.
func TestLocalPath(t *testing.T) {
	for uri, want := range map[string]string{
		"file:///c%3A/Users/ana/work/app":                 filepath.FromSlash("c:/Users/ana/work/app"),
		"file:///C:/Users/ana/Mis%20proyectos/d%C3%A9lta": filepath.FromSlash("C:/Users/ana/Mis proyectos/délta"),
		"file:///home/ana/x":                              filepath.FromSlash("/home/ana/x"),
	} {
		if got, ok := LocalPath(uri); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", uri, got, ok, want)
		}
	}
	for _, uri := range []string{
		"vscode-remote://wsl%2Bubuntu/home/ana/x", "vscode-remote://ssh-remote%2Bbox/srv/x",
		"file://server/share/x", "file://wsl.localhost/Ubuntu/home/x", "file://wsl$/Ubuntu/home/x",
		"file:////server/share/x", "file:///c%3A/bad%2", "file:///c%3A/bad%zz", "C:\\Users\\ana\\x", "",
	} {
		if got, ok := LocalPath(uri); ok {
			t.Errorf("%s: local %q", uri, got)
		}
	}
}

// U3: duplicates are dropped without regard to case, keeping the first
// source's.
func TestDetectDuplicateIgnoresCase(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, "work", "App")
	b := filepath.Join(home, "work", "app")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	raw := `{"projects": {"` + a + `": {}, "` + b + `": {}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	res := Detect(Env{Home: home})
	if len(res.Repos) != 1 || res.Repos[0] != a {
		t.Fatalf("repos %v", res.Repos)
	}
	if d := res.Decisions[1]; d.Path != b || d.Rule != RuleDuplicate {
		t.Errorf("decisions %+v", res.Decisions)
	}
}

// A ~/.claude.json that cannot be parsed, or one over the size limit, is
// a source not read, never a crash.
func TestDetectUnreadableSource(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := Detect(Env{Home: home})
	if len(res.Repos) != 0 || len(res.Decisions) != 1 || res.Decisions[0].Rule != RuleUnreadableSrc {
		t.Errorf("%+v", res)
	}
	big := strings.Repeat(" ", maxJSONBytes+1)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := Detect(Env{Home: home}); len(res.Decisions) != 1 || res.Decisions[0].Rule != RuleUnreadableSrc {
		t.Errorf("oversize: %+v", res)
	}
}

// U4: the chosen folder itself when it has .git, then one level under it.
func TestPicked(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{dir, filepath.Join(dir, "a"), filepath.Join(dir, "b")} {
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "c"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := Picked(dir)
	if want := []string{dir, filepath.Join(dir, "a"), filepath.Join(dir, "b")}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%q, want %q", got, want)
	}
}

// U4: the picker's script is constant; the title and the folder travel as
// base64 of UTF-8, whatever they hold.
func TestPickerDataStaysData(t *testing.T) {
	args := PickerArgs()
	if len(args) != 5 || args[4] != PickerScript || args[2] != "-STA" {
		t.Fatalf("args %q", args)
	}
	for _, s := range []string{`Elige la carpeta donde guardas tus proyectos`, `O'Brien "x" $(Remove-Item C:\) ñ`, "line\nbreak"} {
		in := pickerInput(s)
		if strings.ContainsAny(in, "'\"$()\n ") {
			t.Errorf("%q: input %q is not plain base64", s, in)
		}
		out, err := pickerOutput(base64.StdEncoding.EncodeToString([]byte(s)) + "\r\n")
		if err != nil || out != s {
			t.Errorf("%q: round trip %q %v", s, out, err)
		}
		if strings.Contains(PickerScript, s) {
			t.Errorf("%q in the script", s)
		}
	}
	if out, err := pickerOutput(""); out != "" || err != nil {
		t.Errorf("cancelled: %q %v", out, err)
	}
	if _, err := pickerOutput("not base64!"); err == nil {
		t.Error("bad output accepted")
	}
}
