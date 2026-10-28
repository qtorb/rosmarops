package credfiles_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// envWith is an Env with one variable.
func envWith(name, value string) credfiles.Env {
	return func(k string) (string, bool) {
		if k == name {
			return value, true
		}
		return "", false
	}
}

// entriesOf are path's entries, pointer → kind=value.
func entriesOf(t *testing.T, home string, env credfiles.Env, path string) (map[string]string, []string, []string) {
	t.Helper()
	scan := credfiles.Collect(home, nil, env)
	got := map[string]string{}
	for _, e := range scan.Entries {
		if e.Path == path {
			got[e.Pointer] = e.Kind + "=" + e.Value.Reveal()
		}
	}
	var conflicts, failed []string
	for _, c := range scan.KeyConflicts {
		if c.Path == path {
			conflicts = append(conflicts, c.Pointer)
		}
	}
	for _, f := range scan.Failed {
		if f.Path == path {
			failed = append(failed, f.Reason)
		}
	}
	return got, conflicts, failed
}

// N1b (BV-96): Codex's .credentials.json, the MCP OAuth fallback
// (codex-rs/rmcp-client/src/oauth.rs @ b89952d). Per entry, access_token and
// refresh_token; server_name, server_url and client_id are never read; a
// key with other capitals is not read and is reported.
func TestCollect_CodexCredentials(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", ".credentials.json")
	write(t, path, `{
	  "github|https://api.github.example/mcp": {"server_name": "github", "server_url": "https://api.github.example/mcp",
	    "client_id": "cid-1", "access_token": "mcp-acc-1", "refresh_token": "mcp-ref-1", "expires_at": 1, "scopes": ["x"]},
	  "linear/x": {"Access_Token": "hidden-acc", "refresh_token": "mcp-ref-2"},
	  "odd": "not an entry"}`)
	got, conflicts, failed := entriesOf(t, home, noEnv, path)
	want := map[string]string{
		"/github|https:~1~1api.github.example~1mcp/access_token":  "codex_mcp_access_token=mcp-acc-1",
		"/github|https:~1~1api.github.example~1mcp/refresh_token": "codex_mcp_refresh_token=mcp-ref-1",
		"/linear~1x/refresh_token":                                "codex_mcp_refresh_token=mcp-ref-2",
	}
	if !reflect.DeepEqual(got, want) || len(failed) != 0 {
		t.Errorf("entries %v (failed %v), want %v", got, failed, want)
	}
	if !reflect.DeepEqual(conflicts, []string{"/linear~1x/Access_Token"}) {
		t.Errorf("conflicts %v", conflicts)
	}
	for p, v := range got {
		if strings.Contains(v, "api.github.example") || strings.Contains(v, "cid-1") || strings.Contains(p, "server_") {
			t.Errorf("%s: server_name, server_url or client_id read", p)
		}
	}
}

// Its absence is the normal case (the keyring works): no entry, no failure.
// CODEX_HOME moves it, as it moves auth.json.
func TestCollect_CodexCredentialsAbsentOrMoved(t *testing.T) {
	home := t.TempDir()
	scan := credfiles.Collect(home, nil, noEnv)
	for _, f := range scan.Failed {
		if strings.HasSuffix(f.Path, ".credentials.json") {
			t.Errorf("absent file failed: %+v", f)
		}
	}
	alt := filepath.Join(home, "alt")
	path := filepath.Join(alt, ".credentials.json")
	write(t, path, `{"k": {"access_token": "mcp-acc-alt"}}`)
	if got, _, _ := entriesOf(t, home, envWith("CODEX_HOME", alt), path); got["/k/access_token"] != "codex_mcp_access_token=mcp-acc-alt" {
		t.Errorf("CODEX_HOME: %v", got)
	}
}

// N1b (BV-96): Hermes Agent's auth.json (agent/credential_pool.py @
// 7ee6d99), read with a BOM as Hermes does (utf-8-sig). Nous keeps its
// secrets flat; other providers under tokens; the pool per provider and
// index. A value a built-in pattern recognises is left to the pattern (no
// kind), so it is the same credential as anywhere else.
func TestCollect_HermesAuth(t *testing.T) {
	home := t.TempDir()
	hermes := filepath.Join(home, "hermes")
	path := filepath.Join(hermes, "auth.json")
	anthropic := "sk-ant-api03-" + strings.Repeat("A", 93) + "-AAABBBCCCDDDAA" // the Anthropic pattern
	write(t, path, "\ufeff"+`{"active_provider": "nous",
	  "providers": {
	    "nous": {"access_token": "nous-acc", "refresh_token": "nous-ref", "agent_key": "nous-agent", "portal_base_url": "https://portal.example"},
	    "openai-codex": {"tokens": {"access_token": "oc-acc", "refresh_token": "oc-ref"}, "access_token": "flat-not-read"},
	    "odd": "x"},
	  "credential_pool": {
	    "anthropic": [{"api_key": "`+anthropic+`", "label": "main"}, {"access_token": "pool-acc", "Refresh_Token": "hidden"}],
	    "nous": [{"agent_key": "pool-agent", "refresh_token": "pool-ref"}],
	    "odd": {"x": 1}}}`)
	got, conflicts, failed := entriesOf(t, home, envWith("HERMES_HOME", hermes), path)
	want := map[string]string{
		"/providers/nous/access_token":                 "hermes_access_token=nous-acc",
		"/providers/nous/refresh_token":                "hermes_refresh_token=nous-ref",
		"/providers/nous/agent_key":                    "hermes_agent_key=nous-agent",
		"/providers/openai-codex/tokens/access_token":  "hermes_access_token=oc-acc",
		"/providers/openai-codex/tokens/refresh_token": "hermes_refresh_token=oc-ref",
		"/credential_pool/anthropic/0/api_key":         "=" + anthropic,
		"/credential_pool/anthropic/1/access_token":    "hermes_access_token=pool-acc",
		"/credential_pool/nous/0/agent_key":            "hermes_agent_key=pool-agent",
		"/credential_pool/nous/0/refresh_token":        "hermes_refresh_token=pool-ref",
	}
	if !reflect.DeepEqual(got, want) || len(failed) != 0 {
		t.Errorf("entries %v (failed %v),\nwant %v", got, failed, want)
	}
	if !reflect.DeepEqual(conflicts, []string{"/credential_pool/anthropic/1/Refresh_Token"}) {
		t.Errorf("conflicts %v", conflicts)
	}
}

// A value of the wrong type fails the file, never with its content; a
// missing file is not a failure.
func TestCollect_HermesAuthShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		failed  bool
	}{
		"not an object":       {`["sk-secret-in-array"]`, true},
		"providers not obj":   {`{"providers": "sk-secret-text"}`, true},
		"token not a string":  {`{"providers": {"nous": {"access_token": {"v": "sk-secret-nested"}}}}`, true},
		"pool item not str":   {`{"credential_pool": {"p": [{"api_key": 42}]}}`, true},
		"empty object":        {`{}`, false},
		"null and empty":      {`{"providers": {"nous": {"access_token": null, "agent_key": " "}}}`, false},
		"other capital roots": {`{"Providers": {"nous": {"access_token": "x"}}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			hermes := filepath.Join(home, "hermes")
			path := filepath.Join(hermes, "auth.json")
			write(t, path, tc.content)
			got, _, failed := entriesOf(t, home, envWith("HERMES_HOME", hermes), path)
			if (len(failed) > 0) != tc.failed || len(got) != 0 {
				t.Errorf("entries %v, failed %v", got, failed)
			}
			for _, r := range failed {
				if strings.Contains(r, "sk-secret") {
					t.Errorf("reason holds content: %s", r)
				}
			}
		})
	}
	home := t.TempDir()
	if _, _, failed := entriesOf(t, home, envWith("HERMES_HOME", filepath.Join(home, "hermes")), filepath.Join(home, "hermes", "auth.json")); len(failed) != 0 {
		t.Errorf("absent file failed: %v", failed)
	}
}

// Every kind the two files state maps to a credential type; the service is
// unknown (the file does not name it).
func TestAgentAuthKindsMapped(t *testing.T) {
	for _, k := range []string{"codex_mcp_access_token", "codex_mcp_refresh_token", "hermes_access_token", "hermes_refresh_token", "hermes_agent_key", "hermes_api_key"} {
		if typ, service, ok := graph.CredentialKind(k); !ok || typ == "" || service != graph.ServiceUnknown {
			t.Errorf("%s: %q %q %v", k, typ, service, ok)
		}
	}
}
