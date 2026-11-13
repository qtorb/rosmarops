package agenthooks

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// StatuslineFile is the line each run leaves in the reports' data folder for
// `rosmarops statusline` (CTO-44 §4b): one line per language,
// "<lang>\t<checked at, RFC 3339>\t<text>". The text carries only counts, the
// score and the date: never a path, a file, a key, a server, a skill, an
// extension, a project or the user's name.
const StatuslineFile = "statusline.txt"

// datePlaceholder stays in the stored text: the date, or how old the check
// is, is filled when the line is shown.
const datePlaceholder = "{date}"

// StaleAfter is how old a check may be before the line says how long ago it
// was instead of its date (BV-226 §6: more than 8 days).
const StaleAfter = 8 * 24 * time.Hour

// gone marks a part of the line with nothing to tell (0 red, 0 yellow).
const gone = "\x00"

// StatuslineText is a finished run's line in cat's language, with {date}
// still to fill: «RosmarOps · {red} · {yellow} · {score}/100 · {date}», without
// the red part when there are none, nor the yellow part.
func StatuslineText(cat catalog.Catalog, red, yellow, score int) string {
	part := func(key string, n int) string {
		if n == 0 {
			return gone
		}
		return cat.Plural(key, n, map[string]string{"count": strconv.Itoa(n)})
	}
	t := cat.Text("statusline.line", map[string]string{
		"red": part("statusline.red", red), "yellow": part("statusline.yellow", yellow),
		"score": strconv.Itoa(score), "date": datePlaceholder,
	})
	for strings.Contains(t, gone) {
		switch {
		case strings.Contains(t, gone+" · "):
			t = strings.Replace(t, gone+" · ", "", 1)
		case strings.Contains(t, " · "+gone):
			t = strings.Replace(t, " · "+gone, "", 1)
		default:
			t = strings.Replace(t, gone, "", 1)
		}
	}
	return t
}

// WriteStatusline leaves the lines (language → text) in dataDir, atomically.
func WriteStatusline(dataDir string, checked time.Time, lines map[string]string) error {
	langs := make([]string, 0, len(lines))
	for l := range lines {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	var b bytes.Buffer
	for _, l := range langs {
		b.WriteString(l + "\t" + checked.UTC().Format(time.RFC3339) + "\t" + oneLine(lines[l]) + "\n")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dataDir, StatuslineFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func oneLine(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// ShowStatusline is the line to print from the file's contents raw: the one
// of lang (English when there is none), with {date} as cat's short date of
// the check in loc, or statusline.stale when the check is older than
// StaleAfter. "" when the file has no usable line.
func ShowStatusline(raw []byte, lang string, now time.Time, loc *time.Location, cat catalog.Catalog) string {
	found := map[string][2]string{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 3)
		if len(parts) == 3 {
			found[parts[0]] = [2]string{parts[1], parts[2]}
		}
	}
	line, ok := found[lang]
	if !ok {
		line, ok = found[catalog.DefaultLang]
	}
	if !ok {
		return ""
	}
	checked, err := time.Parse(time.RFC3339, line[0])
	if err != nil {
		return ""
	}
	text := line[1]
	if !strings.Contains(text, datePlaceholder) {
		return text
	}
	date := cat.DayLabel(checked.In(loc))
	if age := now.Sub(checked); age > StaleAfter {
		date = cat.Text("statusline.stale", map[string]string{"days": strconv.Itoa(int(age / (24 * time.Hour)))})
	}
	return strings.Replace(text, datePlaceholder, date, 1)
}
