package trust_test

// Trust test (CLAUDE.md invariant 1, BV-137 / CTO-13 §3, option B — one
// program): the network lives only in internal/spacenet, only with the
// person's permission.
//
// Static part, never skipped: of every package in the module, only
// internal/spacenet imports net, any net/*, or crypto/tls — directly or in its
// own test files. The single binary does link those packages through
// internal/spacenet, so option B drops the former "no net in the binary graph
// or symbols" checks; the direct-import check and the runtime check below are
// what prove the invariant now. No module code refers to a raw socket call of
// syscall or golang.org/x/sys (net opens its own sockets; our code only ever
// goes through net).
//
// Runtime part (Linux, needs strace): init, baseline and run in the sandbox of
// no_writes_out_of_scope_test.go, with every child process traced; without the
// permission in the state (the sandbox records none) any AF_INET or AF_INET6
// connect/sendto/sendmsg fails the test. It runs on Linux only: on Windows and
// on macOS (no strace; decision 2026-10-28, macOS, M1a) only the static part
// runs, declared as a limit.

import (
	"bufio"
	cryptorand "crypto/rand"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/kit"
)

// spacenetPkg is the one package allowed to import the network (invariant 1).
const spacenetPkg = "github.com/qtorb/rosmarops/internal/spacenet"

// staticTargets are the systems the static checks run for (CTO-14 P1): Windows
// is the main target, so a windows-only file that imports the network must not
// slip past a host-only (linux) check. go list reads imports per GOOS/GOARCH
// without compiling.
var staticTargets = []struct{ goos, goarch string }{
	{"windows", "amd64"},
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"linux", "amd64"},
}

// isNet reports a package the invariant confines to internal/spacenet: net,
// any net/* (net/http, net/url, net/netip, …), and crypto/tls.
func isNet(path string) bool {
	return path == "net" || strings.HasPrefix(path, "net/") || path == "crypto/tls"
}

// socketCalls are the socket functions module code must not refer to, by
// import path, whatever name it imports them under.
var socketCalls = map[string]map[string]bool{
	"syscall":                  {"Socket": true, "Connect": true, "Sendto": true, "Sendmsg": true, "SendmsgN": true, "WSASocket": true, "WSAConnect": true, "WSASendto": true},
	"golang.org/x/sys/unix":    {"Socket": true, "Connect": true, "Sendto": true, "Sendmsg": true, "SendmsgN": true},
	"golang.org/x/sys/windows": {"Socket": true, "Connect": true, "Sendto": true, "WSASocket": true, "WSAConnect": true, "WSASendto": true, "WSASendTo": true},
}

// TestNoNetwork_OnlySpacenetImportsNetwork is the option-B static proof, run
// for every static target (CTO-14 P1): over every package of the module (its
// imports and its test imports), only internal/spacenet imports net, net/* or
// crypto/tls.
func TestNoNetwork_OnlySpacenetImportsNetwork(t *testing.T) {
	for _, tgt := range staticTargets {
		t.Run(tgt.goos+"/"+tgt.goarch, func(t *testing.T) {
			out := goListFor(t, tgt.goos, tgt.goarch, "-f", `{{.ImportPath}}|{{join .Imports ","}},{{join .TestImports ","}},{{join .XTestImports ","}}`, "./...")
			sawSpacenet := false
			for _, line := range out {
				pkg, imports, _ := strings.Cut(line, "|")
				if pkg == spacenetPkg {
					sawSpacenet = true
					continue // the one package allowed network imports
				}
				for _, imp := range strings.Split(imports, ",") {
					if isNet(imp) {
						t.Errorf("package %s imports network package %s (only %s may)", pkg, imp, spacenetPkg)
					}
				}
			}
			if !sawSpacenet {
				t.Fatalf("internal/spacenet not found in go list; the allowance check is miscalibrated")
			}
		})
	}
}

// TestNoNetwork_NetworkPackagesOnlyViaSpacenet is the recovered dependency-graph
// proof (CTO-14 P2): for every static target, any package in the binary's full
// dependency closure (go list -deps ./cmd/rosmarops) that imports net, net/* or
// crypto/tls must also be in internal/spacenet's closure. So the network can
// enter the binary only through spacenet — directly (P1) or through an indirect
// dependency, standard (log/syslog) or external, that would otherwise go
// unseen.
func TestNoNetwork_NetworkPackagesOnlyViaSpacenet(t *testing.T) {
	for _, tgt := range staticTargets {
		t.Run(tgt.goos+"/"+tgt.goarch, func(t *testing.T) {
			spacenetClosure := map[string]bool{}
			for _, line := range goListFor(t, tgt.goos, tgt.goarch, "-deps", "-f", `{{.ImportPath}}`, "./internal/spacenet") {
				spacenetClosure[line] = true
			}
			if !spacenetClosure[spacenetPkg] {
				t.Fatalf("internal/spacenet not in its own -deps closure; the check is miscalibrated")
			}
			for _, line := range goListFor(t, tgt.goos, tgt.goarch, "-deps", "-f", `{{.ImportPath}}|{{join .Imports ","}}`, "./cmd/rosmarops") {
				pkg, imports, _ := strings.Cut(line, "|")
				importsNet := false
				for _, imp := range strings.Split(imports, ",") {
					if isNet(imp) {
						importsNet = true
						break
					}
				}
				if importsNet && !spacenetClosure[pkg] {
					t.Errorf("package %s is in the binary and imports the network but is not in internal/spacenet's closure (the network must enter only through %s)", pkg, spacenetPkg)
				}
			}
		})
	}
}

// TestNoNetwork_NoDirectSocketCalls: no code of the module refers to a socket
// call of syscall or golang.org/x/sys directly (internal/spacenet reaches the
// network only through net, which opens its own sockets).
func TestNoNetwork_NoDirectSocketCalls(t *testing.T) {
	root := repoRoot(t)
	files := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		files++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		names := map[string]string{} // local name → import path
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if socketCalls[p] == nil {
				continue
			}
			name := p[strings.LastIndex(p, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "." {
				t.Errorf("%s dot-imports %s", path, p)
			}
			names[name] = p
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && socketCalls[names[id.Name]][sel.Sel.Name] {
				t.Errorf("%s refers to %s.%s", path, names[id.Name], sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Fatalf("walked %d Go files", files)
	}
}

// The helpers catch what they are meant to.
func TestNoNetwork_ChecksCatch(t *testing.T) {
	for _, p := range []string{"net", "net/http", "net/http/httptest", "net/url", "net/netip", "crypto/tls"} {
		if !isNet(p) {
			t.Errorf("isNet(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"crypto/rand", "os", "internal/spacenet", "encoding/json", "nettest"} {
		if isNet(p) {
			t.Errorf("isNet(%q) = true, want false", p)
		}
	}
}

func TestNoNetwork_RuntimeNoInetSockets(t *testing.T) {
	if runtime.GOOS != "linux" {
		// Declared limit (decision 2026-10-28, macOS, M1a): on Windows and
		// macOS only the static checks above run.
		t.Skip("runtime network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available: runtime network check not executed")
	}
	s := newSandbox(t)
	traceDir := t.TempDir()
	traces := map[string]string{}
	s.runAll(t, func(step string, argv []string) []string {
		out := filepath.Join(traceDir, step+".strace")
		traces[step] = out
		// -Y prints the command name next to each pid.
		return append([]string{strace, "-f", "-qq", "-Y", "-e", "trace=connect,sendto,sendmsg", "-o", out}, argv...)
	})

	steps := make([]string, 0, len(traces))
	for step := range traces {
		steps = append(steps, step)
	}
	sort.Strings(steps)
	for _, step := range steps {
		f, err := os.Open(traces[step])
		if err != nil {
			t.Fatalf("open trace for %s: %v", step, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.Contains(line, "AF_INET"): // also matches AF_INET6
				t.Errorf("rosmarops %s: inet socket call: %s", step, line)
			case strings.Contains(line, "AF_UNIX"):
				t.Logf("rosmarops %s: unix socket call: %s", step, line)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("read trace for %s: %v", step, err)
		}
		_ = f.Close()
	}
}

// TestNoNetwork_RuntimeNoInetSockets_ConnectedKit is the CTO-14 P3 strace
// variant: with a kit and a send credential present on this computer but the
// permission in the state set to false, the run still opens no inet socket.
// spacenet refuses without a Permission from a state whose SpacePermission is
// true, so no caller — now or once sending is wired — can reach the network
// while the person has not approved it. It runs on Linux with strace only.
func TestNoNetwork_RuntimeNoInetSockets_ConnectedKit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available: runtime network check not executed")
	}
	s := newSandbox(t)
	// A full first run creates the config and state; the permission stays
	// false (DefaultState never sets it).
	s.runAll(t, nil)

	// Seed a kit with a 32-byte send credential, as a connected computer
	// would hold it — but leave the permission false.
	salt := make([]byte, kit.SaltSize)
	cryptoRead(t, salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kitID, err := kit.NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	cred := make([]byte, 32)
	cryptoRead(t, cred)
	storePath := filepath.Join(s.home, ".rosmarops", "kit.key")
	if err := kit.Save(storePath, kit.Stored{KitID: kitID, Secret: sec, SpaceCredential: cred}); err != nil {
		t.Fatalf("seed kit with credential: %v", err)
	}
	assertStatePermissionFalse(t, filepath.Join(s.home, ".rosmarops", "state.json"))

	// Run again, now with the credential present, under strace.
	traceDir := t.TempDir()
	traces := map[string]string{}
	for _, step := range []string{"run", "unattended"} {
		out := filepath.Join(traceDir, step+".strace")
		args := []string{"run"}
		if step == "unattended" {
			args = []string{"run", "--unattended"}
		}
		argv := append([]string{strace, "-f", "-qq", "-Y", "-e", "trace=connect,sendto,sendmsg", "-o", out}, append([]string{s.binary}, args...)...)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = s.work
		cmd.Env = s.env
		_, _ = cmd.CombinedOutput() // a yellow/red run exits non-zero; the trace is what matters
		traces[step] = out
	}
	assertNoInet(t, traces)
}

// assertStatePermissionFalse fails if the state file records space_permission
// true (it must be false/absent for the P3 scenario to be meaningful).
func assertStatePermissionFalse(t *testing.T, statePath string) {
	t.Helper()
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.Contains(string(raw), `"space_permission": true`) {
		t.Fatalf("state already has space_permission true; the P3 scenario needs it false:\n%s", raw)
	}
}

// assertNoInet fails on any AF_INET/AF_INET6 socket call in the traces, and
// logs AF_UNIX ones (as the main runtime test does).
func assertNoInet(t *testing.T, traces map[string]string) {
	t.Helper()
	steps := make([]string, 0, len(traces))
	for step := range traces {
		steps = append(steps, step)
	}
	sort.Strings(steps)
	for _, step := range steps {
		f, err := os.Open(traces[step])
		if err != nil {
			t.Fatalf("open trace for %s: %v", step, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.Contains(line, "AF_INET"):
				t.Errorf("rosmarops %s (connected kit, no permission): inet socket call: %s", step, line)
			case strings.Contains(line, "AF_UNIX"):
				t.Logf("rosmarops %s: unix socket call: %s", step, line)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("read trace for %s: %v", step, err)
		}
		_ = f.Close()
	}
}

// cryptoRead fills b with random bytes or fails the test.
func cryptoRead(t *testing.T, b []byte) {
	t.Helper()
	if _, err := cryptorand.Read(b); err != nil {
		t.Fatal(err)
	}
}

// goListFor runs `go list` at the repository root for the given GOOS/GOARCH and
// returns its output lines.
func goListFor(t *testing.T, goos, goarch string, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("go list (GOOS=%s GOARCH=%s) %v: %v\n%s", goos, goarch, args, err, ee.Stderr)
		}
		t.Fatalf("go list (GOOS=%s GOARCH=%s) %v: %v", goos, goarch, args, err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
