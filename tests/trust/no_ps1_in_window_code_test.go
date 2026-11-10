package trust_test

// Trust test for BV-211b / CTO-34 §3(e): the PowerShell windows run their
// scripts through windowBootstrap over standard input, never from a file on
// disk. This test fails if any of the window source files in cmd/rosmarops
// creates a .ps1 file or calls os.CreateTemp — the two ways the superseded
// file-based launcher (commit 58f8c1f) put a script on disk. It parses the
// source (not a build, so it runs on every system) and flags a ".ps1" string
// literal or a call to os.CreateTemp.
//
// Scope is exactly the window code: the four window drivers, the neutral
// launcher and the Windows-only failure recorder. Other packages' legitimate
// os.CreateTemp uses (cmd_space.go, findings.go) are not window code and are
// out of scope.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoPS1FileInWindowCode(t *testing.T) {
	root := repoRoot(t)
	windowFiles := []string{
		"consolewin.go",
		"consolewin_windows.go",
		"windowscripts.go",
		"findingswindow_windows.go",
		"connectwindow_windows.go",
		"problemwindow_windows.go",
	}
	for _, name := range windowFiles {
		path := filepath.Join(root, "cmd", "rosmarops", name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		fset := token.NewFileSet()
		// Parse comments too: a window must not even leave a .ps1 path behind
		// in a comment, since that would mean the file-based path came back.
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING && strings.Contains(x.Value, ".ps1") {
					t.Errorf("%s: a .ps1 file name must not appear in window code (found %s)", name, x.Value)
				}
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "os" && x.Sel.Name == "CreateTemp" {
					t.Errorf("%s: os.CreateTemp must not be used in window code", name)
				}
			}
			return true
		})
		for _, group := range f.Comments {
			if strings.Contains(group.Text(), ".ps1") {
				t.Errorf("%s: a .ps1 file name must not appear even in a comment", name)
			}
		}
	}
}
