package trust_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// BV-203 point 1 (CTO-24 E3): the graph assembly lives in one exported
// function, internal/pipeline.Assemble, called by the real run, the
// full-machine test and the factory-note test. BV-201 and BV-202 were the same
// class of failure — a bug `rosmarops run` hit on a real machine slipped past
// tests that assembled the graph on a path of their own.
//
// TestPipelineSingleAssembly is the mechanical guard: it fails if cmd/rosmarops
// or tests/trust call graph.Build, graph.Infer or any graph.Cross* directly,
// instead of going through internal/pipeline. Build, Infer and the Cross steps
// are the steps whose order and inputs the single assembly fixes; the lower-
// level readers (FromScans*, Merge, Add*, Mark*) stay available.
func TestPipelineSingleAssembly(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(self), "..", ".."))
	dirs := []string{
		filepath.Join(repoRoot, "cmd", "rosmarops"),
		filepath.Join(repoRoot, "tests", "trust"),
	}
	// forbidden reports whether a graph.<name> call is one the single assembly
	// owns: Build, Infer or any Cross*.
	forbidden := func(name string) bool {
		return name == "Build" || name == "Infer" || strings.HasPrefix(name, "Cross")
	}

	var violations []string
	for _, dir := range dirs {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok || pkgIdent.Name != "graph" || !forbidden(sel.Sel.Name) {
						return true
					}
					pos := fset.Position(call.Pos())
					rel, err := filepath.Rel(repoRoot, path)
					if err != nil {
						rel = path
					}
					violations = append(violations, rel+":"+itoa(pos.Line)+": graph."+sel.Sel.Name)
					return true
				})
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("graph assembly must go through internal/pipeline, not a direct call (E3). Offenders:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
