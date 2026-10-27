package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BV-72: the version comes from the build, as release.yml sets it from the
// tag: built with -ldflags="-X main.version=9.9.9", --version, the
// integrity question and engine_version say 9.9.9.
func TestVersionFromLdflags(t *testing.T) {
	_, work, home, env := integrityEnv(t)
	bin := filepath.Join(t.TempDir(), "rosmarops")
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-X main.version=9.9.9", "-o", bin, "./cmd/rosmarops")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, flag := range []string{"--version", "-v"} {
		out, err := exec.Command(bin, flag).Output()
		if err != nil || strings.TrimSpace(string(out)) != "rosmarops version 9.9.9" {
			t.Errorf("%s: %q, %v", flag, out, err)
		}
	}

	rosmarops(t, bin, work, env, "init")
	stPath := filepath.Join(home, ".rosmarops", "state.json")
	var raw map[string]any
	readJSON(t, stPath, &raw)
	if raw["engine_version"] != "9.9.9" {
		t.Errorf("init: engine_version %v", raw["engine_version"])
	}
	// An earlier version's reference: the question shows both versions.
	raw["engine_version"] = "0.0.1"
	raw["integrity"].(map[string]any)["vigil_binary_sha256"] = strings.Repeat("a", 64)
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(stPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run")
	cmd.Dir, cmd.Env = work, env
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "Before: version 0.0.1, hash aaaaaaaaaaaaaaaa…") || !strings.Contains(string(out), "Now:    version 9.9.9, hash ") {
		t.Errorf("integrity question:\n%s", out)
	}

	// Accepted (baseline), the reference is of 9.9.9; the run's graph
	// says 9.9.9 too.
	rosmarops(t, bin, work, env, "baseline")
	readJSON(t, stPath, &raw)
	if raw["engine_version"] != "9.9.9" {
		t.Errorf("baseline: engine_version %v", raw["engine_version"])
	}
	rosmarops(t, bin, work, env, "run")
	var g struct {
		Provenance struct {
			EngineVersion string `json:"engine_version"`
		} `json:"provenance"`
	}
	readJSON(t, filepath.Join(home, ".rosmarops", "reports", "graph.v2.json"), &g)
	if g.Provenance.EngineVersion != "9.9.9" {
		t.Errorf("graph: engine_version %q", g.Provenance.EngineVersion)
	}
}
