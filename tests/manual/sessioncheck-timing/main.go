// Command sessioncheck-timing is the BV-253 §C.1 measurement: it runs
// `rosmarops session-check` with a test SessionStart input 20 times, cold and
// warm, with 1 and with 200 projects that already have a snapshot, and prints
// the median, p95 and maximum wall time. It is a manual helper (tests/manual),
// never part of the program; the measure-plugin workflow runs it on the GitHub
// runners of Windows, macOS and Linux.
//
//	go run ./tests/manual/sessioncheck-timing -bin ./rosmarops -projects 200
//
// Everything happens under a temporary home (HOME and USERPROFILE point at it),
// so the runner's own folders are not read or written. Each project has a
// CLAUDE.md and a .claude/settings.json; before measuring, session-check runs
// once in every project, so each has its snapshot.
//
//   - warm: the same binary, 20 times in a row, in the first project;
//   - cold: before each run the binary is copied to a new file never executed
//     before (the copy is not timed), and the run goes to the next project in
//     turn, so the binary, its folder and that project's files are not the ones
//     the previous run used. It is not a cold machine (the runners do not allow
//     dropping the system cache), only the coldest start available there.
//
// It fails if a run exits non-zero or prints anything but nothing or one JSON
// object.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

func main() {
	bin := flag.String("bin", "", "rosmarops binary")
	projects := flag.Int("projects", 1, "projects with a snapshot")
	runs := flag.Int("runs", 20, "runs per mode")
	flag.Parse()
	if err := run(*bin, *projects, *runs); err != nil {
		fmt.Fprintln(os.Stderr, "sessioncheck-timing:", err)
		os.Exit(1)
	}
}

func run(bin string, n, runs int) error {
	bin, err := filepath.Abs(bin)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "sc-timing-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "home")
	var dirs []string
	for i := 0; i < n; i++ {
		d := filepath.Join(home, "code", fmt.Sprintf("project-%03d", i))
		if err := os.MkdirAll(filepath.Join(d, ".claude"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(d, "CLAUDE.md"), []byte("# Project\n\nRun the tests before committing.\n"), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(d, ".claude", "settings.json"), []byte(`{"permissions":{"allow":["Bash(go test:*)"]}}`), 0o644); err != nil {
			return err
		}
		dirs = append(dirs, d)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("Be brief.\n"), 0o644); err != nil {
		return err
	}
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)

	// Every project gets its snapshot.
	for _, d := range dirs {
		if _, err := once(bin, d, env); err != nil {
			return fmt.Errorf("seed %s: %w", d, err)
		}
	}

	var warm, cold []time.Duration
	for i := 0; i < runs; i++ {
		t, err := once(bin, dirs[0], env)
		if err != nil {
			return fmt.Errorf("warm %d: %w", i, err)
		}
		warm = append(warm, t)
	}
	for i := 0; i < runs; i++ {
		fresh := filepath.Join(root, fmt.Sprintf("bin-%02d", i), filepath.Base(bin))
		if err := copyFile(bin, fresh); err != nil {
			return err
		}
		t, err := once(fresh, dirs[(i+1)%len(dirs)], env)
		if err != nil {
			return fmt.Errorf("cold %d: %w", i, err)
		}
		cold = append(cold, t)
	}
	fmt.Printf("os=%s/%s projects=%d runs=%d\n", runtime.GOOS, runtime.GOARCH, n, runs)
	for _, m := range []struct {
		name string
		d    []time.Duration
	}{{"cold", cold}, {"warm", warm}} {
		med, p95, max := stats(m.d)
		fmt.Printf("  %s: median %d ms, p95 %d ms, max %d ms; all: %v\n", m.name, med.Milliseconds(), p95.Milliseconds(), max.Milliseconds(), ms(m.d))
	}
	return nil
}

// once runs session-check in dir with the test SessionStart input and returns
// its wall time.
func once(bin, dir string, env []string) (time.Duration, error) {
	in, _ := json.Marshal(map[string]string{
		"session_id": "00000000-0000-0000-0000-000000000000", "transcript_path": filepath.Join(dir, "t.jsonl"),
		"cwd": dir, "hook_event_name": "SessionStart", "source": "startup",
	})
	cmd := exec.Command(bin, "session-check")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(in)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	start := time.Now()
	err := cmd.Run()
	d := time.Since(start)
	if err != nil {
		return d, err
	}
	if b := bytes.TrimSpace(out.Bytes()); len(b) > 0 {
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			return d, fmt.Errorf("output is not one JSON object: %q", b)
		}
	}
	return d, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

// stats gives the median, the p95 (nearest rank) and the maximum.
func stats(d []time.Duration) (med, p95, max time.Duration) {
	s := append([]time.Duration{}, d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		med = s[n/2]
	} else {
		med = (s[n/2-1] + s[n/2]) / 2
	}
	rank := (95*n + 99) / 100
	return med, s[rank-1], s[n-1]
}

func ms(d []time.Duration) []int64 {
	out := make([]int64, len(d))
	for i, x := range d {
		out[i] = x.Milliseconds()
	}
	return out
}
