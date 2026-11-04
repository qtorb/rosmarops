//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// shownTimeout is generous: the first WinForms launch on a cold CI runner can
// take many seconds to JIT and paint (an earlier run missed a 10 s deadline).
const shownTimeout = 30 * time.Second

// TestConnectWindowSelfTest runs the real WinForms window on windows-latest and
// proves the BV-147 reader. It is gated on ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN
// so the normal `go test ./...` on the verify-windows job never launches a GUI;
// the diagnostic-windows.yml connect-window job (manual, and on a pull request
// that touches connectwindow*.go) sets it.
//
// Three subtests, all passing:
//   - "runspace alive": the fix. With no state line sent, the UI Timer keeps
//     ticking (the interface thread is alive, the reader runs in its own
//     runspace); the number and the result then show promptly.
//   - "consolein_async freezes": BV-145's reader. It blocks the interface thread,
//     so almost no ticks appear — the alive check above would FAIL on it.
//   - "backgroundworker no number": v0.7.0's reader. Its worker fails off-runspace
//     and reports an error, so the number never reaches the window (the blank
//     window Albert saw) — the alive check's NUMLABEL step would FAIL on it.
//
// Together these prove, on Windows (the only place it is observable), that both
// broken readers fail the self-test and the fix passes it.
func TestConnectWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}
	t.Run("runspace alive", testWindowAlive)
	t.Run("consolein_async freezes", func(t *testing.T) { testWindowFreezes(t, "consolein_async") })
	t.Run("backgroundworker no number", func(t *testing.T) { testWindowNoNumber(t, "backgroundworker") })
	t.Run("geometry and colours", testWindowLook)
}

// argbOf is the .NET Color.ToArgb() value of an opaque #RRGGBB, as the signed
// int32 PowerShell prints.
func argbOf(rgb uint32) int { return int(int32(0xFF000000 | rgb)) }

// testWindowLook (BV-178) asserts the window's geometry and colours with the
// real (longest) Spanish texts, so a real-text layout bug is caught: both
// buttons visible, inside the client and not overlapping; the title and body
// fonts at 15 pt and 10.5 pt scaled to the runner DPI; each button sized to its
// text + 2×18 px (scaled) and 40 px tall (scaled); the rosemary title-bar icon;
// the cobalt primary (#2346B8, ink on hover/press).
func testWindowLook(t *testing.T) {
	ico, err := filepath.Abs(filepath.Join("..", "..", "assets", "windows", "rosmarops.ico"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("es") // the longest button/text, to stress the layout
	if err != nil {
		t.Fatal(err)
	}
	cfg := connectWindowConfigFrom(cat)
	cfg.Icon = "" // no exe icon in the test binary; force the rosmarops.ico fallback
	cfg.IconFallback = ico

	lines, send, stop := runSelfWindow(t, "")
	defer stop()
	if err := send(connectWindowInput(cfg) + "\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, shownTimeout, func(l string) bool { return l == "SHOWN" }) {
		t.Fatal("no SHOWN: the window did not paint")
	}
	kv := map[string]string{}
	deadline := time.After(5 * time.Second)
collect:
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				break collect
			}
			if !strings.Contains(l, "=") {
				continue
			}
			for _, tok := range strings.Fields(l) {
				if i := strings.IndexByte(tok, '='); i > 0 {
					kv[tok[:i]] = tok[i+1:]
				}
			}
			if _, done := kv["ICONROSEMARY"]; done {
				break collect
			}
		case <-deadline:
			break collect
		}
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	atof := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
	nums := func(key string) []int {
		var out []int
		for _, p := range strings.Split(kv[key], ",") {
			out = append(out, atoi(p))
		}
		return out
	}
	dpi := atoi(kv["DPI"])
	if dpi == 0 {
		t.Fatalf("no DPI emitted; collected: %v", kv)
	}
	s := float64(dpi) / 96
	sc := func(px int) int { return int(float64(px)*s + 0.5) }
	near := func(name string, got, want, tol int) {
		if got < want-tol || got > want+tol {
			t.Errorf("%s = %d, want ≈ %d (±%d, dpi %d)", name, got, want, tol, dpi)
		}
	}
	client := nums("CLIENT")
	cr := nums("CLIENTRECT")
	title := nums("TITLE")
	body := nums("BODY")
	yes := nums("YES")
	no := nums("NO")
	near("client width", client[0], sc(480), 3)
	if client[1] < sc(248)-3 {
		t.Errorf("client height %d < %d (the window must not shrink below the design)", client[1], sc(248))
	}
	near("title left", title[0], sc(28), 3)
	near("title top", title[1], sc(28), 3)
	near("body left", body[0], sc(28), 3)
	near("body top", body[1], title[1]+title[3]+sc(12), 4)

	// (1) Both buttons visible, inside the client, not overlapping.
	if kv["VIS"] != "True,True" {
		t.Errorf("both buttons must be visible, VIS=%q", kv["VIS"])
	}
	inside := func(name string, b []int) {
		if b[0] < 0 || b[1] < 0 || b[0]+b[2] > cr[0] || b[1]+b[3] > cr[1] {
			t.Errorf("%s button %v not inside client %v", name, b, cr)
		}
	}
	inside("primary", yes)
	inside("secondary", no)
	if no[0]+no[2] > yes[0] { // secondary fully left of primary
		t.Errorf("buttons overlap: secondary right %d > primary left %d", no[0]+no[2], yes[0])
	}
	near("gap", yes[0]-(no[0]+no[2]), sc(12), 3)
	near("yes height", yes[3], sc(40), 3)
	near("no height", no[3], sc(40), 3)
	near("yes top", yes[1], client[1]-sc(28)-sc(40), 3)
	near("no top", no[1], client[1]-sc(28)-sc(40), 3)
	near("yes right edge", yes[0]+yes[2], client[0]-sc(28), 3)

	// (2) Fonts scaled to the runner DPI, and each button = its text + 2×18 px.
	pt := strings.Split(kv["PT"], ",")
	if d := atof(pt[0]) - 15*s; d < -0.7 || d > 0.7 {
		t.Errorf("title font %s pt, want ≈ %.1f", pt[0], 15*s)
	}
	for i, name := range []string{"body", "primary", "secondary"} {
		if d := atof(pt[i+1]) - 10.5*s; d < -0.7 || d > 0.7 {
			t.Errorf("%s font %s pt, want ≈ %.2f", name, pt[i+1], 10.5*s)
		}
	}
	tw := nums("TEXTW") // yesTextW, noTextW, pad(=18×2 scaled)
	near("yes width", yes[2], tw[0]+tw[2], 2)
	near("no width", no[2], tw[1]+tw[2], 2)

	// (3) The rosemary icon, not the generic WinForms/PowerShell one.
	if kv["ICONROSEMARY"] != "True" {
		t.Errorf("title-bar icon is not the rosemary (ICONROSEMARY=%q)", kv["ICONROSEMARY"])
	}

	// Chrome and colours (4): cobalt primary, ink hover/press; secondary unchanged.
	if !strings.Contains(kv["BORDER"], "FixedSingle") {
		t.Errorf("FormBorderStyle = %q, want FixedSingle", kv["BORDER"])
	}
	if kv["MAX"] != "False" || kv["MIN"] != "False" || kv["TOP"] != "True" {
		t.Errorf("chrome: MAX=%s MIN=%s TOP=%s, want False/False/True", kv["MAX"], kv["MIN"], kv["TOP"])
	}
	white, ink, border, cobalt := argbOf(0xFFFFFF), argbOf(0x1B1A18), argbOf(0xC9C5BC), argbOf(0x2346B8)
	if atoi(kv["FORMBACK"]) != white {
		t.Errorf("form back = %s, want white (%d)", kv["FORMBACK"], white)
	}
	if atoi(kv["YESBACK"]) != cobalt || atoi(kv["YESFORE"]) != white || kv["YESBORDER"] != "0" || kv["YESFLAT"] != "Flat" ||
		atoi(kv["YESMOVER"]) != ink || atoi(kv["YESMDOWN"]) != ink {
		t.Errorf("primary button: back=%s fore=%s border=%s flat=%s mover=%s mdown=%s (want cobalt/white/0/Flat/ink/ink)",
			kv["YESBACK"], kv["YESFORE"], kv["YESBORDER"], kv["YESFLAT"], kv["YESMOVER"], kv["YESMDOWN"])
	}
	if atoi(kv["NOBACK"]) != white || atoi(kv["NOFORE"]) != ink || kv["NOBORDER"] != "1" || atoi(kv["NOBORDERCOLOR"]) != border || kv["NOFLAT"] != "Flat" {
		t.Errorf("secondary button: back=%s fore=%s border=%s borderColor=%s flat=%s", kv["NOBACK"], kv["NOFORE"], kv["NOBORDER"], kv["NOBORDERCOLOR"], kv["NOFLAT"])
	}
}

// testWindowAlive is the real proof of the fix: SHOWN, then at least 5 Timer
// ticks during a 2 s window in which the test sends NOTHING (the interface thread
// is free, not blocked on a read), then the number and the result show in under
// a second each once the lines arrive.
func testWindowAlive(t *testing.T) {
	lines, send, stop := runSelfWindow(t, "")
	defer stop()
	if err := send(selfConfig()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, shownTimeout, func(l string) bool { return l == "SHOWN" }) {
		t.Fatal("no SHOWN: the window did not paint")
	}
	ticks := countLines(lines, 2*time.Second, "TICK")
	t.Logf("runspace: %d TICK in 2 s with no input", ticks)
	if ticks < 5 {
		t.Fatalf("only %d TICK in 2 s with no input: the interface thread is not looping", ticks)
	}
	if err := send("number 42\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, 2*time.Second, func(l string) bool { return l == "NUMLABEL=42" }) {
		t.Fatal("NUMLABEL=42 not shown within 2 s of sending the number")
	}
	if err := send("done\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, 2*time.Second, func(l string) bool { return l == "RESULT=DONE_OK" }) {
		t.Fatal("RESULT=DONE_OK not shown within 2 s of sending done")
	}
}

// testWindowFreezes proves a reader that blocks the interface thread fails the
// self-test: SHOWN still arrives (it is emitted before the blocking read starts),
// but with no input sent the Timer cannot tick, so fewer than 5 TICK appear in
// 2 s — exactly the alive check testWindowAlive would fail on. Used for
// consolein_async. If it did NOT freeze it would wrongly pass, so this fails loud.
func testWindowFreezes(t *testing.T, readmode string) {
	lines, send, stop := runSelfWindow(t, readmode)
	defer stop()
	if err := send(selfConfig()); err != nil {
		t.Fatal(err)
	}
	sawShown := waitFor(lines, shownTimeout, func(l string) bool { return l == "SHOWN" })
	ticks := countLines(lines, 2*time.Second, "TICK")
	t.Logf("%s: shown=%v, %d TICK in 2 s with no input", readmode, sawShown, ticks)
	if ticks >= 5 {
		t.Fatalf("%s: %d TICK in 2 s — the interface thread did not freeze, so this broken reader would pass the self-test", readmode, ticks)
	}
}

// testWindowNoNumber proves the v0.7.0 BackgroundWorker reader fails the
// self-test differently: the interface thread stays alive (it ticks), but the
// worker's DoWork runs on a thread-pool thread with no runspace, fails, and
// reports the error via RunWorkerCompleted, so the number never reaches the
// window. We send the number and require that NUMLABEL never appears and that the
// worker error is surfaced (WORKERERROR=...). A working reader would show NUMLABEL.
func testWindowNoNumber(t *testing.T, readmode string) {
	lines, send, stop := runSelfWindow(t, readmode)
	defer stop()
	if err := send(selfConfig()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, shownTimeout, func(l string) bool { return l == "SHOWN" }) {
		t.Fatal("no SHOWN: the window did not paint")
	}
	if err := send("number 42\n"); err != nil {
		t.Fatal(err)
	}
	var sawNum, sawErr bool
	deadline := time.After(3 * time.Second)
loop:
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				break loop
			}
			switch {
			case l == "NUMLABEL=42":
				sawNum = true
			case strings.HasPrefix(l, "WORKERERROR="):
				sawErr = true
			}
		case <-deadline:
			break loop
		}
	}
	t.Logf("%s: sawNumber=%v sawWorkerError=%v", readmode, sawNum, sawErr)
	if sawNum {
		t.Fatalf("%s: NUMLABEL=42 appeared — the number reached the window, so this broken reader would pass the self-test", readmode)
	}
	if !sawErr {
		t.Fatalf("%s: no WORKERERROR surfaced: v0.7.0's silent worker failure was not reproduced", readmode)
	}
}

// runSelfWindow launches the window script in self-test mode with the given read
// mode (empty for the fix) and returns the stdout lines, a send func for stdin,
// and a stop func (idempotent).
func runSelfWindow(t *testing.T, readmode string) (<-chan string, func(string) error, func()) {
	t.Helper()
	ps1 := filepath.Join(t.TempDir(), "connectwindow.ps1")
	if err := os.WriteFile(ps1, []byte(connectWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
	cmd.Env = append(os.Environ(), "ROSMAROPS_CONNECT_WINDOW_SELFTEST=1", "ROSMAROPS_CONNECT_WINDOW_READMODE="+readmode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Surface any PowerShell error (the window script runs with -Stop) in the CI
	// log, so a blocked or failed run is diagnosable without another round.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l != "TICK" { // TICK is frequent; log the rest as a transcript.
				t.Logf("window[%q] stdout: %s", readmode, l)
			}
			lines <- l
		}
		close(lines)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	send := func(s string) error { _, e := stdin.Write([]byte(s)); return e }
	return lines, send, stop
}

// selfConfig is the base64 config line (with newline) the window reads first.
func selfConfig() string {
	cfg := connectWindowConfig{
		Title: "RosmarOps", Q: "Q", Text: "T", Yes: "Y", No: "N",
		Done: "DONE_OK", Expired: "EXP", Failed: "FAIL", OK: "OK",
	}
	return connectWindowInput(cfg) + "\n"
}

// waitFor reports whether a line satisfying pred arrives within d. Non-matching
// lines (e.g. TICK) are discarded.
func waitFor(lines <-chan string, d time.Duration, pred func(string) bool) bool {
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return false
			}
			if pred(l) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// countLines counts lines equal to want that arrive within d.
func countLines(lines <-chan string, d time.Duration, want string) int {
	n := 0
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return n
			}
			if l == want {
				n++
			}
		case <-deadline:
			return n
		}
	}
}
