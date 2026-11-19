//go:build windows

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/winpath"
)

// dismissCard is one finding as the window draws it (BV-184 §4). It carries no
// secret and no graph detail; only what the person reads.
type dismissCard struct {
	ID    string `json:"id"`
	Sev   string `json:"sev"`   // "red" or "yellow"
	Title string `json:"title"` // the finding's title
	What  string `json:"what"`  // the report's «what it means»
	When  string `json:"when"`  // findings.when.<rule>
	New   bool   `json:"new"`   // a new finding
	Since string `json:"since"` // the localized «open since» sentence; empty when New
	Agent string `json:"agent"` // the agent, or empty for a machine finding
	Group string `json:"group"` // "new", "rest", "accepted" or "dismissed"
	// State is the button chosen when the window opens (BV-236 §5):
	// "accepted" («Lo asumo»), "dismissed" («Descartar») or "" (pending).
	State string `json:"state"`
}

// dismissWindowConfig is everything the window draws, all text from the
// catalog, passed as base64 JSON on the first stdin line (BV-184 §4).
type dismissWindowConfig struct {
	Title        string `json:"title"`
	Q            string `json:"q"`
	Text         string `json:"text"`
	GroupNew     string `json:"group_new"`
	GroupRest    string `json:"group_rest"`
	GroupAccept  string `json:"group_accepted"`
	GroupDismiss string `json:"group_dismissed"`
	Accept       string `json:"accept"`  // «Lo asumo»
	Dismiss      string `json:"dismiss"` // «Descartar»
	Red          string `json:"red"`
	Yellow       string `json:"yellow"`
	SubNew       string `json:"sub_new"`
	WhatLabel    string `json:"what_label"`
	WhenLabel    string `json:"when_label"`
	// W3: «{accepted} · {dismissed}», each with its form of 1, or TallyNone.
	Tally        string        `json:"tally"`
	TallyAOne    string        `json:"tally_a_one"`
	TallyAOther  string        `json:"tally_a_other"`
	TallyDOne    string        `json:"tally_d_one"`
	TallyDOther  string        `json:"tally_d_other"`
	TallyNone    string        `json:"tally_none"`
	No           string        `json:"no"`
	Save         string        `json:"save"`
	Cards        []dismissCard `json:"cards"`
	Icon         string        `json:"icon"`
	IconFallback string        `json:"iconfallback"`
	// EncProbe is a fixed string with non-ASCII characters used only by the
	// self-test to check the UTF-8 pipeline end to end.
	EncProbe string `json:"enc_probe"`
}

// dismissEncProbe is the fixed probe string (a middle dot, an á and an ñ).
const dismissEncProbe = "·áñ"

// dismissWindowConfigFrom builds the window config from the finding cards and
// the catalog. The per-card «open since» subtitle is resolved here (with the
// localized long date already in the card), so the window only draws strings.
func dismissWindowConfigFrom(cat catalog.Catalog, cards []report.FindingCard) dismissWindowConfig {
	icon, _ := os.Executable()
	out := dismissWindowConfig{
		Title:        "RosmarOps",
		Q:            cat.Text("findings.q.accept", nil),    // W1
		Text:         cat.Text("findings.text.accept", nil), // W2
		GroupNew:     cat.Text("findings.group.new", nil),
		GroupRest:    cat.Text("findings.group.rest", nil),
		GroupAccept:  cat.Text("findings.group.accepted", nil),
		GroupDismiss: cat.Text("findings.group.dismissed", nil),
		Accept:       cat.Text("findings.accept", nil),
		Dismiss:      cat.Text("findings.dismiss", nil),
		Red:          cat.Text("findings.red", nil),
		Yellow:       cat.Text("findings.yellow", nil),
		SubNew:       cat.Text("findings.sub.new", nil),
		WhatLabel:    cat.Text("findings.what", nil),
		WhenLabel:    cat.Text("findings.when", nil),
		Tally:        cat.Text("findings.tally", nil),
		TallyAOne:    cat.Text("findings.tally.accepted.one", nil),
		TallyAOther:  cat.Text("findings.tally.accepted.other", nil),
		TallyDOne:    cat.Text("findings.tally.dismissed.one", nil),
		TallyDOther:  cat.Text("findings.tally.dismissed.other", nil),
		TallyNone:    cat.Text("findings.tally.none", nil),
		No:           cat.Text("findings.no", nil),
		Save:         cat.Text("findings.save", nil),
		Icon:         icon,
		EncProbe:     dismissEncProbe,
	}
	for _, c := range cards {
		sub := ""
		if !c.New && c.Since != "" {
			sub = cat.Text("findings.sub.since", map[string]string{"date": c.Since})
		}
		out.Cards = append(out.Cards, dismissCard{
			ID: c.ID, Sev: c.Severity, Title: c.Title, What: c.WhatItMeans, When: c.WhenApplies,
			New: c.New, Since: sub, Agent: c.Agent, Group: c.Group,
			State: cardState(c.Group),
		})
	}
	return out
}

// cardState is the button a card's group starts chosen: «Lo asumo» for the
// accepted group, «Descartar» for the dismissed one, none (pending) otherwise.
func cardState(group string) string {
	switch group {
	case report.CardGroupAccepted:
		return "accepted"
	case report.CardGroupDismissed:
		return "dismissed"
	}
	return ""
}

func dismissWindowInput(c dismissWindowConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

// driveDismissWindow reads the window's answer (BV-236 §5): "save", then a
// line of the space-separated ids marked «Descartar», then a line of those
// marked «Lo asumo» (either possibly empty); or "cancel" (or a closed stream)
// for «Not now». It returns both lists and whether saved.
func driveDismissWindow(out io.Reader) (findingChoice, bool) {
	r := bufio.NewReader(out)
	first, _ := r.ReadString('\n')
	noteSelfTestShown(first, "findings", findingsSelfTestEnv)
	if strings.TrimRight(first, "\r\n") != "save" {
		return findingChoice{}, false
	}
	dismissed, _ := r.ReadString('\n')
	accepted, _ := r.ReadString('\n')
	return findingChoice{Dismissed: strings.Fields(dismissed), Accepted: strings.Fields(accepted)}, true
}

// showDismiss shows the findings window on Windows and returns both lists the
// person left chosen and whether they saved. Any failure to show the window is
// logged and treated as «Not now».
func showDismiss(cards []report.FindingCard, cat catalog.Catalog) (findingChoice, bool) {
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		recordWindowFailure()
		return findingChoice{}, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		recordWindowFailure()
		return findingChoice{}, false
	}
	if err := cmd.Start(); err != nil {
		slog.Info("dismiss window not shown", "err", err)
		recordWindowFailure()
		return findingChoice{}, false
	}
	if err := writeWindowScript(stdin, dismissWindowScript); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		recordWindowFailure()
		return findingChoice{}, false
	}
	if _, err := stdin.Write([]byte(dismissWindowInput(dismissWindowConfigFrom(cat, cards)) + "\n")); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return findingChoice{}, false
	}
	type res struct {
		ids   findingChoice
		saved bool
	}
	done := make(chan res, 1)
	go func() {
		ids, saved := driveDismissWindow(stdout)
		done <- res{ids, saved}
	}()
	var out res
	select {
	case out = <-done:
	case <-time.After(15 * time.Minute):
		slog.Info("dismiss window: no answer in 15 minutes; closing as «Not now»")
		_ = cmd.Process.Kill()
		<-done
	}
	_ = stdin.Close()
	waitWindowExit(cmd, "findings", windowExitLimit)
	return out.ids, out.saved
}
