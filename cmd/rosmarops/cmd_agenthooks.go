package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-226 §0: prototype of the two Claude Code commands, to measure them on the
// Windows runner before the rest is written. session-check reads the hook
// input, checks integrity without asking or writing, reads the files of
// CTO-44 §4a point 2, fingerprints each item and compares it with the
// project's snapshot; statusline checks integrity and prints one line of
// data/statusline.txt. Both are hidden from the help: Claude Code runs them.

func newSessionCheckCmd() *cli.Command {
	return &cli.Command{Name: "session-check", Hidden: true, Args: 0, Run: runSessionCheck}
}

func newStatuslineCmd() *cli.Command {
	return &cli.Command{Name: "statusline", Hidden: true, Args: 0, Run: runStatusline}
}

// quietIntegrity is invariant 8 for these two commands: the same comparison
// as every command, but it never asks, never writes and never aborts; false
// on a mismatch.
func quietIntegrity() bool {
	stPath, err := state.StatePath()
	if err != nil {
		return true
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return true
	}
	if ref := st.Integrity.VigilBinarySHA256; ref != "" && ref != zeroHash {
		self, err := hash.Self()
		if err != nil || self != ref {
			return false
		}
	}
	if ref := st.Integrity.ConfigSHA256; ref != "" && ref != zeroHash {
		if p, err := resolveCfgPath(); err == nil {
			if h, err := hash.File(p); err == nil && h != ref {
				return false
			}
		}
	}
	return true
}

type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	Source        string `json:"source"`
	Cwd           string `json:"cwd"`
}

func runSessionCheck(cli.Invocation) error {
	done := make(chan struct{})
	go func() { defer close(done); sessionCheck(os.Stdin, os.Stdout) }()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
	return nil
}

func sessionCheck(in io.Reader, out io.Writer) {
	raw, _ := io.ReadAll(io.LimitReader(in, 1<<16))
	var hi hookInput
	if json.Unmarshal(raw, &hi) != nil || hi.HookEventName != "SessionStart" || (hi.Source != "startup" && hi.Source != "resume") || hi.Cwd == "" {
		return
	}
	if !quietIntegrity() {
		fmt.Fprintln(out, `{"systemMessage":"RosmarOps changed"}`)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	items := map[string]string{}
	for _, f := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(hi.Cwd, ".claude", "settings.json"),
		filepath.Join(hi.Cwd, ".claude", "settings.local.json"),
		filepath.Join(hi.Cwd, ".mcp.json"),
		filepath.Join(home, ".claude.json"),
	} {
		b, err := os.ReadFile(f)
		if err != nil || len(b) > 2<<20 {
			continue
		}
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			continue
		}
		collect(items, f, v)
	}
	for _, d := range []string{filepath.Join(home, ".claude", "skills"), filepath.Join(hi.Cwd, ".claude", "skills"), filepath.Join(hi.Cwd, ".claude", "agents")} {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			items["name|"+d+"|"+e.Name()] = ""
		}
	}
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	snap, _ := json.Marshal(keys)
	dir := filepath.Join(home, ".rosmarops", "reports", report.DataSubdir, "session")
	sum := sha256.Sum256([]byte(hi.Cwd))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".json")
	prev, _ := os.ReadFile(path)
	if !bytes.Equal(prev, snap) {
		_ = os.MkdirAll(dir, 0o700)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, snap, 0o600) == nil {
			_ = os.Rename(tmp, path)
		}
		fmt.Fprintln(out, `{"systemMessage":"RosmarOps · changed"}`)
	}
}

// collect fingerprints the allow rules, defaultMode, hook commands and MCP
// servers of one parsed settings file.
func collect(items map[string]string, file string, v map[string]any) {
	if p, ok := v["permissions"].(map[string]any); ok {
		if a, ok := p["allow"].([]any); ok {
			for _, r := range a {
				if s, ok := r.(string); ok {
					items["allow|"+hash.Bytes([]byte(s))] = ""
				}
			}
		}
		if m, ok := p["defaultMode"].(string); ok {
			items["mode|"+m] = ""
		}
	}
	if h, ok := v["hooks"].(map[string]any); ok {
		b, _ := json.Marshal(h)
		items["hooks|"+file+"|"+hash.Bytes(b)] = ""
	}
	if m, ok := v["mcpServers"].(map[string]any); ok {
		for name, s := range m {
			b, _ := json.Marshal(s)
			items["mcp|"+name+"|"+hash.Bytes(b)] = ""
		}
	}
}

func runStatusline(cli.Invocation) error {
	if !quietIntegrity() {
		fmt.Println("RosmarOps changed")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", report.DataSubdir, "statusline.txt"))
	if err != nil {
		return nil
	}
	for _, l := range strings.Split(string(b), "\n") {
		if lang, text, ok := strings.Cut(l, "\t"); ok && lang == "en" {
			fmt.Println(text)
		}
	}
	return nil
}
