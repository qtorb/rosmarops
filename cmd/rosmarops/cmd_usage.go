package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
	"github.com/qtorb/rosmarops/internal/usage"
	"github.com/qtorb/rosmarops/internal/winpkg"
)

// Usage statistics, program side. The pattern is the problem
// reports': the state records the person's answer, spacenet.SendUsage sends
// only under the consent read from the state, every attempt is noted in
// rosmarops-sends.log, and the last package sent or tried is kept as it is in
// data/rosmarops-usage-last.json.

// sendUsageFn is the network seam for the usage send: production is
// spacenet.SendUsage (host-pinned to app.rosmarops.com:443, POST /api/v1/usage,
// 3 seconds, no retries); tests replace it.
var sendUsageFn = spacenet.SendUsage

// usageNow is the clock of the usage answer and package; tests replace it.
var usageNow = time.Now

// usageTextVersion is the version of the question's text the program shows:
// an answer to an older one is asked again. A variable for the tests.
var usageTextVersion = usage.TextVersion

// lastUsageInput is this run's review, as the usage package needs it: set by
// runWith once the report is written, read by the after-report step. nil when
// no review finished this run.
var lastUsageInput *usage.Input

// newUsageCmd is `rosmarops usage [on|off]`.
func newUsageCmd() *cli.Command {
	return &cli.Command{
		Name: "usage", Short: "usage.help", Args: cli.AnyArgs,
		Flags:  []cli.Flag{{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"}},
		Before: integrityCheck,
		Run:    runUsage,
	}
}

// runUsage turns usage statistics on or off, or prints whether they are on.
func runUsage(inv cli.Invocation) error {
	cat, err := catalog.Load(inv.Flags["lang"])
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if len(inv.Args) > 1 {
		return fmt.Errorf("usage: one argument at most: on or off")
	}
	if len(inv.Args) == 1 {
		switch inv.Args[0] {
		case "on", "off":
			if err := decideUsage(&st, inv.Args[0] == "on", usageNow()); err != nil {
				return err
			}
			if err := state.SaveState(stPath, st); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
		default:
			return fmt.Errorf("usage: unknown argument %q: on or off", inv.Args[0])
		}
	}
	fmt.Println(cat.Text(usageStateKey(st.Usage), nil))
	return nil
}

// usageStateKey is the line that says whether usage statistics are on.
func usageStateKey(u *state.Usage) string {
	switch {
	case u == nil:
		return "usage.state.undecided"
	case u.Enabled:
		return "usage.state.on"
	}
	return "usage.state.off"
}

// decideUsage records the person's answer. Turning them on from off (or from
// no answer) draws a new install id; the text version is the current one.
func decideUsage(st *state.State, on bool, now time.Time) error {
	prev := st.Usage
	u := state.Usage{Enabled: on, DecidedAt: now.UTC().Format(time.RFC3339), TextVersion: usageTextVersion}
	if prev != nil && prev.InstallID != "" && (prev.Enabled || !on) {
		u.InstallID = prev.InstallID
	} else {
		id, err := usage.NewInstallID()
		if err != nil {
			return err
		}
		u.InstallID = id
	}
	st.Usage = &u
	return nil
}

// usageUndecided reports whether the person must be asked: no answer yet, or
// an answer to an older text.
func usageUndecided(u *state.Usage) bool {
	return u == nil || u.TextVersion < usageTextVersion
}

// askUsage asks the question in a console: Enter, s, S, y or Y is yes;
// n or N is no; anything else repeats the question once and then counts as no.
func askUsage(in io.Reader, out io.Writer, cat catalog.Catalog) bool {
	fmt.Fprintln(out)
	fmt.Fprintln(out, cat.Text("usage.ask.title", nil))
	fmt.Fprintln(out, cat.Text("usage.ask.body", nil))
	fmt.Fprintln(out, cat.Text("usage.ask.never", nil))
	fmt.Fprintln(out, cat.Text("usage.ask.off", nil))
	r := bufio.NewReader(in)
	for try := 0; try < 2; try++ {
		fmt.Fprint(out, cat.Text("usage.ask.prompt", nil)+" ")
		line, err := r.ReadString('\n')
		switch strings.TrimSpace(line) {
		case "", "s", "S", "y", "Y":
			if err != nil && line == "" {
				return false // the input ended: nobody answered
			}
			return true
		case "n", "N":
			return false
		}
		if err != nil {
			return false
		}
	}
	return false
}

// afterReportUsage is the usage step after a review's report:
// with someone at a console and no answer to the current text, it asks and
// records the answer; then, with usage statistics on, it sends this review's
// package. It never fails the run and never changes the report.
func afterReportUsage(reportDir, lang string, canAskNow bool) {
	stPath, err := state.StatePath()
	if err != nil {
		return
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return
	}
	if usageUndecided(st.Usage) {
		if !canAskNow {
			return
		}
		cat, err := catalog.Load(lang)
		if err != nil {
			return
		}
		if err := decideUsage(&st, askUsage(runInput, os.Stdout, cat), usageNow()); err != nil {
			slog.Warn("usage: answer", "err", err)
			return
		}
		if err := state.SaveState(stPath, st); err != nil {
			slog.Warn("usage: save answer", "err", err)
			return
		}
		fmt.Println(cat.Text(usageStateKey(st.Usage), nil))
	}
	if st.Usage == nil || !st.Usage.Enabled || lastUsageInput == nil {
		return
	}
	consent, err := spacenet.UsageConsentFromState(stPath)
	if err != nil {
		return
	}
	in := *lastUsageInput
	in.InstallID, in.Now = st.Usage.InstallID, usageNow()
	sendUsage(context.Background(), consent, report.DataDir(reportDir), in)
}

// sendUsage builds this review's package, keeps it in rosmarops-usage-last.json
// and sends it once, noting the result in the sends log. A package that does
// not build is not sent: the log says «failed».
func sendUsage(ctx context.Context, consent spacenet.UsageConsent, dataDir string, in usage.Input) string {
	logPath := filepath.Join(dataDir, SendsLogName)
	p, err := usage.Build(in)
	if err != nil {
		slog.Warn("usage: package not built; not sent", "err", err)
		appendSendLog(logPath, in.Now, usage.SendsLabel, 0, sendFailed)
		return sendFailed
	}
	body, err := usage.Marshal(p)
	if err != nil {
		appendSendLog(logPath, in.Now, usage.SendsLabel, 0, sendFailed)
		return sendFailed
	}
	writeUsageLast(dataDir, body)
	status, sendErr := sendUsageFn(ctx, consent, body)
	result := usageResultFor(status, sendErr)
	appendSendLog(logPath, in.Now, usage.SendsLabel, len(body), result)
	return result
}

// writeUsageLast keeps the package as it was sent (or tried), overwriting the
// previous one, in the reports' data folder (invariant 2).
func writeUsageLast(dataDir string, body []byte) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		slog.Warn("usage: data dir", "err", err)
		return
	}
	path := filepath.Join(dataDir, usage.LastFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		slog.Warn("usage: write last package", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("usage: write last package", "err", err)
	}
}

// usageResultFor maps the endpoint's status (0 on a network error) to a
// sends-log result. 204 is the app's success; nothing is retried.
func usageResultFor(status int, err error) string {
	switch {
	case err != nil:
		return sendDeferred
	case status == 204 || status == 200 || status == 201:
		return sendSent
	case status == 400:
		return sendBadRequest
	case status == 413:
		return sendTooLarge
	case status == 429 || status >= 500:
		return sendDeferred
	}
	return sendError
}

// usageInputOf is what a finished review gives the usage package; the install
// id and the day are added when it is sent.
func usageInputOf(v2 report.V2, spaceConnected bool, settings *platform.ClaudeSettingsScan, exe string) usage.Input {
	in := usage.Input{
		Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Channel: usageChannel(exe), Status: v2.Status, Findings: map[string]int{},
		SpaceConnected: spaceConnected, PluginInstalled: ownPluginEnabled(settings),
	}
	for _, o := range v2.AgentsOverview {
		in.Agents = append(in.Agents, o.Product)
	}
	if s := v2.Index.Machine.Value; s != nil {
		n := *s
		in.Score = &n
	}
	if l := v2.Index.Machine.Letter; l != nil && l.Class != nil {
		in.Class = *l.Class
	}
	for _, f := range v2.Findings {
		in.Findings[f.Rule]++
	}
	return in
}

// usageChannel is how this copy was installed: the Microsoft Store's
// own package (winpkg.IdentityIsOwn, the same check the integrity uses), a Homebrew keg (brewCellar), or other.
func usageChannel(exe string) string {
	if runtime.GOOS == "windows" {
		if real, err := currentExe(); err == nil && winpkg.IdentityIsOwn(currentPackage(), real, os.Getenv("LOCALAPPDATA")) {
			return usage.ChannelStore
		}
		return usage.ChannelOther
	}
	if real, err := currentExe(); err == nil && brewCellar.MatchString(real) {
		return usage.ChannelHomebrew
	}
	if brewCellar.MatchString(exe) {
		return usage.ChannelHomebrew
	}
	return usage.ChannelOther
}

// ownPluginEnabled reports RosmarOps's own Claude Code plugin enabled in the
// user or managed settings (OwnPluginEnabled).
func ownPluginEnabled(s *platform.ClaudeSettingsScan) bool {
	if s == nil {
		return false
	}
	files := append([]platform.ClaudeSettingsFile{}, s.Files...)
	for _, m := range s.Managed {
		files = append(files, m.Files...)
	}
	for _, f := range files {
		if f.OwnPluginEnabled {
			return true
		}
	}
	return false
}
