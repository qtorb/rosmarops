// Command conpty is the journey driver on Windows: it runs a command in a
// pseudo console (ConPTY) and walks it through the same steps file as
// tests/journeys/drive/drive.py does on Linux and macOS, with the same rules:
//
//	expect      <catalog key or text>   [timeout <N>s]
//	send        <text>                  (\r, \n and \t are escapes)
//	expect-exit <code>                  [timeout <N>s]
//	if-asked    <catalog key or text> send <text>
//
// An expect naming a catalog key waits for that text in the language given,
// its {params} matching anything. Output is compared without escape codes and
// with whitespace runs as one space. While waiting for the exit, a process
// silent for -idle seconds fails: a question the journey did not plan for.
// Only the standard library: kernel32 through syscall, no dependency. A test
// tool, never part of RosmarOps.
//
//	go run ./tests/journeys/drive/conpty -steps r1.steps -catalog internal/catalog/messages -lang es -text out.txt -- rosmarops.exe
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type step struct {
	kind    string // expect, send, expect-exit, if-asked
	arg     string
	answer  string // if-asked
	timeout int    // seconds, 0: the default
	line    int
}

var (
	stepRx    = regexp.MustCompile(`^(expect-exit|expect|send)\s+(.*?)(?:\s+timeout\s+(\d+)s)?\s*$`)
	ifAskedRx = regexp.MustCompile(`^if-asked\s+(.*?)\s+send\s+(.*)$`)
	paramRx   = regexp.MustCompile(`\{[a-z_]+\}`)
	ansiRx    = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]")
	spaceRx   = regexp.MustCompile(`\s+`)
)

func parseSteps(path string) ([]step, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []step
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := ifAskedRx.FindStringSubmatch(line); m != nil {
			out = append(out, step{kind: "if-asked", arg: m[1], answer: m[2], line: n})
			continue
		}
		m := stepRx.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: not a step: %q", path, n, line)
		}
		s := step{kind: m[1], arg: m[2], line: n}
		if m[3] != "" {
			s.timeout, _ = strconv.Atoi(m[3])
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func loadCatalog(dir, lang string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, lang+".json"))
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	return m, json.Unmarshal(b, &m)
}

// plain is the output without escape codes, with one kind of line break.
func plain(raw string) string {
	s := ansiRx.ReplaceAllString(raw, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func squeeze(s string) string { return spaceRx.ReplaceAllString(s, " ") }

// pattern is an expect's argument as a regular expression: a catalog text
// with its parameters as wildcards, or the literal text.
func pattern(arg string, catalog map[string]string) *regexp.Regexp {
	text, ok := catalog[arg]
	if !ok {
		text = arg
	}
	parts := paramRx.Split(strings.TrimSpace(squeeze(text)), -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("(?s)" + strings.Join(parts, ".*?"))
}

func unescape(s string) string {
	return strings.NewReplacer(`\r`, "\r", `\n`, "\n", `\t`, "\t").Replace(s)
}
