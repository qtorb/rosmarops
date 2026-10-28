package credfiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/jsonkeys"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// The agents' key files of N1b (decision 2026-10-27, agentes nuevos, signed
// 2026-10-28; BV-92 §4, docs/research/2026-10-28-bv92.md). Both are read
// with exact keys (jsonkeys, decision 2026-10-27, blindaje, B5): a key that
// matches only without regard to case is not read and is reported. Only the
// secrets below are kept, by JSON Pointer; nothing else of either file
// (Codex's server_name and server_url, client ids, expiry, the active
// provider) is read. No error text holds file content.

// codexCredentialEntry is one entry of Codex's .credentials.json, the MCP
// OAuth fallback of the OS keyring: https://github.com/openai/codex, commit
// b89952d582beaae3af3ddd7b679286cbb094f8b9, codex-rs/rmcp-client/src/oauth.rs
// (FallbackFile = BTreeMap<String, FallbackTokenEntry>; access_token L869,
// refresh_token L873).
type codexCredentialEntry struct {
	AccessToken  *string `json:"access_token"`
	RefreshToken *string `json:"refresh_token"`
}

// parseCodexCredentials returns the access and refresh tokens of every entry
// of a Codex .credentials.json: /<key>/access_token and /<key>/refresh_token.
func parseCodexCredentials(path string, raw []byte) ([]platform.CredentialEntry, []string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, nil, errors.New("not a JSON object")
	}
	var out []platform.CredentialEntry
	var conflicts []string
	for _, key := range sortedKeys(root) {
		base := "/" + pointerEscape(key)
		if !isObject(root[key]) {
			continue // not a token entry: nothing to read
		}
		var e codexCredentialEntry
		c, err := jsonkeys.Decode(root[key], &e)
		if err != nil {
			return nil, nil, fmt.Errorf("%s has a value that is not a string", base)
		}
		conflicts = append(conflicts, prefixed(base, c)...)
		out = appendSecret(out, path, base+"/access_token", "access_token", e.AccessToken, "codex_mcp_access_token")
		out = appendSecret(out, path, base+"/refresh_token", "refresh_token", e.RefreshToken, "codex_mcp_refresh_token")
	}
	return out, conflicts, nil
}

// hermesAuthFile is the part of Hermes Agent's <HERMES_HOME>\auth.json
// RosmarOps reads: https://github.com/NousResearch/hermes-agent, commit
// 7ee6d99c4f16df08cd6ca0b2c34abb81014db2ba, agent/credential_pool.py
// (providers L1467-L1482, credential_pool L216-L274). Hermes reads it as
// utf-8-sig, so a leading BOM is tolerated.
type hermesAuthFile struct {
	Providers      map[string]json.RawMessage `json:"providers"`
	CredentialPool map[string]json.RawMessage `json:"credential_pool"`
}

// hermesProvider is providers.<p>: Nous keeps its secrets flat; every other
// provider under tokens.
type hermesProvider struct {
	AccessToken  *string `json:"access_token"`
	RefreshToken *string `json:"refresh_token"`
	AgentKey     *string `json:"agent_key"`
	Tokens       *struct {
		AccessToken  *string `json:"access_token"`
		RefreshToken *string `json:"refresh_token"`
	} `json:"tokens"`
}

// hermesPoolEntry is credential_pool.<p>[i].
type hermesPoolEntry struct {
	AccessToken  *string `json:"access_token"`
	RefreshToken *string `json:"refresh_token"`
	AgentKey     *string `json:"agent_key"`
	APIKey       *string `json:"api_key"`
}

// parseHermesAuth returns the secrets of a Hermes Agent auth.json:
// /providers/nous/{access_token,refresh_token,agent_key},
// /providers/<p>/tokens/{access_token,refresh_token} and
// /credential_pool/<p>/<i>/{access_token,refresh_token,agent_key,api_key}.
func parseHermesAuth(path string, raw []byte) ([]platform.CredentialEntry, []string, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var file hermesAuthFile
	if !isObject(raw) {
		return nil, nil, errors.New("not a JSON object")
	}
	conflicts, err := jsonkeys.Decode(raw, &file)
	if err != nil {
		return nil, nil, errors.New("providers or credential_pool is not a JSON object")
	}
	var out []platform.CredentialEntry
	for _, p := range sortedKeys(file.Providers) {
		base := "/providers/" + pointerEscape(p)
		if !isObject(file.Providers[p]) {
			continue
		}
		var pr hermesProvider
		c, err := jsonkeys.Decode(file.Providers[p], &pr)
		if err != nil {
			return nil, nil, fmt.Errorf("%s has a value of the wrong type", base)
		}
		conflicts = append(conflicts, prefixed(base, c)...)
		if p == "nous" {
			out = appendSecret(out, path, base+"/access_token", "access_token", pr.AccessToken, "hermes_access_token")
			out = appendSecret(out, path, base+"/refresh_token", "refresh_token", pr.RefreshToken, "hermes_refresh_token")
			out = appendSecret(out, path, base+"/agent_key", "agent_key", pr.AgentKey, "hermes_agent_key")
		}
		if pr.Tokens != nil {
			out = appendSecret(out, path, base+"/tokens/access_token", "access_token", pr.Tokens.AccessToken, "hermes_access_token")
			out = appendSecret(out, path, base+"/tokens/refresh_token", "refresh_token", pr.Tokens.RefreshToken, "hermes_refresh_token")
		}
	}
	for _, p := range sortedKeys(file.CredentialPool) {
		base := "/credential_pool/" + pointerEscape(p)
		var list []json.RawMessage
		if json.Unmarshal(file.CredentialPool[p], &list) != nil {
			continue // not a list of entries: nothing to read
		}
		for i, item := range list {
			at := base + "/" + strconv.Itoa(i)
			if !isObject(item) {
				continue
			}
			var e hermesPoolEntry
			c, err := jsonkeys.Decode(item, &e)
			if err != nil {
				return nil, nil, fmt.Errorf("%s has a value that is not a string", at)
			}
			conflicts = append(conflicts, prefixed(at, c)...)
			out = appendSecret(out, path, at+"/access_token", "access_token", e.AccessToken, "hermes_access_token")
			out = appendSecret(out, path, at+"/refresh_token", "refresh_token", e.RefreshToken, "hermes_refresh_token")
			out = appendSecret(out, path, at+"/agent_key", "agent_key", e.AgentKey, "hermes_agent_key")
			out = appendSecret(out, path, at+"/api_key", "api_key", e.APIKey, "hermes_api_key")
		}
	}
	sort.Strings(conflicts)
	return out, conflicts, nil
}

// appendSecret adds a non-empty value. A value a built-in secret pattern
// recognises (an Anthropic or OpenAI key in Hermes' pool, say) is left to
// the patterns, so that it is the same credential, with the same service,
// as anywhere else; any other value is a credential of the fallback kind.
func appendSecret(out []platform.CredentialEntry, path, pointer, key string, v *string, kind string) []platform.CredentialEntry {
	if v == nil || strings.TrimSpace(*v) == "" {
		return out
	}
	value := secrets.NewSecretValue(*v)
	if _, _, ok := secrets.ClassifyEnv(key, value, secrets.BuiltinPatterns()); ok {
		kind = ""
	}
	return append(out, platform.CredentialEntry{Path: path, Key: key, Pointer: pointer, Kind: kind, Value: value})
}

func isObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

func prefixed(base string, pointers []string) []string {
	out := make([]string, len(pointers))
	for i, p := range pointers {
		out[i] = base + p
	}
	return out
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func pointerEscape(s string) string { return pointerEscaper.Replace(s) }
