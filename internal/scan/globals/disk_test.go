package globals

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifest(name, version string) string {
	return `{"name":"` + name + `","version":"` + version + `","scripts":{"postinstall":"x"}}`
}

func sortedNames(pkgs []GlobalPackage) []string {
	var out []string
	for _, p := range pkgs {
		out = append(out, p.Name+"@"+p.Version)
	}
	sort.Strings(out)
	return out
}

// clearPackageEnv unsets every variable the resolvers read, so the host's
// own npm/pnpm configuration cannot leak into a test.
func clearPackageEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToLower(name) {
		case "npm_config_prefix", "npm_config_userconfig", "npm_config_global_dir", "pnpm_home", "xdg_data_home":
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
}

func TestReadGlobalModules_PlainScopedLinkedAndDotEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	nm := filepath.Join(root, "node_modules")
	writeTestFile(t, filepath.Join(nm, "alpha", "package.json"), manifest("alpha", "1.0.0"))
	writeTestFile(t, filepath.Join(nm, "@scope", "beta", "package.json"), manifest("@scope/beta", "2.1.0"))
	writeTestFile(t, filepath.Join(root, "src", "gamma", "package.json"), manifest("gamma", "3.0.0"))
	// A nested dependency below a package must not be listed.
	writeTestFile(t, filepath.Join(nm, "alpha", "node_modules", "nested", "package.json"), manifest("nested", "9.9.9"))
	writeTestFile(t, filepath.Join(nm, ".package-lock.json"), `{}`)
	writeTestFile(t, filepath.Join(nm, ".pnpm", "x", "package.json"), manifest("x", "0.0.1"))
	if err := os.Symlink(filepath.Join(root, "src", "gamma"), filepath.Join(nm, "gamma")); err != nil {
		t.Fatal(err)
	}

	pkgs, errs := readGlobalModules(nm)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := []string{"@scope/beta@2.1.0", "alpha@1.0.0", "gamma@3.0.0"}
	if got := sortedNames(pkgs); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadGlobalModules_BadManifestIsSkipped(t *testing.T) {
	nm := filepath.Join(t.TempDir(), "node_modules")
	writeTestFile(t, filepath.Join(nm, "good", "package.json"), manifest("good", "1.0.0"))
	writeTestFile(t, filepath.Join(nm, "broken", "package.json"), `{not json`)
	writeTestFile(t, filepath.Join(nm, "nameless", "package.json"), `{"version":"1.0.0"}`)
	if err := os.MkdirAll(filepath.Join(nm, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	big := `{"name":"big","version":"1.0.0","pad":"` + strings.Repeat("x", maxManifestBytes) + `"}`
	writeTestFile(t, filepath.Join(nm, "big", "package.json"), big)

	pkgs, errs := readGlobalModules(nm)
	if got := sortedNames(pkgs); !reflect.DeepEqual(got, []string{"good@1.0.0"}) {
		t.Errorf("got %v, want only good@1.0.0", got)
	}
	if len(errs) != 4 {
		t.Errorf("want 4 errors (broken, nameless, empty, big), got %d: %v", len(errs), errs)
	}
}

func TestReadGlobalModules_MissingDirIsEmpty(t *testing.T) {
	pkgs, errs := readGlobalModules(filepath.Join(t.TempDir(), "absent"))
	if pkgs != nil || errs != nil {
		t.Errorf("want nothing, got %v %v", pkgs, errs)
	}
}

func TestNpmrcPrefix_ReadsOnlyPrefix(t *testing.T) {
	t.Setenv("RT_BASE", "/opt/base")
	path := filepath.Join(t.TempDir(), ".npmrc")
	writeTestFile(t, path, strings.Join([]string{
		"registry=https://registry.npmjs.org/",
		"//registry.npmjs.org/:_authToken=npm_FAKEFAKEFAKEFAKEFAKE",
		"; prefix=/commented",
		`prefix = "${RT_BASE}/global"`,
	}, "\n"))

	got, ok, err := npmrcPrefix(path)
	if err != nil || !ok {
		t.Fatalf("npmrcPrefix: %q %v %v", got, ok, err)
	}
	if got != "/opt/base/global" {
		t.Errorf("prefix = %q, want /opt/base/global", got)
	}
	if strings.Contains(got, "FAKE") {
		t.Fatal("auth token leaked into the prefix value")
	}
}

func TestNpmrcPrefix_MissingAndOversize(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := npmrcPrefix(filepath.Join(dir, "absent")); ok || err != nil {
		t.Errorf("missing file: ok=%v err=%v", ok, err)
	}
	big := filepath.Join(dir, "big")
	writeTestFile(t, big, "prefix=/x\n"+strings.Repeat("#", maxManifestBytes))
	if _, ok, err := npmrcPrefix(big); ok || err == nil {
		t.Errorf("oversize file must not be read: ok=%v err=%v", ok, err)
	}
}

func TestNpmGlobalModulesDir_Precedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	clearPackageEnv(t)
	home := t.TempDir()
	// Platform default: one level above the node executable found on PATH.
	bin := filepath.Join(t.TempDir(), "nodeprefix", "bin")
	writeTestFile(t, filepath.Join(bin, "node"), "")
	if err := os.Chmod(filepath.Join(bin, "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	// The node executable's links are resolved: on macOS the temporary
	// folder /var/folders/… is /private/var/folders/… (BV-98).
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(resolved), "lib", "node_modules")
	if got, _ := npmGlobalModulesDir(home); got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}

	// The user npmrc beats the default.
	writeTestFile(t, filepath.Join(home, ".npmrc"), "prefix=/from/npmrc\n")
	if got, _ := npmGlobalModulesDir(home); got != filepath.Join("/from/npmrc", "lib", "node_modules") {
		t.Errorf("npmrc: got %q", got)
	}

	// npm_config_userconfig selects which npmrc is read.
	other := filepath.Join(t.TempDir(), "npmrc")
	writeTestFile(t, other, "prefix=/from/userconfig\n")
	t.Setenv("NPM_CONFIG_USERCONFIG", other)
	if got, _ := npmGlobalModulesDir(home); got != filepath.Join("/from/userconfig", "lib", "node_modules") {
		t.Errorf("userconfig: got %q", got)
	}

	// The environment variable beats both, in any case.
	t.Setenv("NPM_CONFIG_PREFIX", "/from/env")
	if got, _ := npmGlobalModulesDir(home); got != filepath.Join("/from/env", "lib", "node_modules") {
		t.Errorf("env: got %q", got)
	}

	// No node on PATH and nothing configured: npm is treated as absent.
	clearPackageEnv(t)
	t.Setenv("PATH", t.TempDir())
	if got, err := npmGlobalModulesDir(t.TempDir()); got != "" || err != nil {
		t.Errorf("absent: got %q %v", got, err)
	}
}

func TestPnpmGlobalDir_Precedence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux default")
	}
	clearPackageEnv(t)
	home := "/home/u"
	if got := pnpmGlobalDir(home); got != "/home/u/.local/share/pnpm/global" {
		t.Errorf("default: %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if got := pnpmGlobalDir(home); got != "/xdg/pnpm/global" {
		t.Errorf("XDG_DATA_HOME: %q", got)
	}
	t.Setenv("PNPM_HOME", "/pnpm-home")
	if got := pnpmGlobalDir(home); got != "/pnpm-home/global" {
		t.Errorf("PNPM_HOME: %q", got)
	}
	t.Setenv("NPM_CONFIG_GLOBAL_DIR", "/global-dir")
	if got := pnpmGlobalDir(home); got != "/global-dir" {
		t.Errorf("npm_config_global_dir: %q", got)
	}
}

func TestPnpmGlobalModulesDirs_NumericLayouts(t *testing.T) {
	clearPackageEnv(t)
	gd := t.TempDir()
	t.Setenv("npm_config_global_dir", gd)
	for _, d := range []string{"5", "notalayout"} {
		if err := os.MkdirAll(filepath.Join(gd, d, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := pnpmGlobalModulesDirs("")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(gd, "5", "node_modules")}; !reflect.DeepEqual(dirs, want) {
		t.Errorf("got %v, want %v", dirs, want)
	}
}

// TestScanNpmrc_UserconfigUnsetUnchanged: with npm_config_userconfig unset,
// the registry check reads ~/.npmrc exactly as before.
func TestScanNpmrc_UserconfigUnsetUnchanged(t *testing.T) {
	clearPackageEnv(t)
	t.Setenv("PATH", t.TempDir()) // no node: no npm packages to list
	home := t.TempDir()
	path := filepath.Join(home, ".npmrc")
	writeTestFile(t, path, "registry=https://evil.example.invalid/\n")

	if got := userNpmrcPath(home); got != path {
		t.Fatalf("userNpmrcPath = %q, want %q", got, path)
	}
	wantFindings, wantSHA := scanNpmrc(path)
	res := ScanGlobals(context.Background(), home, nil, nil)
	if res.NewNpmrcSHA != wantSHA || wantSHA == "" {
		t.Errorf("sha: got %q, want %q", res.NewNpmrcSHA, wantSHA)
	}
	if !reflect.DeepEqual(res.Findings, wantFindings) || len(wantFindings) != 1 {
		t.Errorf("findings: got %+v, want %+v", res.Findings, wantFindings)
	}
}

func TestScanNpmrc_UserconfigSelectsFile(t *testing.T) {
	clearPackageEnv(t)
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, ".npmrc"), "registry=https://registry.npmjs.org/\n")
	other := filepath.Join(t.TempDir(), "npmrc")
	writeTestFile(t, other, "registry=https://evil.example.invalid/\n")
	t.Setenv("npm_config_userconfig", other)

	res := ScanGlobals(context.Background(), home, nil, nil)
	if len(res.Findings) != 1 || res.Findings[0].Category != "npm_registry_non_standard" {
		t.Errorf("want the registry finding from npm_config_userconfig, got %+v", res.Findings)
	}
}

func TestScanGlobals_ReadsPackagesFromDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	clearPackageEnv(t)
	t.Setenv("PATH", t.TempDir())
	prefix := t.TempDir()
	t.Setenv("npm_config_prefix", prefix)
	writeTestFile(t, filepath.Join(prefix, "lib", "node_modules", "left-pad", "package.json"), manifest("left-pad", "1.3.0"))
	gd := t.TempDir()
	t.Setenv("npm_config_global_dir", gd)
	writeTestFile(t, filepath.Join(gd, "5", "node_modules", "cowsay", "package.json"), manifest("cowsay", "1.6.0"))

	res := ScanGlobals(context.Background(), "", nil, nil)
	var got []string
	for _, p := range res.NewPackageBaseline {
		got = append(got, p.Manager+":"+p.Name+"@"+p.Version)
	}
	sort.Strings(got)
	if want := []string{"npm:left-pad@1.3.0", "pnpm:cowsay@1.6.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("baseline: got %v, want %v", got, want)
	}
	if len(res.Findings) != 2 {
		t.Errorf("want 2 global_package_new findings, got %d", len(res.Findings))
	}
}
