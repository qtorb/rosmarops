package trust_test

// Trust test (CLAUDE.md invariant 1): RosmarOps opens no network connection.
//
// Static part, never skipped (decision 2026-10-25, sustituir gojsonschema,
// §4): no package of the module imports a network package directly; the
// binary's dependency graph, for Windows and for Linux, holds no net, net/*
// or crypto/tls package; the binary itself, built here, has no symbol of
// those packages nor syscall.Socket, syscall.Connect or syscall.Sendto; and
// no code of the module refers to a socket call of syscall or
// golang.org/x/sys. Test imports do not count: gojsonschema, the oracle of
// the schema validator's differential test, exists in test binaries only.
//
// Runtime part (Linux, needs strace): init, baseline and run in the sandbox of
// no_writes_out_of_scope_test.go, with every child process traced; any
// AF_INET or AF_INET6 connect/sendto/sendmsg fails the test.

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

// networkPackages are the standard-library packages that open sockets.
// Subpackages of net/http count as net/http.
var networkPackages = map[string]bool{
	"net":        true,
	"net/http":   true,
	"crypto/tls": true,
	"net/rpc":    true,
	"net/smtp":   true,
}

func isNetworkPackage(path string) bool {
	return networkPackages[path] || strings.HasPrefix(path, "net/http/")
}

// inBinaryGraph reports a package the binary's dependency graph must not
// hold (§4 check 1): net, any net/*, crypto/tls.
func inBinaryGraph(path string) bool {
	return path == "net" || strings.HasPrefix(path, "net/") || path == "crypto/tls"
}

// forbiddenSymbol reports a symbol the binary must not have (§4 check 2,
// calibrated on the first clean binary of BV-49): any symbol of net, net/*
// or crypto/tls, and the socket calls of syscall. The Winsock symbols of
// internal/poll and internal/syscall/windows that os links on Windows are
// not forbidden: no socket is created through them unless net asks.
func forbiddenSymbol(name string) bool {
	switch {
	case strings.HasPrefix(name, "net."), strings.HasPrefix(name, "net/"), strings.HasPrefix(name, "crypto/tls."):
		return true
	}
	for _, s := range []string{"syscall.Socket", "syscall.Connect", "syscall.Sendto"} {
		if name == s || strings.HasPrefix(name, s+".") {
			return true
		}
	}
	return false
}

// socketCalls are the socket functions module code must not refer to (§4
// check 3), by import path.
var socketCalls = map[string]map[string]bool{
	"syscall":                  {"Socket": true, "Connect": true, "Sendto": true, "Sendmsg": true, "SendmsgN": true, "WSASocket": true, "WSAConnect": true, "WSASendto": true},
	"golang.org/x/sys/unix":    {"Socket": true, "Connect": true, "Sendto": true, "Sendmsg": true, "SendmsgN": true},
	"golang.org/x/sys/windows": {"Socket": true, "Connect": true, "Sendto": true, "WSASocket": true, "WSAConnect": true, "WSASendto": true, "WSASendTo": true},
}

func TestNoNetwork_ModuleImportsNoNetworkPackage(t *testing.T) {
	out := goList(t, "-f", `{{.ImportPath}}|{{join .Imports ","}},{{join .TestImports ","}},{{join .XTestImports ","}}`, "./...")
	for _, line := range out {
		pkg, imports, _ := strings.Cut(line, "|")
		for _, imp := range strings.Split(imports, ",") {
			if isNetworkPackage(imp) {
				t.Errorf("module package %s imports %s", pkg, imp)
			}
		}
	}
}

// §4 check 1: the binary's dependency graph, for both platforms.
func TestNoNetwork_BinaryDependencyGraph(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		cmd := exec.Command("go", "list", "-deps", "./cmd/rosmarops")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps (%s): %v", goos, err)
		}
		n := 0
		for _, pkg := range strings.Fields(string(out)) {
			n++
			if inBinaryGraph(pkg) {
				t.Errorf("GOOS=%s: the binary depends on %s", goos, pkg)
			}
		}
		if n < 10 {
			t.Fatalf("GOOS=%s: go list -deps listed %d packages", goos, n)
		}
	}
}

// §4 check 2: the symbols of the binary, built here for Windows and for
// Linux as the release builds it.
func TestNoNetwork_BinarySymbols(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		bin := filepath.Join(t.TempDir(), "rosmarops-"+goos)
		build := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/rosmarops")
		build.Dir = repoRoot(t)
		build.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build (%s): %v\n%s", goos, err, out)
		}
		out, err := exec.Command("go", "tool", "nm", bin).Output()
		if err != nil {
			t.Fatalf("go tool nm (%s): %v", goos, err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 1000 {
			t.Fatalf("GOOS=%s: nm listed %d symbols", goos, len(lines))
		}
		for _, l := range lines {
			f := strings.Fields(l)
			if len(f) >= 3 && forbiddenSymbol(f[len(f)-1]) {
				t.Errorf("GOOS=%s: the binary has %s", goos, f[len(f)-1])
			}
		}
	}
}

// §4 check 3: no code of the module refers to a socket call of syscall or
// golang.org/x/sys, whatever name it imports them under.
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

// The checks above catch what they are meant to.
func TestNoNetwork_ChecksCatch(t *testing.T) {
	for _, s := range []string{"net.Dial", "net/http.Get", "net/netip.ParseAddr", "crypto/tls.Client", "syscall.Socket", "syscall.Connect", "syscall.Sendto"} {
		if !forbiddenSymbol(s) {
			t.Errorf("%s not forbidden", s)
		}
	}
	for _, s := range []string{"internal/poll.InitWSA", "syscall.WSAStartup", "netbuf.x", "os.Open", "syscall.Socketpair"} {
		if forbiddenSymbol(s) {
			t.Errorf("%s forbidden", s)
		}
	}
	for _, p := range []string{"net", "net/url", "net/netip", "crypto/tls"} {
		if !inBinaryGraph(p) {
			t.Errorf("%s allowed in the graph", p)
		}
	}
}

func TestNoNetwork_RuntimeNoInetSockets(t *testing.T) {
	if runtime.GOOS != "linux" {
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
