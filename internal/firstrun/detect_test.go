package firstrun

import (
	"encoding/base64"
	"encoding/json"
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

// CTO-38: the first use's Detect is the project search: a repository named in
// ~/.claude.json outside the user folder's walk is accepted once, and two
// paths equal but for case are one on a case-insensitive system only.
func TestDetectIndicatorOnce(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir() // outside the user folder: only the indicator finds it
	a := filepath.Join(other, "App")
	if err := os.MkdirAll(filepath.Join(a, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	ka, _ := json.Marshal(a) // a Windows path has backslashes to escape
	raw := `{"projects": {` + string(ka) + `: {}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	res := Detect(Env{Home: home})
	if len(res.Repos) != 1 || res.Repos[0] != a || res.Decisions[0].Source != "claude" {
		t.Fatalf("repos %v decisions %+v", res.Repos, res.Decisions)
	}
}

// A ~/.claude.json that cannot be parsed, or one over the size limit, is
// skipped, never a crash.
func TestDetectUnreadableSource(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := Detect(Env{Home: home}); len(res.Repos) != 0 {
		t.Errorf("%+v", res)
	}
	big := strings.Repeat(" ", maxJSONBytes+1)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := Detect(Env{Home: home}); len(res.Repos) != 0 {
		t.Errorf("oversize: %+v", res)
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
