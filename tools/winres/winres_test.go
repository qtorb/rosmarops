package main

import (
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf16"
)

// icoPath / gitignorePath are relative to this package (tools/winres).
const (
	repoICO       = "../../assets/windows/rosmarops.ico"
	repoGitignore = "../../.gitignore"
	sysoRelPath   = "cmd/rosmarops/rsrc_windows_amd64.syso"
	// The official rosemary icon (BV-149, signed).
	icoSHA256 = "8675c06fc09caac9bbb1848dabd4619ef1434828a4215dcf6301451c417c7227"
)

func readICO(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(repoICO)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// BV-149 point 1: the committed icon is exactly the signed one.
func TestICOHash(t *testing.T) {
	sum := sha256.Sum256(readICO(t))
	if got := hex.EncodeToString(sum[:]); got != icoSHA256 {
		t.Errorf("assets/windows/rosmarops.ico SHA-256 = %s, want %s", got, icoSHA256)
	}
}

// BV-149 point 2: the .syso is byte-for-byte deterministic — the same inputs
// always produce the same object, with nothing time- or machine-dependent.
func TestDeterministic(t *testing.T) {
	ico := readICO(t)
	a, err := buildSyso(ico, "0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildSyso(ico, "0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("two runs produced different bytes")
	}
	if len(a) == 0 {
		t.Fatal("empty object")
	}
	// A different version must change the object (the version is really in it).
	c, err := buildSyso(ico, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(c) {
		t.Error("version is not reflected in the object")
	}
}

// BV-149 point 2: the generator uses only the standard library (no third-party
// dependency, and nothing that reaches the network).
func TestNoExternalImports(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			// Standard-library import paths have no dot in their first segment;
			// third-party ones look like host.tld/owner/repo.
			first := path
			if i := strings.IndexByte(path, '/'); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports non-standard package %q", e.Name(), path)
			}
			// Explicitly: no networking, even from the standard library.
			if path == "net" || strings.HasPrefix(path, "net/") || path == "crypto/tls" {
				t.Errorf("%s imports networking package %q", e.Name(), path)
			}
		}
	}
}

// BV-149 point 2: the generated .syso is git-ignored (it is a build input, not
// committed).
func TestSysoIgnored(t *testing.T) {
	b, err := os.ReadFile(repoGitignore)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == sysoRelPath {
			found = true
		}
	}
	if !found {
		t.Errorf(".gitignore does not ignore %q", sysoRelPath)
	}
}

// walkVS parses a VS_VERSIONINFO block the way VerQueryValue does and returns
// the StringFileInfo strings — so a layout bug that makes Windows read empty
// values (as it did for ProductName) fails here on any OS. The returned map is
// key -> value for every String under StringFileInfo/StringTable.
func walkVS(t *testing.T, data []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	var walk func(d []byte, path string)
	walk = func(d []byte, path string) {
		for off := 0; off+6 <= len(d); {
			wLength := int(d[off]) | int(d[off+1])<<8
			if wLength < 6 || off+wLength > len(d) {
				return
			}
			block := d[off : off+wLength]
			wValueLen := int(block[2]) | int(block[3])<<8
			wType := int(block[4]) | int(block[5])<<8
			// Key: UTF-16 until a NUL word, from offset 6.
			k := 6
			for k+1 < len(block) && !(block[k] == 0 && block[k+1] == 0) {
				k += 2
			}
			key := decodeUTF16(block[6:k])
			p := align4(k + 2) // past the key NUL, aligned
			valBytes := wValueLen
			if wType == 1 {
				valBytes *= 2 // text value length is in WCHARs
			}
			var value []byte
			if p+valBytes <= len(block) {
				value = block[p : p+valBytes]
			}
			childStart := align4(p + valBytes)

			switch {
			case key == "StringFileInfo", path == "StringFileInfo": // StringTable(s) / its strings
				if childStart < len(block) {
					walk(block[childStart:], key)
				}
			default:
				if key != "" && path != "" && path != "StringFileInfo" && wType == 1 && len(value) > 0 {
					out[key] = decodeUTF16(value)
				}
				if childStart < len(block) {
					walk(block[childStart:], key)
				}
			}
			off += align4(wLength)
		}
	}
	walk(data, "")
	return out
}

func decodeUTF16(b []byte) string {
	var u []uint16
	for i := 0; i+1 < len(b); i += 2 {
		c := uint16(b[i]) | uint16(b[i+1])<<8
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

func align4(n int) int { return (n + 3) &^ 3 }

// BV-149: the version resource parses the way Windows will read it — the strings
// come back non-empty (this is the ProductName=” failure caught off Windows).
func TestVersionInfoParses(t *testing.T) {
	data, err := versionInfo("0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	got := walkVS(t, data)
	want := map[string]string{
		"ProductName":      "RosmarOps",
		"FileDescription":  "RosmarOps",
		"LegalCopyright":   "© 2026 Albert Garcia Pujadas. AGPL-3.0",
		"OriginalFilename": "rosmarops.exe",
		"ProductVersion":   "0.7.2",
		"FileVersion":      "0.7.2",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d strings %v, want %d", len(got), got, len(want))
	}
}

// findRTVersion returns the bytes of the first RT_VERSION (type 16) resource in
// a .rsrc section, resolving the data entry's RVA against the section's RVA.
func findRTVersion(t *testing.T, rsrc []byte, sectionRVA uint32) []byte {
	t.Helper()
	le16 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
	le32 := func(b []byte) uint32 { return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24 }
	n := le16(rsrc[12:]) + le16(rsrc[14:])
	for i, off := 0, 16; i < n; i, off = i+1, off+8 {
		id := le32(rsrc[off:])
		sub := le32(rsrc[off+4:])
		if id != 16 || sub&0x80000000 == 0 {
			continue
		}
		l2 := int(sub & 0x7fffffff)  // RT_VERSION type dir
		sub2 := le32(rsrc[l2+16+4:]) // first id entry -> id subdir
		l3 := int(sub2 & 0x7fffffff) // id (language) dir
		sub3 := le32(rsrc[l3+16+4:]) // first language entry -> data entry
		de := int(sub3 & 0x7fffffff) // data entry (high bit clear)
		rva := le32(rsrc[de:])       // OffsetToData (already an RVA after linking)
		size := le32(rsrc[de+4:])
		start := rva - sectionRVA
		return rsrc[start : start+size]
	}
	t.Fatal("no RT_VERSION resource in .rsrc")
	return nil
}

// BV-149: the resource must survive the linker. A tiny program is cross-built
// for windows/amd64 with the generated .syso; the RT_VERSION resource is read
// back from the linked PE and its strings must be non-empty. This catches
// linker-level mistakes (a bad relocation points the resource at the wrong
// bytes) that a check of the generator's output alone would miss.
func TestLinksIntoExe(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-builds an exe")
	}
	syso, err := buildSyso(readICO(t), "0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dir+"/rsrc_windows_amd64.syso", syso, 0o644); err != nil {
		t.Fatal(err)
	}
	write("go.mod", "module winrestest\n\ngo 1.24\n")
	write("main.go", "package main\n\nfunc main() {}\n")
	exe := dir + "/out.exe"
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross build failed: %v\n%s", err, out)
	}
	f, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rsrc *pe.Section
	for _, s := range f.Sections {
		if s.Name == ".rsrc" {
			rsrc = s
		}
	}
	if rsrc == nil {
		t.Fatal("no .rsrc section in the linked exe")
	}
	data, err := rsrc.Data()
	if err != nil {
		t.Fatal(err)
	}
	ver := findRTVersion(t, data, rsrc.VirtualAddress)
	got := walkVS(t, ver)
	if got["ProductName"] != "RosmarOps" || got["ProductVersion"] != "0.7.2" || got["OriginalFilename"] != "rosmarops.exe" {
		t.Errorf("linked RT_VERSION strings = %v", got)
	}
}
