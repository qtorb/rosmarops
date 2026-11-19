package trust_test

// Static trust test: what RosmarOps prints for the person comes from the
// message catalog, in their language. A text written as a Go literal reaches
// the console in English whatever the language (the integrity stop of 0.15.0
// did). This walks cmd/ and internal/ with go/ast and fails when
//   - fmt.Print*, or fmt.Fprint* to os.Stdout or os.Stderr, receives a string
//     literal with a word in it, unless consoleLiterals lists it;
//   - abortWithIntegrityFailure or abortIntegrity receives a string literal
//     after the finding's category: an integrity stop's texts are catalog keys.
// Format verbs, punctuation and symbols are not words.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	formatVerb = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)
	aWord      = regexp.MustCompile(`[A-Za-z]{3,}`)
)

// consoleCall reports whether c prints to the console: fmt.Print* or fmt.Fprint*
// to os.Stdout or os.Stderr; skip is how many leading arguments are not text.
func consoleCall(c *ast.CallExpr) (ok bool, skip int) {
	sel, isSel := c.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false, 0
	}
	pkg, isID := sel.X.(*ast.Ident)
	if !isID || pkg.Name != "fmt" {
		return false, 0
	}
	switch sel.Sel.Name {
	case "Print", "Printf", "Println":
		return true, 0
	case "Fprint", "Fprintf", "Fprintln":
		if len(c.Args) == 0 {
			return false, 0
		}
		if w, ok := c.Args[0].(*ast.SelectorExpr); ok {
			if os, ok := w.X.(*ast.Ident); ok && os.Name == "os" && (w.Sel.Name == "Stdout" || w.Sel.Name == "Stderr") {
				return true, 1
			}
		}
	}
	return false, 0
}

// isAbort reports a call to the integrity stop, by name (abort is the
// closure run's check calls it through).
func isAbort(c *ast.CallExpr) bool {
	id, ok := c.Fun.(*ast.Ident)
	return ok && (id.Name == "abortWithIntegrityFailure" || id.Name == "abortIntegrity" || id.Name == "abort")
}

type literal struct{ file, text, pos string }

// sourceLiterals walks the program's Go sources (no tests) and returns the
// console literals with words, and the literals passed to an integrity stop.
func sourceLiterals(t *testing.T) (console, abort []literal) {
	t.Helper()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isAbort(c) {
					for _, a := range c.Args[1:] {
						if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
							s, _ := strconv.Unquote(bl.Value)
							abort = append(abort, literal{rel, s, fset.Position(bl.Pos()).String()})
						}
					}
				}
				if ok, skip := consoleCall(c); ok {
					for _, a := range c.Args[skip:] {
						if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
							s, _ := strconv.Unquote(bl.Value)
							if aWord.MatchString(formatVerb.ReplaceAllString(s, "")) {
								console = append(console, literal{rel, s, fset.Position(bl.Pos()).String()})
							}
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return console, abort
}

func TestNoConsoleTextOutsideCatalog(t *testing.T) {
	console, abort := sourceLiterals(t)
	allowed := map[string]map[string]bool{}
	for f, texts := range consoleLiterals {
		allowed[f] = map[string]bool{}
		for _, s := range texts {
			allowed[f][s] = true
		}
	}
	for _, l := range console {
		if !allowed[l.file][l.text] {
			t.Errorf("%s: %q reaches the console as a literal: take it from the catalog", l.pos, l.text)
		}
	}
	for _, l := range abort {
		t.Errorf("%s: %q passed to an integrity stop: its texts are catalog keys", l.pos, l.text)
	}
	if len(console) == 0 {
		t.Fatal("no console literal found at all: the walk is broken")
	}
}

// Every listed literal is still in its file: the list only shrinks.
func TestConsoleLiteralsListIsCurrent(t *testing.T) {
	console, _ := sourceLiterals(t)
	found := map[string]map[string]bool{}
	for _, l := range console {
		if found[l.file] == nil {
			found[l.file] = map[string]bool{}
		}
		found[l.file][l.text] = true
	}
	for f, texts := range consoleLiterals {
		for _, s := range texts {
			if !found[f][s] {
				t.Errorf("%s: %q is listed but no longer printed: remove it from consoleLiterals", f, s)
			}
		}
	}
}
