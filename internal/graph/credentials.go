package graph

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Credential node types and the service used when a pattern does not name one.
const (
	CredentialAPIKey        = "api_key"
	CredentialDBCredential  = "db_credential"
	CredentialOAuthToken    = "oauth_token"
	CredentialWebhookSecret = "webhook_secret"
	CredentialPrivateKey    = "private_key"
	CredentialGenericSecret = "generic_secret"
	ServiceUnknown          = "unknown"
)

// CertaintyInferred marks an edge deduced rather than read as-is.
const CertaintyInferred = "inferred"

// credentialKind is the credential type and service a secret pattern implies.
type credentialKind struct {
	Type    string
	Service string
}

// credentialKinds maps each built-in secret pattern ID, and each kind a
// credential file's format states, to the credential type and service it
// denotes. Every built-in pattern must have an entry (tested).
var credentialKinds = map[string]credentialKind{
	"anthropic_api_key":              {CredentialAPIKey, "anthropic"},
	"openai_project_key":             {CredentialAPIKey, "openai"},
	"openai_api_key":                 {CredentialAPIKey, "openai"},
	"openrouter_api_key":             {CredentialAPIKey, "openrouter"},
	"github_pat_fine_grained":        {CredentialAPIKey, "github"},
	"github_pat_classic":             {CredentialAPIKey, "github"},
	"github_oauth_token":             {CredentialOAuthToken, "github"},
	"github_user_to_server_token":    {CredentialOAuthToken, "github"},
	"npm_access_token":               {CredentialAPIKey, "npm"},
	"railway_api_token":              {CredentialAPIKey, "railway"},
	"aws_access_key_id":              {CredentialAPIKey, "aws"},
	"aws_temp_access_key":            {CredentialAPIKey, "aws"},
	"stripe_secret_key":              {CredentialAPIKey, "stripe"},
	"stripe_restricted_key":          {CredentialAPIKey, "stripe"},
	"slack_bot_token":                {CredentialOAuthToken, "slack"},
	"slack_webhook_url":              {CredentialWebhookSecret, "slack"},
	"jwt_token":                      {CredentialGenericSecret, ServiceUnknown},
	"supabase_secret_key":            {CredentialAPIKey, "supabase"},
	"supabase_publishable_key":       {CredentialAPIKey, "supabase"},
	"database_url_with_credentials":  {CredentialDBCredential, ServiceUnknown},
	"authorization_bearer_hardcoded": {CredentialGenericSecret, ServiceUnknown},
	"private_key_pem":                {CredentialPrivateKey, ServiceUnknown},

	// Kinds a credential file's format states for a key, not secret
	// patterns (platform.CredentialEntry.Kind, BV-36).
	"railway_cli_token":         {CredentialAPIKey, "railway"},
	"railway_cli_access_token":  {CredentialAPIKey, "railway"},
	"railway_cli_refresh_token": {CredentialOAuthToken, "railway"},

	// Codex's auth.json (decision 2026-10-27, agentes nuevos, N1).
	"codex_openai_api_key":        {CredentialAPIKey, "openai"},
	"codex_access_token":          {CredentialOAuthToken, "openai"},
	"codex_refresh_token":         {CredentialOAuthToken, "openai"},
	"codex_id_token":              {CredentialOAuthToken, "openai"},
	"codex_personal_access_token": {CredentialAPIKey, "openai"},

	// N1b (BV-96): Codex's MCP OAuth fallback file and Hermes Agent's
	// auth.json. The service behind them is not stated in the file (an MCP
	// server; a Hermes provider), so it is unknown; a value a built-in
	// pattern recognises takes that pattern's kind instead.
	"codex_mcp_access_token":  {CredentialOAuthToken, ServiceUnknown},
	"codex_mcp_refresh_token": {CredentialOAuthToken, ServiceUnknown},
	"hermes_access_token":     {CredentialOAuthToken, ServiceUnknown},
	"hermes_refresh_token":    {CredentialOAuthToken, ServiceUnknown},
	"hermes_agent_key":        {CredentialAPIKey, ServiceUnknown},
	"hermes_api_key":          {CredentialAPIKey, ServiceUnknown},

	// Claude Code's account OAuth tokens (BV-111): the service is Anthropic.
	"claude_code_oauth_token": {CredentialOAuthToken, "anthropic"},
}

// supabaseKeyScopes is the observed_scope read from the prefix of Supabase
// opaque API keys.
var supabaseKeyScopes = map[string]string{
	"supabase_secret_key":      "secret",
	"supabase_publishable_key": "publishable",
}

// CredentialKind returns the credential type and service for a secret pattern
// ID, and whether the pattern is known.
func CredentialKind(patternID string) (credType, service string, ok bool) {
	k, ok := credentialKinds[patternID]
	return k.Type, k.Service, ok
}

// hostIDLabel is the HMAC message host_id is derived from.
// Stable label; must not change with product renames, or host_id changes.
const hostIDLabel = "rosmarops-vigil/host-id/v1"

// minSaltLen guards against fingerprints keyed with a missing salt.
const minSaltLen = 16

// errNoSalt is returned when credential fingerprints are requested without the
// machine-local salt.
var errNoSalt = errors.New("credential fingerprints need the machine-local salt")

// HostID derives a stable machine id from the local salt that reveals nothing
// about the machine: "host_" + the first 16 hex characters of
// HMAC-SHA256(salt, "rosmarops-vigil/host-id/v1").
func HostID(salt []byte) string {
	return "host_" + hash.Keyed(salt, []byte(hostIDLabel))[:16]
}

// Fingerprint returns the salted fingerprint of a secret:
// "hmac-sha256:" + HMAC-SHA256(salt, secret). secret is the span chosen by
// secrets.ClassifyEnv (the secret itself, never the variable name), so the
// same secret matches across contexts.
func Fingerprint(salt []byte, secret string) string {
	return "hmac-sha256:" + hash.Keyed(salt, []byte(secret))
}

// serviceIdentity is what a credential's own text says about the service it
// opens.
type serviceIdentity struct {
	service       string
	path          string // project URL when known, else "service://<service>"
	observedScope string // scope read from the token itself, else empty
	observed      bool   // issuer and project are written in the token
}

// identifyService works out the service behind a credential. The service
// comes from the pattern table, except for JWTs, which are decoded locally
// (without verifying the signature) to recognise Supabase project keys.
// Only iss, ref and role are read; no other claim reaches the graph.
func identifyService(patternID, credService, secret string) serviceIdentity {
	if patternID == "jwt_token" {
		if ref, role, ok := supabaseJWTClaims(secret); ok {
			return serviceIdentity{
				service:       "supabase",
				path:          "https://" + ref + ".supabase.co",
				observedScope: role,
				observed:      true,
			}
		}
	}
	return serviceIdentity{
		service:       credService,
		path:          "service://" + credService,
		observedScope: supabaseKeyScopes[patternID],
	}
}

// supabaseJWTClaims decodes a JWT payload (second segment, base64url with or
// without padding) and returns ref and role if iss is "supabase" or
// "supabase-demo" and both are non-empty strings.
func supabaseJWTClaims(token string) (ref, role string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", "", false
	}
	var claims struct {
		Iss  any `json:"iss"`
		Ref  any `json:"ref"`
		Role any `json:"role"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", false
	}
	iss, _ := claims.Iss.(string)
	ref, _ = claims.Ref.(string)
	role, _ = claims.Role.(string)
	if (iss != "supabase" && iss != "supabase-demo") || ref == "" || role == "" {
		return "", "", false
	}
	return ref, role, true
}

// CredentialsFromMCP builds, for the secrets in each MCP server's env block:
//   - credential nodes, one per fingerprint, with all their locations;
//   - receives edges, one per credential and server (observed), whether the
//     secret sits in the server's env or in a remote server's headers;
//   - for credentials whose service is known, an external_service resource
//     and an authenticates_to edge to it.
//
// authenticates_to certainty: "observed" only when the token itself names
// its issuer and project (a Supabase JWT with iss and ref); "inferred" when
// the service comes from the pattern table. observed_scope is set only when
// read from the token (the JWT role, or the sb_ key prefix).
//
// Servers with LoadsMCP false are skipped: a secret in a file the client
// does not load MCP servers from is never received by a server.
//
// It is a pure function with no I/O. Env values are used only to compute
// fingerprints and to read the claims above; they are never stored.
func CredentialsFromMCP(servers []platform.MCPServer, salt []byte, discoveredAt time.Time) ([]Node, []Edge, error) {
	return credentialsFrom(mcpCredentialInputs(servers), salt, discoveredAt)
}

// credentialInput is one value that may hold a secret: the agent that
// receives it (empty for a file at rest, which no agent receives), the
// location of the value and the entry itself.
type credentialInput struct {
	to       string
	location string
	kind     string // set when the source states the credential kind
	platform.EnvVar

	// committed is set for a secret a repository scan found (CTO-7 §2.2):
	// already identified, without its value. location is empty when it is
	// only in the history.
	committed *finding.CommittedSecret
}

// mcpCredentialInputs lists the env and header entries of every server
// whose client loads it, received by that server.
func mcpCredentialInputs(servers []platform.MCPServer) []credentialInput {
	var out []credentialInput
	for _, srv := range servers {
		if !srv.LoadsMCP {
			continue // the client does not load this file's MCP servers
		}
		to := serverID(srv)
		for _, ev := range secretInputs(srv) {
			if srv.Client == knowledge.CursorProduct && cursorEnvRef.MatchString(strings.TrimSpace(ev.Value.Reveal())) {
				continue // ${env:NAME} is not resolved at rest (decision 2026-10-25, Cursor, K10)
			}
			out = append(out, credentialInput{
				to:       to,
				location: srv.ConfigPath + "#" + serverPointer(srv) + "/" + ev.section + "/" + pointerEscape(ev.Key),
				EnvVar:   ev.EnvVar,
			})
		}
	}
	return out
}

// credentialsFrom builds credential nodes (one per fingerprint, with every
// location), receives edges (one per credential and receiving agent) and the
// external_service resources and authenticates_to edges described on
// CredentialsFromMCP, from any set of inputs.
func credentialsFrom(inputs []credentialInput, salt []byte, discoveredAt time.Time) ([]Node, []Edge, error) {
	return credentialsFromRepos(inputs, nil, salt, discoveredAt)
}

// credentialsFromRepos is credentialsFrom with the families of the
// repositories of committed inputs (root → first root of its family).
func credentialsFromRepos(inputs []credentialInput, families map[string]string, salt []byte, discoveredAt time.Time) ([]Node, []Edge, error) {
	if len(salt) < minSaltLen {
		return nil, nil, errNoSalt
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	patterns := secrets.BuiltinPatterns()

	creds := make(map[string]*Node)
	identities := make(map[string]serviceIdentity)
	locations := make(map[string]map[string]bool)
	edges := make(map[string]Edge)
	repos := make(map[string]Node)

	for _, in := range inputs {
		to, ev, loc := in.to, in.EnvVar, in.location
		var patternID, secret string
		var ok bool
		if c := in.committed; c != nil {
			committedInput(creds, identities, locations, edges, repos, c, loc, families, seen)
			continue
		}
		if in.kind != "" {
			// The source states the kind: the whole value is the secret.
			patternID, secret = in.kind, strings.TrimSpace(ev.Value.Reveal())
			ok = secret != ""
		} else {
			patternID, secret, ok = secrets.ClassifyEnv(ev.Key, ev.Value, patterns)
		}
		if !ok {
			continue
		}
		fp := Fingerprint(salt, secret)
		id := "cred_" + keyHash(fp)

		if _, ok := creds[id]; !ok {
			credType, service, known := CredentialKind(patternID)
			if !known {
				credType, service = CredentialGenericSecret, ServiceUnknown
			}
			ident := identifyService(patternID, service, secret)
			if ident.observed {
				// A JWT that names its Supabase project is that project's API key.
				credType = CredentialAPIKey
			}
			creds[id] = &Node{
				ID:           id,
				Kind:         KindCredential,
				Type:         credType,
				Service:      ident.service,
				Fingerprint:  fp,
				DiscoveredAt: seen,
			}
			identities[id] = ident
			locations[id] = make(map[string]bool)
		}
		locations[id][loc] = true
		if to == "" {
			continue // a file at rest: no agent receives it
		}

		eid := edgeID(EdgeReceives, id, to)
		if e, ok := edges[eid]; !ok || loc < e.Source {
			// One edge per credential and receiving agent; if the same
			// secret is under two keys of one agent, keep the first location.
			edges[eid] = Edge{
				ID:        eid,
				Type:      EdgeReceives,
				From:      id,
				To:        to,
				Source:    loc,
				Certainty: CertaintyObserved,
			}
		}
	}

	nodes := make([]Node, 0, len(creds))
	resources := make(map[string]Node)
	for id, n := range creds {
		for loc := range locations[id] {
			n.Locations = append(n.Locations, loc)
		}
		sort.Strings(n.Locations)
		nodes = append(nodes, *n)

		ident := identities[id]
		if ident.service == ServiceUnknown {
			continue
		}
		rid := serviceResourceID(ident.path)
		if _, ok := resources[rid]; !ok {
			resources[rid] = Node{
				ID:           rid,
				Kind:         KindResource,
				Type:         ResourceExternalService,
				Path:         ident.path,
				DiscoveredAt: seen,
			}
		}
		certainty := CertaintyInferred
		if ident.observed {
			certainty = CertaintyObserved
		}
		eid := edgeID(EdgeAuthenticatesTo, id, rid)
		edges[eid] = Edge{
			ID:            eid,
			Type:          EdgeAuthenticatesTo,
			From:          id,
			To:            rid,
			Source:        firstSource(n.Locations, edges, id),
			Certainty:     certainty,
			ObservedScope: ident.observedScope,
		}
	}
	for _, r := range resources {
		nodes = append(nodes, r)
	}
	for _, r := range repos {
		nodes = append(nodes, r)
	}

	out := make([]Edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, e)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return nodes, out, nil
}

// serviceResourceID derives an external_service resource id from its natural
// key: type + canonical path (lowercased; service paths are case-insensitive).
func serviceResourceID(path string) string {
	return "res_service_" + keyHash(ResourceExternalService, strings.ToLower(path))
}

// secretInput is one env or header entry of a server, with the section it
// came from ("env" or "headers").
type secretInput struct {
	platform.EnvVar
	section string
}

// secretInputs lists a server's env entries and, for remote servers, its
// headers. Both are classified the same way.
//
// Locations are JSON Pointers into the config file:
// "<config_path>#<server pointer>/env/<KEY>" or ".../headers/<KEY>", where the
// server pointer is "/mcpServers/<name>" or
// "/projects/<path>/mcpServers/<name>", all escaped per RFC 6901
// ("~" → "~0", "/" → "~1").
func secretInputs(srv platform.MCPServer) []secretInput {
	out := make([]secretInput, 0, len(srv.Env)+len(srv.Headers))
	for _, e := range srv.Env {
		out = append(out, secretInput{e, "env"})
	}
	for _, h := range srv.Headers {
		out = append(out, secretInput{h, "headers"})
	}
	return out
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func pointerEscape(s string) string {
	return pointerEscaper.Replace(s)
}

// credentialFileInputsFor lists the entries of the credential files collector
// (decision 2026-10-24, D1): location "<file>:<line>", or "<file>#<JSON
// Pointer>" for a value read from a JSON file, received by no agent, but
// the entries of a Cursor server's envFile, which that server receives
// (decision 2026-10-25, Cursor, K10).
func credentialFileInputsFor(scan platform.CredentialFileScan, servers []platform.MCPServer) []credentialInput {
	envFile := map[string][]string{} // canonical envFile → servers
	for _, srv := range servers {
		if srv.EnvFile != "" && srv.LoadsMCP {
			k := canonicalPath(srv.EnvFile)
			envFile[k] = append(envFile[k], serverID(srv))
		}
	}
	out := make([]credentialInput, 0, len(scan.Entries))
	for _, e := range scan.Entries {
		loc := e.Path + ":" + strconv.Itoa(e.Line)
		if e.Pointer != "" {
			loc = e.Path + "#" + e.Pointer
		}
		in := credentialInput{
			location: loc,
			kind:     e.Kind,
			EnvVar:   platform.EnvVar{Key: e.Key, Value: e.Value},
		}
		out = append(out, in)
		for _, to := range envFile[canonicalPath(e.Path)] {
			in.to = to
			out = append(out, in)
		}
	}
	return out
}
