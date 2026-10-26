package trust_test

// Trust test (decision 2026-10-26, integridad y revisión semanal, J7;
// docs/trust-model.md claim 10): a scheduled review's notification says an
// agent's name, its score and the counts, nothing else. Two machines with
// the same agent, score and counts give the same text; no repository,
// service, path, host or secret enters it. Its XML carries that text and,
// apart from it, only the report's file URI as the click's target.

import (
	"encoding/base64"
	"encoding/xml"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/notify"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

func TestNoIdentityInNotification(t *testing.T) {
	day := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	seg := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	jwt := seg(`{"alg":"HS256","typ":"JWT"}`) + "." + seg(`{"iss":"supabase","ref":"alicealicealicealice","role":"service_role"}`) + ".YWxpY2Utc2lnbmF0dXJlLWZpbGxlcg"
	awsKey := "AKIABOBBOBBOBBOB1234"
	saltA, saltB := []byte("alice-salt-0123456789abcdef-0001"), []byte("bob-salt-9876543210fedcba-000002")
	homeA, repoA := "/home/alice", "/home/alice/work/webapp"
	homeB, repoB := `/srv/bob`, `/srv/bob/code/payments`
	vA := scenarioV2(t, saltA, homeA, repoA, []platform.CredentialEntry{credEntry(repoA+"/dashboard/.env.local", 4, "SUPABASE_SERVICE_ROLE_KEY", jwt)}, day)
	vB := scenarioV2(t, saltB, homeB, repoB, []platform.CredentialEntry{credEntry(repoB+"/.env", 2, "AWS_ACCESS_KEY_ID", awsKey)}, day)
	forbidden := []string{
		homeA, repoA, homeB, repoB, "alice", "bob", "webapp", "payments", "dashboard", ".env",
		"supabase", "aws", "service_role", graph.HostID(saltA), graph.HostID(saltB), "host_",
		jwt, awsKey, awsKey[4 : len(awsKey)-4], strings.Split(jwt, ".")[1],
	}
	// The only shape: «RosmarOps: … · <agent> <score>/100 · …», with the
	// agent's name, a score and at most a count.
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		textA, okA := report.NotificationText(vA, cat)
		textB, okB := report.NotificationText(vB, cat)
		if !okA || !okB {
			t.Fatalf("%s: no notification: test would be vacuous", lang)
		}
		if textA != textB {
			t.Errorf("%s: the two machines' notifications differ:\n%s\n%s", lang, textA, textB)
		}
		if !strings.HasPrefix(textA, "RosmarOps: ") || !regexp.MustCompile(`Claude Code \d{1,3}/100`).MatchString(textA) {
			t.Errorf("%s: notification %q", lang, textA)
		}
		// No path separator but the score's «/100».
		if strings.ContainsAny(strings.ReplaceAll(textA, "/100", ""), `/\`) {
			t.Errorf("%s: the notification has a path separator: %s", lang, textA)
		}
		for _, f := range forbidden {
			if strings.Contains(strings.ToLower(textA), strings.ToLower(f)) {
				t.Errorf("%s: the notification contains %q: %s", lang, f, textA)
			}
		}
		// Every variant: only {agent}, {score} and {count}.
		for _, k := range []string{"notify.done.same", "notify.done.new.one", "notify.done.new.other", "notify.done.changed", "notify.done.not_comparable", "notify.aborted"} {
			left := regexp.MustCompile(`\{(agent|score|count)\}`).ReplaceAllString(cat.Text(k, nil), "")
			if strings.ContainsAny(left, "{}") || !strings.HasPrefix(cat.Text(k, nil), "RosmarOps:") {
				t.Errorf("%s: %s has another parameter or does not start with «RosmarOps:»: %q", lang, k, cat.Text(k, nil))
			}
		}
		// The XML: one text node, the notification's; the path only as the
		// click's file URI.
		x := notify.ToastXML(textA, notify.FileURI(homeA+"/.rosmarops/reports/report.html"))
		var toast struct {
			Launch string   `xml:"launch,attr"`
			Texts  []string `xml:"visual>binding>text"`
		}
		if err := xml.Unmarshal([]byte(x), &toast); err != nil {
			t.Fatal(err)
		}
		if len(toast.Texts) != 1 || toast.Texts[0] != textA || !strings.HasPrefix(toast.Launch, "file:///") {
			t.Errorf("%s: toast %+v", lang, toast)
		}
	}
}
