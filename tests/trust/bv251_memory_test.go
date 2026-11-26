package trust_test

// BV-251 (decision 2): RosmarOps never opens an agent's memory files — not with
// os.Open, not to read their content. It only lists folders and reads each
// file's metadata. Two proofs:
//   - the walk runs on a file system that fails the test on any file open,
//     and still counts every file and its size;
//   - the memory functions' source calls no open or read function.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// noOpenFS lists folders and gives metadata, and fails the test when a file
// is opened.
type noOpenFS struct {
	t    *testing.T
	fsys fstest.MapFS
}

func (n noOpenFS) Open(name string) (fs.File, error) {
	if info, err := fs.Stat(n.fsys, name); err == nil && !info.IsDir() {
		n.t.Errorf("memory file opened: %s", name)
	}
	return n.fsys.Open(name)
}

func (n noOpenFS) ReadDir(name string) ([]fs.DirEntry, error) { return n.fsys.ReadDir(name) }

func TestBV251_MemoryNeverOpened(t *testing.T) {
	mem := fstest.MapFS{
		"MEMORY.md":              {Data: []byte("AKIAABCDEFGHIJKLMNOP the agent remembers this")},
		"USER.md":                {Data: []byte("0123456789")},
		"topics/deploy.md":       {Data: []byte("abcde")},
		"topics/deep/a/b/c.json": {Data: []byte("{}")},
	}
	files, size := windows.MemoryWalk(noOpenFS{t: t, fsys: mem})
	if files != 4 || size != int64(len(mem["MEMORY.md"].Data)+10+5+2) {
		t.Errorf("walk: %d files, %d bytes", files, size)
	}

	// On the real file system, unreadable memory files are still counted:
	// only their metadata is read.
	home := t.TempDir()
	dir := filepath.Join(home, ".hermes", "memories")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("secret-like content"), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	scan := windows.ReadAlwaysOn(agentdirs.LinuxLayout, home, filepath.Join(home, ".claude"), nil, agentdirs.Env(func(string) (string, bool) { return "", false }), platform.ClaudeSettingsScan{})
	found := false
	for _, m := range scan.Memory {
		if m.Path == dir {
			found = m.Files == 2 && m.Bytes == 2*int64(len("secret-like content"))
		}
	}
	if !found {
		t.Errorf("Hermes memory not counted from metadata: %+v", scan.Memory)
	}

	// The memory functions call no open or read function.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("..", "..", "adapters", "windows", "alwayson.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"os.Open": true, "os.OpenFile": true, "os.ReadFile": true, "fs.ReadFile": true,
		"readBounded": true, "io.ReadAll": true,
	}
	memoryFuncs := map[string]bool{"addMemory": true, "MemoryStat": true, "MemoryWalk": true}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !memoryFuncs[fn.Name.Name] {
			continue
		}
		delete(memoryFuncs, fn.Name.Name)
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				if x, ok := fun.X.(*ast.Ident); ok {
					name = x.Name + "." + fun.Sel.Name
				}
				if fun.Sel.Name == "Open" {
					name = "Open"
					forbidden["Open"] = true
				}
			}
			if forbidden[name] {
				t.Errorf("%s calls %s", fn.Name.Name, name)
			}
			return true
		})
	}
	if len(memoryFuncs) > 0 {
		t.Errorf("memory functions not found: %v", memoryFuncs)
	}
}
