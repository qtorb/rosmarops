package trust_test

// Trust test (decision índice de postura agéntica, §6, I11): the share card
// comes only from the closed list (index name, product name, score, band,
// counts, day, method version). Two machines with the same score, band,
// counts, day and version give the same bytes; the card holds nothing of
// the machine; its SVG is inert.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// cardOf extracts the card's SVG and text from the HTML report.
func cardOf(t *testing.T, page string) (svg, text string) {
	t.Helper()
	s := regexp.MustCompile(`(?s)<template id="card-svg">(.*?)</template>`).FindStringSubmatch(page)
	x := regexp.MustCompile(`(?s)<template id="card-txt">(.*?)</template>`).FindStringSubmatch(page)
	if s == nil || x == nil {
		t.Fatal("no card in the report")
	}
	return s[1], x[1]
}

func TestNoIdentityInCard(t *testing.T) {
	day := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	seg := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	jwt := seg(`{"alg":"HS256","typ":"JWT"}`) + "." + seg(`{"iss":"supabase","ref":"alicealicealicealice","role":"service_role"}`) + ".YWxpY2Utc2lnbmF0dXJlLWZpbGxlcg"
	awsKey := "AKIABOBBOBBOBBOB1234"

	// Two machines: other salt, host_id, home, repository, service and
	// secret; the same score (one red high credential from any project:
	// 35), band, counts, day and index version.
	saltA, saltB := []byte("alice-salt-0123456789abcdef-0001"), []byte("bob-salt-9876543210fedcba-000002")
	homeA, repoA := "/home/alice", "/home/alice/work/webapp"
	homeB, repoB := `/srv/bob`, `/srv/bob/code/payments`
	vA := scenarioV2(t, saltA, homeA, repoA, []platform.CredentialEntry{credEntry(repoA+"/dashboard/.env.local", 4, "SUPABASE_SERVICE_ROLE_KEY", jwt)}, day)
	vB := scenarioV2(t, saltB, homeB, repoB, []platform.CredentialEntry{credEntry(repoB+"/.env", 2, "AWS_ACCESS_KEY_ID", awsKey)}, day)
	for _, v := range []report.V2{vA, vB} {
		a := v.Index.Agents
		if len(a) != 1 || a[0].Value == nil || *a[0].Value != 35 || a[0].Red != 1 || a[0].Yellow != 0 {
			t.Fatalf("scenario score %+v — the two machines must score the same", a)
		}
	}

	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		svgA, textA := cardOf(t, report.RenderHTML(vA, cat, homeA, time.UTC))
		svgB, textB := cardOf(t, report.RenderHTML(vB, cat, homeB, time.UTC))
		// 1. Byte for byte the same.
		if svgA != svgB || textA != textB {
			t.Errorf("%s: the two machines' cards differ:\n%s\n%s\n%s\n%s", lang, svgA, svgB, textA, textB)
		}
		// 2. Nothing of either machine.
		forbidden := []string{
			homeA, repoA, homeB, repoB, "alice", "bob", "webapp", "payments", "dashboard", ".env",
			"supabase", "Supabase", "aws", "AWS", "service_role",
			graph.HostID(saltA), graph.HostID(saltB), "hmac-sha256", "host_",
			jwt, awsKey, awsKey[4 : len(awsKey)-4], strings.Split(jwt, ".")[1],
		}
		for _, card := range []string{svgA, textA, svgB, textB} {
			for _, f := range forbidden {
				if strings.Contains(strings.ToLower(card), strings.ToLower(f)) {
					t.Errorf("%s: the card contains %q", lang, f)
				}
			}
		}
		// 3. An inert SVG. The one exception (BV-41): the exact namespace
		// attribute, at most once, on the root <svg> only; it is removed
		// before looking for "http".
		if n := strings.Count(svgA, index.SVGNamespaceAttr); n > 1 || n == 1 && !strings.HasPrefix(svgA, "<svg "+index.SVGNamespaceAttr+" ") {
			t.Errorf("%s: the namespace attribute is not only on the root <svg> (%d)", lang, n)
		}
		inert := strings.Replace(svgA, index.SVGNamespaceAttr, "", 1)
		for _, banned := range []string{"<script", "href", "http", "<!--", "<metadata"} {
			if strings.Contains(inert, banned) {
				t.Errorf("%s: the card's SVG contains %q", lang, banned)
			}
		}
	}
}

// BV-42 (decision memoria, M2): findings-memory.json holds no path,
// repository, service, host_id, fingerprint, value, nor any raw id (finding,
// agent, credential): only salted references, codes and days.
func TestNoIdentityInFindingsMemory(t *testing.T) {
	day := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	salt := []byte("alice-salt-0123456789abcdef-0001")
	home, repo := "/home/alice", "/home/alice/work/webapp"
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImFsaWNlYWxpY2VhbGljZWFsaWNlIiwicm9sZSI6InNlcnZpY2Vfcm9sZSJ9.YWxpY2Utc2lnbmF0dXJlLWZpbGxlcg"
	v := scenarioV2(t, salt, home, repo, []platform.CredentialEntry{credEntry(repo+"/dashboard/.env.local", 4, "SUPABASE_SERVICE_ROLE_KEY", jwt)}, day)
	dir := t.TempDir()
	if err := memory.Write(dir, v.NextMemory()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, memory.FileName))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.ToLower(string(raw))
	if !strings.Contains(out, `"mid"`) || len(v.Findings) == 0 {
		t.Fatal("no memory entry — test would be vacuous")
	}
	forbidden := []string{
		home, repo, "alice", "webapp", "dashboard", ".env", "supabase", "service_role",
		graph.HostID(salt), "host_", "hmac-sha256", jwt, strings.Split(jwt, ".")[1], "agent_client", "http",
	}
	for _, f := range v.Findings {
		forbidden = append(forbidden, f.ID)
		forbidden = append(forbidden, f.Evidence.Nodes...)
	}
	for _, f := range forbidden {
		if strings.Contains(out, strings.ToLower(f)) {
			t.Errorf("findings-memory.json contains %q", f)
		}
	}
	// No raw graph or finding id, whatever its prefix.
	if m := regexp.MustCompile(`\b(f|cred|cap|res_[a-z_]+|edge_[a-z_]+|agent_client|agent_server)_[0-9a-f]{16}\b`).FindString(out); m != "" {
		t.Errorf("findings-memory.json contains the id %s", m)
	}
}
