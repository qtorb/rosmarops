package notify_test

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/notify"
)

// hostile is text a report could carry into a notification (an agent's
// name, a path): quotes, markup, PowerShell subexpressions, new lines.
var hostile = []string{
	`O'Brien`, `say "hi"`, `<text>x</text>`, `a & b`, `$(Remove-Item C:\)`, "`$env:USERNAME`", "line\nbreak", `]]><toast>`, `%USERPROFILE%`,
}

// J7: the command is constant and nothing of the text or the path enters
// it; the XML carries the text and the URI as they are, escaped.
func TestHostileInputStaysData(t *testing.T) {
	args := notify.Args()
	if !reflect.DeepEqual(args, []string{"-NonInteractive", "-NoProfile", "-Command", notify.Script}) {
		t.Fatalf("args %q", args)
	}
	for _, h := range hostile {
		uri := notify.FileURI(`C:\Users\` + h + `\.rosmarops\reports\report.html`)
		x := notify.ToastXML("RosmarOps: "+h, uri)
		if !reflect.DeepEqual(notify.Args(), args) || strings.Contains(notify.Script, h) {
			t.Errorf("%q: the command depends on the text", h)
		}
		var toast struct {
			XMLName        xml.Name
			ActivationType string   `xml:"activationType,attr"`
			Launch         string   `xml:"launch,attr"`
			Texts          []string `xml:"visual>binding>text"`
		}
		if err := xml.Unmarshal([]byte(x), &toast); err != nil {
			t.Fatalf("%q: %v\n%s", h, err, x)
		}
		if toast.XMLName.Local != "toast" || toast.ActivationType != "protocol" || toast.Launch != uri ||
			len(toast.Texts) != 1 || toast.Texts[0] != "RosmarOps: "+h {
			t.Errorf("%q: toast %+v", h, toast)
		}
		if strings.Count(x, "<text>") != 1 || strings.Count(x, "<toast") != 1 {
			t.Errorf("%q: markup injected: %s", h, x)
		}
		if strings.ContainsAny(strings.TrimPrefix(uri, "file:///C:"), " '\"<>&$\n") {
			t.Errorf("%q: unencoded character in %s", h, uri)
		}
	}
}

func TestFileURI(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\Ana Pérez\.rosmarops\reports\report.html`: "file:///C:/Users/Ana%20P%C3%A9rez/.rosmarops/reports/report.html",
		`D:\O'Brien\r#1?.html`:                              "file:///D:/O%27Brien/r%231%3F.html",
		`/home/ana/.rosmarops/reports/report.html`:          "file:///home/ana/.rosmarops/reports/report.html",
	} {
		if got := notify.FileURI(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// J7: the four settings that keep the notification from showing, and
// nothing for Enabled or an answer not known.
func TestUnavailable(t *testing.T) {
	for setting, want := range map[string]string{
		"DisabledForApplication": "disabled_for_application", "DisabledForUser": "disabled_for_user",
		"DisabledByGroupPolicy": "disabled_by_group_policy", "DisabledByManifest\r\n": "disabled_by_manifest",
	} {
		if got, ok := notify.Unavailable(setting); !ok || got != want {
			t.Errorf("%q: %q %v", setting, got, ok)
		}
	}
	for _, s := range []string{notify.Enabled, "", "Unknown"} {
		if _, ok := notify.Unavailable(s); ok {
			t.Errorf("%q: unavailable", s)
		}
	}
	if len(notify.Reasons) != 4 {
		t.Errorf("reasons %v", notify.Reasons)
	}
}
