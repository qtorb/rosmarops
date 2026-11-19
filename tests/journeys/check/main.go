// Command check holds the fixed checks of the journey tests (journeys.yml).
// Every check reads files a journey left and prints one line per problem; any
// problem exits 1. Never part of the program.
//
//	check text     -catalog DIR -lang es FILE...      a. no {param} left; b. with es, no English sentence
//	check version  -want X.Y.Z -version-output FILE -v2 report.v2.json -html report.es.html
//	check leaks    -home DIR [-allow PATH[,PATH]] -forbid PATH[,PATH] FILE...
//	                                                   no runner path outside the test home and the
//	                                                   installed program, no host name
//	check salt     STATE_BEFORE STATE_AFTER           the same local_salt
//	check same     REPORT_V2_A REPORT_V2_B            the same findings, rules and score (exit 3: a repository timed out)
//	check timeouts REPORT_V2                          exit 3 if a repository or the review ran out of time
//
// FILE is a console log or an HTML page: an HTML page (.html) is read
// without its tags, styles, scripts and <code>/<pre> blocks (what the person's
// computer shows, such as paths), with its entities decoded.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"regexp"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: check text|version|leaks|salt|same|timeouts ...")
		os.Exit(2)
	}
	var problems []string
	var err error
	switch os.Args[1] {
	case "text":
		problems, err = checkText(os.Args[2:])
	case "version":
		problems, err = checkVersion(os.Args[2:])
	case "leaks":
		problems, err = checkLeaks(os.Args[2:])
	case "salt":
		problems, err = checkSalt(os.Args[2:])
	case "same":
		var slow bool
		problems, slow, err = checkSame(os.Args[2:])
		if err == nil && slow {
			report(problems)
			fmt.Println("check same: a repository or the review ran out of time: the machine was too slow to compare")
			os.Exit(3)
		}
	case "timeouts":
		var slow bool
		slow, err = anyTimeout(os.Args[2])
		if err == nil && slow {
			fmt.Println("check timeouts: a repository or the review ran out of time")
			os.Exit(3)
		}
	default:
		err = fmt.Errorf("unknown check %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "check:", err)
		os.Exit(2)
	}
	report(problems)
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Printf("check %s: OK\n", os.Args[1])
}

func report(problems []string) {
	for _, p := range problems {
		fmt.Println("FAIL:", p)
	}
}

var (
	tagRx    = regexp.MustCompile(`(?s)<[^>]*>`)
	blockRx  = regexp.MustCompile(`(?is)<(style|script|code|pre|template)\b[^>]*>.*?</(style|script|code|pre|template)>`)
	paramRx  = regexp.MustCompile(`\{([a-z_]+)\}`)
	spacesRx = regexp.MustCompile(`\s+`)
)

// readText is a file as the person reads it: an HTML page without markup and
// without the blocks that show the computer's own content.
func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(b)
	if strings.HasSuffix(strings.ToLower(path), ".html") {
		s = blockRx.ReplaceAllString(s, " ")
		s = tagRx.ReplaceAllString(s, " ")
		s = html.UnescapeString(s)
	}
	return spacesRx.ReplaceAllString(s, " "), nil
}

func loadCatalog(dir, lang string) (map[string]string, error) {
	b, err := os.ReadFile(dir + "/" + lang + ".json")
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	return m, json.Unmarshal(b, &m)
}

// checkText is checks a and b. a: no {name} of a catalog parameter is left
// in the text. b: with lang es, no fixed piece of four words or more of an
// English text that Spanish translates differently appears.
func checkText(args []string) ([]string, error) {
	fs := flag.NewFlagSet("text", flag.ExitOnError)
	dir := fs.String("catalog", "internal/catalog/messages", "the catalog folder")
	lang := fs.String("lang", "en", "the language the run was in")
	_ = fs.Parse(args)
	en, err := loadCatalog(*dir, "en")
	if err != nil {
		return nil, err
	}
	es, err := loadCatalog(*dir, "es")
	if err != nil {
		return nil, err
	}
	params := map[string]bool{}
	for _, cat := range []map[string]string{en, es} {
		for _, v := range cat {
			for _, m := range paramRx.FindAllStringSubmatch(v, -1) {
				params[m[1]] = true
			}
		}
	}
	var english []string
	if *lang == "es" {
		english = englishPieces(en, es)
	}
	var problems []string
	for _, f := range fs.Args() {
		text, err := readText(f)
		if err != nil {
			return nil, err
		}
		for _, m := range paramRx.FindAllStringSubmatch(text, -1) {
			if params[m[1]] {
				problems = append(problems, fmt.Sprintf("%s: unfilled %s", f, m[0]))
			}
		}
		for _, piece := range english {
			if strings.Contains(text, piece) {
				problems = append(problems, fmt.Sprintf("%s: English in a Spanish run: %q", f, piece))
			}
		}
	}
	return dedupe(problems), nil
}

// englishPieces are the fixed pieces (split at the parameters) of four words
// or more of every English text whose Spanish text differs.
func englishPieces(en, es map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for k, v := range en {
		if e, ok := es[k]; !ok || e == v {
			continue
		}
		for _, piece := range paramRx.Split(v, -1) {
			piece = strings.TrimSpace(spacesRx.ReplaceAllString(piece, " "))
			if len(strings.Fields(piece)) >= 4 && !seen[piece] {
				seen[piece] = true
				out = append(out, piece)
			}
		}
	}
	sort.Strings(out)
	return out
}

// checkVersion is check d: --version, the report's engine version and the
// HTML page all say the binary's version.
func checkVersion(args []string) ([]string, error) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	want := fs.String("want", "", "the version, without v")
	out := fs.String("version-output", "", "what --version printed")
	v2 := fs.String("v2", "", "report.v2.json")
	page := fs.String("html", "", "an HTML page of the report")
	_ = fs.Parse(args)
	var problems []string
	if b, err := os.ReadFile(*out); err != nil {
		return nil, err
	} else if got := strings.TrimSpace(string(b)); got != "rosmarops version "+*want {
		problems = append(problems, fmt.Sprintf("--version says %q, want %q", got, "rosmarops version "+*want))
	}
	var r struct {
		Provenance struct {
			Graph struct {
				EngineVersion string `json:"engine_version"`
			} `json:"graph"`
		} `json:"provenance"`
	}
	if err := readJSON(*v2, &r); err != nil {
		return nil, err
	}
	if r.Provenance.Graph.EngineVersion != *want {
		problems = append(problems, fmt.Sprintf("report.v2.json engine_version %q, want %q", r.Provenance.Graph.EngineVersion, *want))
	}
	if b, err := os.ReadFile(*page); err != nil {
		return nil, err
	} else if !strings.Contains(string(b), *want) {
		problems = append(problems, fmt.Sprintf("%s does not show version %s", *page, *want))
	}
	return problems, nil
}

// checkLeaks: no forbidden path (the runner's own folders) outside the test
// home, and not this machine's name. The report names where the program
// itself lives (the Claude Code hook command), so the installed program's
// path is allowed too: a person sees their own install path there.
func checkLeaks(args []string) ([]string, error) {
	fs := flag.NewFlagSet("leaks", flag.ExitOnError)
	home := fs.String("home", "", "the test home, which may appear")
	allow := fs.String("allow", "", "comma-separated paths that may appear too (the installed program)")
	forbid := fs.String("forbid", "", "comma-separated paths that must not appear outside the test home")
	_ = fs.Parse(args)
	host, _ := os.Hostname()
	var bad []string
	for _, p := range strings.Split(*forbid, ",") {
		if p = strings.TrimSpace(p); p != "" {
			bad = append(bad, p, strings.ReplaceAll(p, `\`, "/"), strings.ReplaceAll(p, "/", `\`))
		}
	}
	var problems []string
	for _, f := range fs.Args() {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		s := string(b)
		for _, a := range append(strings.Split(*allow, ","), *home) {
			if a = strings.TrimSpace(a); a == "" {
				continue
			}
			for _, h := range []string{a, strings.ReplaceAll(a, `\`, "/"), strings.ReplaceAll(a, `\`, `\\`)} {
				s = strings.ReplaceAll(s, h, "<home>")
			}
		}
		for _, p := range bad {
			if strings.Contains(s, p) {
				problems = append(problems, fmt.Sprintf("%s: a runner path outside the test home: %s", f, p))
			}
		}
		if short, _, _ := strings.Cut(host, "."); len(short) >= 4 && regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(short)+`\b`).MatchString(s) {
			problems = append(problems, fmt.Sprintf("%s: the machine's name %q", f, short))
		}
	}
	return dedupe(problems), nil
}

func checkSalt(args []string) ([]string, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("salt: two state files")
	}
	var a, b struct {
		LocalSalt string `json:"local_salt"`
	}
	if err := readJSON(args[0], &a); err != nil {
		return nil, err
	}
	if err := readJSON(args[1], &b); err != nil {
		return nil, err
	}
	if a.LocalSalt == "" || a.LocalSalt != b.LocalSalt {
		return []string{fmt.Sprintf("local_salt changed (%t before, %t after, equal %t): this computer would show up as a new one in the space",
			a.LocalSalt != "", b.LocalSalt != "", a.LocalSalt == b.LocalSalt)}, nil
	}
	return nil, nil
}

type reportV2 struct {
	Findings []struct {
		Rule string `json:"rule"`
	} `json:"findings"`
	Index struct {
		Machine struct {
			Value *int `json:"value"`
		} `json:"machine"`
	} `json:"index"`
	Coverage struct {
		ScanErrors []string `json:"scan_errors"`
	} `json:"coverage"`
	Legacy struct {
		Groups []struct {
			Category string `json:"category"`
		} `json:"groups"`
	} `json:"legacy"`
}

func anyTimeout(path string) (bool, error) {
	var r reportV2
	if err := readJSON(path, &r); err != nil {
		return false, err
	}
	for _, s := range r.Coverage.ScanErrors {
		if s == "global_timeout" {
			return true, nil
		}
	}
	for _, g := range r.Legacy.Groups {
		if g.Category == "scan_timeout" {
			return true, nil
		}
	}
	return false, nil
}

// checkSame compares two reviews of the same computer: the number of
// findings, the rules and the score are equal. slow is a time-out in either,
// which makes the comparison meaningless.
func checkSame(args []string) (problems []string, slow bool, err error) {
	if len(args) != 2 {
		return nil, false, fmt.Errorf("same: two report.v2.json files")
	}
	var r [2]reportV2
	for i, p := range args {
		if err := readJSON(p, &r[i]); err != nil {
			return nil, false, err
		}
		t, err := anyTimeout(p)
		if err != nil {
			return nil, false, err
		}
		slow = slow || t
	}
	if a, b := len(r[0].Findings), len(r[1].Findings); a != b {
		problems = append(problems, fmt.Sprintf("findings: %d, then %d", a, b))
	}
	rules := func(x reportV2) string {
		var out []string
		for _, f := range x.Findings {
			out = append(out, f.Rule)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	if rules(r[0]) != rules(r[1]) {
		problems = append(problems, "the rules of the findings differ")
	}
	score := func(x reportV2) string {
		if x.Index.Machine.Value == nil {
			return "none"
		}
		return fmt.Sprint(*x.Index.Machine.Value)
	}
	if score(r[0]) != score(r[1]) {
		problems = append(problems, fmt.Sprintf("score: %s, then %s", score(r[0]), score(r[1])))
	}
	return problems, slow, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
