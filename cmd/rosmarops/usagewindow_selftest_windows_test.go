//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// TestUsageWindowSelfTest runs the real usage statistics window (BV-286) on
// windows-latest. It is gated on ROSMAROPS_USAGE_WINDOW_SELFTEST_RUN so the
// normal `go test ./...` never opens a GUI; the diagnostic-windows.yml
// usage-window job sets it. With the real Spanish and English texts it checks
// the §4 layout, that Enter and the space bar do nothing and Esc closes as
// «later», and each button's answer and the thank-you.
func TestUsageWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_USAGE_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_USAGE_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}
	for _, lang := range []string{"es", "en"} {
		t.Run(lang+" layout and closing", func(t *testing.T) { testUsageLayout(t, lang) })
		t.Run(lang+" keys", func(t *testing.T) { testUsageKeys(t, lang) })
		t.Run(lang+" yes", func(t *testing.T) { testUsageYes(t, lang) })
		t.Run(lang+" no", func(t *testing.T) { testUsageNo(t, lang) })
	}
}

// runUsageWindow runs the script under the self-test with answer and returns
// every line it wrote, once it has ended.
func runUsageWindow(t *testing.T, lang, answer string) []string {
	t.Helper()
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	cfg := usageWindowConfigFrom(cat)
	cfg.Icon = ""
	if ico, err := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico")); err == nil {
		cfg.IconFallback = ico
	}
	ps1 := filepath.Join(t.TempDir(), "usagewindow.ps1")
	if err := os.WriteFile(ps1, []byte(usageWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
	cmd.Env = append(os.Environ(), "ROSMAROPS_USAGE_WINDOW_SELFTEST=1", "ROSMAROPS_USAGE_WINDOW_SELFTEST_ANSWER="+answer)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte(usageWindowInput(cfg) + "\n")); err != nil {
		t.Fatal(err)
	}
	var lines []string
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			t.Logf("usage window [%s %s]: %s", lang, answer, l)
			lines = append(lines, l)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("%s %s: the window did not end", lang, answer)
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return lines
}

// rect is a control's left, top, width, height and visibility, from a
// «NAME=l,t,w,h,visible» line.
type rect struct {
	l, t, w, h int
	visible    bool
}

func (r rect) right() int  { return r.l + r.w }
func (r rect) bottom() int { return r.t + r.h }

func layoutOf(t *testing.T, lines []string, prefix string) (map[string]rect, map[string]string) {
	t.Helper()
	rects, raw := map[string]rect{}, map[string]string{}
	for _, l := range lines {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(l, prefix), "=")
		if !ok {
			continue
		}
		raw[k] = v
		f := strings.Split(v, ",")
		if len(f) == 5 {
			n := make([]int, 4)
			for i := range n {
				n[i], _ = strconv.Atoi(f[i])
			}
			rects[k] = rect{n[0], n[1], n[2], n[3], f[4] == "True"}
		}
	}
	return rects, raw
}

func lastLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// §4 at 96 DPI (the runner): 480 wide, 28 margins, 424 of text, the title at
// (28, 28), why 12 px under it, the other paragraphs 10 px apart, the buttons
// 40 tall, 16 px under the last paragraph and 28 px from the bottom, sized to
// their text plus 18 each side, «Yes» 28 px from the right and «No» 12 px to
// its left; no default or cancel button, no button with the focus, the keys
// previewed and a 60-second thank-you timer. With no answer, it closes as
// «later».
func testUsageLayout(t *testing.T, lang string) {
	lines := runUsageWindow(t, lang, "")
	if len(lines) == 0 || lines[0] != "SHOWN" {
		t.Fatalf("first line %q, want SHOWN", strings.Join(lines, " | "))
	}
	if lastLine(lines) != "later" {
		t.Errorf("closed without an answer: last line %q, want later", lastLine(lines))
	}
	r, raw := layoutOf(t, lines, "")
	if raw["CLIENT"] == "" {
		t.Fatal("no layout lines")
	}
	cw, _ := strconv.Atoi(strings.Split(raw["CLIENT"], ",")[0])
	ch, _ := strconv.Atoi(strings.Split(raw["CLIENT"], ",")[1])
	if cw != 480 {
		t.Errorf("client width %d, want 480", cw)
	}
	if r["TITLE"].l != 28 || r["TITLE"].t != 28 {
		t.Errorf("title at (%d, %d), want (28, 28)", r["TITLE"].l, r["TITLE"].t)
	}
	for _, p := range []struct {
		name, above string
		gap         int
	}{{"WHY", "TITLE", 12}, {"BODY", "WHY", 10}, {"NEVER", "BODY", 10}, {"CHECK", "NEVER", 10}} {
		got := r[p.name]
		if got.l != 28 || got.w > 424 || got.t != r[p.above].bottom()+p.gap {
			t.Errorf("%s at (%d, %d) %d wide, want x 28, at most 424 wide, %d px under %s", p.name, got.l, got.t, got.w, p.gap, p.above)
		}
	}
	yes, no := r["YES"], r["NO"]
	if yes.h != 40 || no.h != 40 {
		t.Errorf("button heights %d and %d, want 40", yes.h, no.h)
	}
	if yes.t != r["CHECK"].bottom()+16 || yes.bottom() != ch-28 || no.t != yes.t {
		t.Errorf("buttons at y %d/%d (bottom %d of %d), want 16 under the last paragraph and 28 from the bottom", yes.t, no.t, yes.bottom(), ch)
	}
	if yes.right() != 480-28 || no.right() != yes.l-12 {
		t.Errorf("«yes» ends at %d (want 452), «no» ends at %d (want %d)", yes.right(), no.right(), yes.l-12)
	}
	tw := strings.Split(raw["TEXTW"], ",")
	if len(tw) == 4 {
		yt, _ := strconv.Atoi(tw[0])
		nt, _ := strconv.Atoi(tw[1])
		pad, _ := strconv.Atoi(tw[3])
		if yes.w != yt+pad || no.w != nt+pad || pad != 36 {
			t.Errorf("button widths %d and %d, want text %d and %d plus %d", yes.w, no.w, yt, nt, pad)
		}
	}
	if !strings.Contains(raw["PT"], ",True,Segoe UI Semibold,") {
		t.Errorf("«never» is not Segoe UI Semibold: %q", raw["PT"])
	}
	if raw["KEYS"] != "accept:True cancel:True preview:True focus:True timer:60000" {
		t.Errorf("keys %q: want no default or cancel button, keys previewed, no focus, a 60 s thank-you", raw["KEYS"])
	}
	if !strings.HasPrefix(raw["FORM"], "FixedSingle,True,CenterScreen,RosmarOps") {
		t.Errorf("form %q", raw["FORM"])
	}
	// White form, ink title and «never», muted paragraphs, the cobalt primary
	// with ink on hover, and the white secondary with ink text and a border.
	want := strings.Join([]string{
		strconv.Itoa(argbOf(0xFFFFFF)), strconv.Itoa(argbOf(0x1B1A18)), strconv.Itoa(argbOf(0x4F4C46)), strconv.Itoa(argbOf(0x1B1A18)),
		strconv.Itoa(argbOf(0x2346B8)), strconv.Itoa(argbOf(0xFFFFFF)), strconv.Itoa(argbOf(0x1B1A18)),
		strconv.Itoa(argbOf(0xFFFFFF)), strconv.Itoa(argbOf(0x1B1A18)), "1",
	}, ",")
	if raw["COLORS"] != want {
		t.Errorf("colours %q, want %q", raw["COLORS"], want)
	}
}

// Enter and the space bar, with no button chosen, do nothing: the window stays
// open with no answer; then Esc closes it like the X («later»).
func testUsageKeys(t *testing.T, lang string) {
	lines := runUsageWindow(t, lang, "keys")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "AFTERKEYS=") {
			found = true
			if l != "AFTERKEYS=open:True result:" {
				t.Errorf("after Enter and the space bar: %q, want the window open with no answer", l)
			}
		}
		if l == "yes" || l == "no" {
			t.Errorf("a key answered %q", l)
		}
	}
	if !found {
		t.Fatal("the keys step did not run")
	}
	if lastLine(lines) != "later" {
		t.Errorf("Esc: last line %q, want later", lastLine(lines))
	}
}

// «Yes, help» writes yes and shows the thank-you at (28, 28) with one primary
// OK button on the right, 28 px from the edge; the question is gone.
func testUsageYes(t *testing.T, lang string) {
	lines := runUsageWindow(t, lang, "yes")
	yesAt := -1
	for i, l := range lines {
		if l == "yes" {
			yesAt = i
		}
		if l == "later" || l == "no" {
			t.Errorf("after yes the window wrote %q", l)
		}
	}
	if yesAt < 0 {
		t.Fatalf("no yes line: %v", lines)
	}
	r, raw := layoutOf(t, lines, "T")
	th, ok := r["THANKS"], r["OK"]
	if !th.visible || th.l != 28 || th.t != 28 {
		t.Errorf("thank-you %+v, want visible at (28, 28)", th)
	}
	if !ok.visible || ok.right() != 480-28 || ok.h != 40 {
		t.Errorf("OK %+v, want a visible 40 px button ending at 452", ok)
	}
	for _, k := range []string{"TITLE", "WHY", "BODY", "NEVER", "CHECK", "YES", "NO"} {
		if r[k].visible {
			t.Errorf("%s still visible on the thank-you", k)
		}
	}
	if !strings.HasSuffix(raw["KEYS"], "timer:60000") {
		t.Errorf("thank-you timer %q, want 60000 ms", raw["KEYS"])
	}
}

// «No, thanks» writes no and closes.
func testUsageNo(t *testing.T, lang string) {
	lines := runUsageWindow(t, lang, "no")
	if lastLine(lines) != "no" {
		t.Errorf("last line %q, want no", lastLine(lines))
	}
}
