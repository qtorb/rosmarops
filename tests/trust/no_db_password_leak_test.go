package trust_test

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A Supabase database URL is recognised by its host and user to group it
// with the project's other keys; its password is never read into any output.
// The real binary reviews a home with Albert's case (a service_role JWT and
// a database URL with a password in each project's .env, an sb_secret key
// committed in three projects, Claude Code that can change other agents'
// settings); no file it writes carries the password, and the report groups
// the database URL with the project.
func TestNoDatabasePasswordInAnyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sandboxed black-box run is implemented for Unix-like hosts")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBin(t, "./cmd/rosmarops", nil)
	home := t.TempDir()
	const (
		ref  = "abcdefghijklmnopqrst"
		pass = "Zq9PlantedDBPassword77"
	)
	// A fake sb_secret key, built so the source holds no literal key.
	sbs := "sb_" + "secret_" + "PlantedKey" + "0123456789ab" + "_" + "EFGH1234"
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	jwt := b64(`{"alg":"HS256","typ":"JWT"}`) + "." + b64(`{"iss":"supabase","ref":"`+ref+`","role":"service_role","iat":1700000000}`) + ".c2lnbmF0dXJlZmlsbGVyMDE"
	write := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude", "settings.json"), `{"permissions":{"defaultMode":"bypassPermissions"}}`)
	write(filepath.Join(home, ".cursor", "mcp.json"), `{}`)
	write(filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5\"\n")
	for _, p := range []string{"app1", "app2", "app3"} {
		dir := filepath.Join(home, "code", p)
		write(filepath.Join(dir, ".env"), "SUPABASE_SERVICE_ROLE_KEY="+jwt+"\nDATABASE_URL=postgresql://postgres:"+pass+"@db."+ref+".supabase.co:5432/postgres\n")
		write(filepath.Join(dir, "keys.js"), `const k = "`+sbs+`";`+"\n")
		for _, args := range [][]string{{"init", "-q"}, {"add", "keys.js"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"}} {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
	}
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	for _, args := range [][]string{{"init", "--repos-under", filepath.Join(home, "code")}, {"run", "--unattended", "--lang", "en"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = home, env
		_, _ = cmd.CombinedOutput()
	}
	// Every file the review wrote, wherever this system puts its reports.
	reports := ""
	files := 0
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p == filepath.Join(home, "code") {
			return filepath.SkipDir // the planted project itself
		}
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		files++
		if d.Name() == "report.en.html" {
			reports = filepath.Dir(p)
		}
		for _, leak := range []string{pass, "PlantedDB"} {
			if strings.Contains(string(raw), leak) {
				t.Errorf("%s carries the database password", p)
			}
		}
		return nil
	})
	if files < 5 {
		t.Fatalf("only %d files written: the review did not run", files)
	}
	page, err := os.ReadFile(filepath.Join(reports, "report.en.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "Your Supabase project is within reach of") {
		t.Error("the report does not group the project's keys")
	}
	// The database URL's credential is the project's: Supabase, by its host.
	var g struct {
		Nodes []struct{ Kind, Type, Service string } `json:"nodes"`
	}
	raw, err := os.ReadFile(filepath.Join(reports, "data", "graph.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	db := false
	for _, n := range g.Nodes {
		if n.Kind == "credential" && n.Type == "db_credential" {
			db = true
			if n.Service != "supabase" {
				t.Errorf("the database URL's credential is %q, not Supabase", n.Service)
			}
		}
	}
	if !db {
		t.Error("no database credential in the graph")
	}
}
