package credfiles_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// BV-36. The Railway CLI's config files and keys come from the vendor's
// source: https://github.com/railwayapp/cli, commit
// b62467ef1b5303e3e340283ed96425dcf9612993, src/config.rs:
//
//	#[serde(rename_all = "camelCase")]
//	pub struct RailwayUser {
//	    pub id: Option<String>,
//	    pub token: Option<String>,
//	    pub access_token: Option<String>,
//	    pub refresh_token: Option<String>,
//	    pub token_expires_at: Option<i64>,
//	}
//
//	#[serde(rename_all = "camelCase")]
//	pub struct RailwayConfig {
//	    pub projects: BTreeMap<String, LinkedProject>,
//	    pub user: RailwayUser,
//
// and root_config_path():
//
//	Environment::Production => ".railway/config.json",
//	Environment::Staging => ".railway/config-staging.json",
//	Environment::Dev => ".railway/config-dev.json",
//	let home_dir = dirs::home_dir().context("Unable to get home directory")?;

// Fake Railway tokens (the CLI stores opaque strings).
var (
	railwayToken   = "rwtk" + strings.Repeat("T", 28) + "TOK1"
	railwayAccess  = "rwac" + strings.Repeat("A", 28) + "ACC2"
	railwayRefresh = "rwrf" + strings.Repeat("F", 28) + "REF3"
)

// railwayEntries lists the collected entries as "<file base>#<pointer>=<kind>".
func railwayEntries(scan platform.CredentialFileScan) []string {
	var out []string
	for _, e := range scan.Entries {
		out = append(out, filepath.Base(e.Path)+"#"+e.Pointer+"="+e.Kind)
	}
	sort.Strings(out)
	return out
}

func TestCollect_RailwayThreeFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".railway", "config.json")
	// Every other field holds secret-looking text: none may be read.
	write(t, path, `{
  "projects": {"/work/app": {"projectPath": "/work/app", "token": "`+ghp(1)+`"}},
  "user": {
    "id": "`+ghp(2)+`",
    "token": "`+railwayToken+`",
    "accessToken": "`+railwayAccess+`",
    "refreshToken": "`+railwayRefresh+`",
    "tokenExpiresAt": 1790000000,
    "TOKEN": "`+ghp(3)+`"
  },
  "editor": "`+ghp(4)+`",
  "sandboxes": [{"id": "`+ghp(5)+`", "environmentId": "e"}]
}`)
	scan := credfiles.Collect(home, nil, noEnv)
	want := []string{
		"config.json#/user/accessToken=railway_cli_access_token",
		"config.json#/user/refreshToken=railway_cli_refresh_token",
		"config.json#/user/token=railway_cli_token",
	}
	if got := railwayEntries(scan); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	if len(scan.Failed) != 0 {
		t.Errorf("failed: %+v", scan.Failed)
	}

	f, _, err := graph.FromScans(platform.MCPScan{}, platform.ClaudeSettingsScan{}, scan, []byte("0123456789abcdef0123456789abcdef"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	creds := map[string]string{}
	var railwayRes string
	for _, n := range f.Nodes {
		switch {
		case n.Kind == graph.KindCredential:
			creds[strings.Join(n.Locations, ",")] = n.Type + "/" + n.Service
		case n.Kind == graph.KindResource && n.Type == graph.ResourceExternalService && n.Path == "service://railway":
			railwayRes = n.ID
		}
	}
	wantCreds := map[string]string{
		path + "#/user/token":        "api_key/railway",
		path + "#/user/accessToken":  "api_key/railway",
		path + "#/user/refreshToken": "oauth_token/railway",
	}
	if !reflect.DeepEqual(creds, wantCreds) {
		t.Errorf("credentials %v, want %v", creds, wantCreds)
	}
	if railwayRes == "" {
		t.Fatal("no service://railway resource")
	}
	var authTo int
	for _, e := range f.Edges {
		if e.Type == graph.EdgeReceives {
			t.Errorf("receives edge from a credential file: %+v", e)
		}
		if e.Type == graph.EdgeAuthenticatesTo {
			authTo++
			if e.To != railwayRes || e.Certainty != graph.CertaintyInferred {
				t.Errorf("authenticates_to %+v, want inferred to %s", e, railwayRes)
			}
		}
	}
	if authTo != 3 {
		t.Errorf("authenticates_to edges: %d, want 3", authTo)
	}
}

// A file without user, or whose user has no tokens, yields nothing and is
// no failure.
func TestCollect_RailwayNoUser(t *testing.T) {
	for name, body := range map[string]string{
		"no user":    `{"projects": {}, "editor": "code"}`,
		"user null":  `{"user": null}`,
		"user empty": `{"user": {"id": "u1"}}`,
	} {
		home := t.TempDir()
		write(t, filepath.Join(home, ".railway", "config.json"), body)
		scan := credfiles.Collect(home, nil, noEnv)
		if len(scan.Entries) != 0 || len(scan.Failed) != 0 || len(scan.Read) != 1 {
			t.Errorf("%s: entries %v, failed %+v, read %v", name, railwayEntries(scan), scan.Failed, scan.Read)
		}
	}
}

func TestCollect_RailwayEmptyFields(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".railway", "config.json"), `{"user": {"token": "", "accessToken": "   ", "refreshToken": null}}`)
	scan := credfiles.Collect(home, nil, noEnv)
	if len(scan.Entries) != 0 || len(scan.Failed) != 0 {
		t.Errorf("entries %v, failed %+v", railwayEntries(scan), scan.Failed)
	}
}

// Production, Staging and Dev each have their own file in ~/.railway.
func TestCollect_RailwayThreeFiles(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".railway", "config.json"), `{"user": {"token": "`+railwayToken+`"}}`)
	write(t, filepath.Join(home, ".railway", "config-staging.json"), `{"user": {"accessToken": "`+railwayAccess+`"}}`)
	write(t, filepath.Join(home, ".railway", "config-dev.json"), `{"user": {"refreshToken": "`+railwayRefresh+`"}}`)
	write(t, filepath.Join(home, ".railway", "config-other.json"), `{"user": {"token": "`+ghp(1)+`"}}`) // not a CLI file
	scan := credfiles.Collect(home, nil, noEnv)
	want := []string{
		"config-dev.json#/user/refreshToken=railway_cli_refresh_token",
		"config-staging.json#/user/accessToken=railway_cli_access_token",
		"config.json#/user/token=railway_cli_token",
	}
	if got := railwayEntries(scan); !reflect.DeepEqual(got, want) {
		t.Errorf("entries %v, want %v", got, want)
	}
}

// Invalid JSON, or a user or token of the wrong type, goes to coverage as
// parse_error; the reason never holds the file's content.
func TestCollect_RailwayInvalidJSON(t *testing.T) {
	for name, body := range map[string]string{
		"syntax":          `{"user": {"token": "` + railwayToken + `"`,
		"not an object":   `["` + railwayToken + `"]`,
		"user not object": `{"user": "` + railwayToken + `"}`,
		"token number":    `{"user": {"token": 12345, "accessToken": "` + railwayAccess + `"}}`,
	} {
		home := t.TempDir()
		path := filepath.Join(home, ".railway", "config.json")
		write(t, path, body)
		scan := credfiles.Collect(home, nil, noEnv)
		if len(scan.Entries) != 0 || len(scan.Read) != 0 {
			t.Errorf("%s: entries %v, read %v", name, railwayEntries(scan), scan.Read)
		}
		if len(scan.Failed) != 1 || scan.Failed[0].Path != path || !strings.HasPrefix(scan.Failed[0].Reason, graph.ReasonParseError+": ") {
			t.Errorf("%s: coverage %+v", name, scan.Failed)
			continue
		}
		for _, v := range []string{railwayToken[4:32], railwayAccess[4:32], "12345"} {
			if strings.Contains(scan.Failed[0].Reason, v) {
				t.Errorf("%s: reason holds content: %q", name, scan.Failed[0].Reason)
			}
		}
	}
}
