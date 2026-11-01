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
)

// spacenetPkg is the one package allowed to import the network (invariant 1).
const spacenetPkg = "github.com/qtorb/rosmarops/internal/spacenet"

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

// TestNoNetwork_OnlySpacenetImportsNetwork is the option-B static proof: over
// every package of the module (its imports and its test imports), only
// internal/spacenet imports net, net/* or crypto/tls.
func TestNoNetwork_OnlySpacenetImportsNetwork(t *testing.T) {
	out := goList(t, "-f", `{{.ImportPath}}|{{join .Imports ","}},{{join .TestImports ","}},{{join .XTestImports ","}}`, "./...")
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

// goList runs `go list` at the repository root and returns its output lines.
func goList(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v", args, err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
