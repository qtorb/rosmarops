package graph_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// A database URL to a Supabase database is that project's credential: the
// direct host db.<ref>.supabase.co and the pooler user postgres.<ref> both
// name the project. Any other host stays an unknown service. The password
// reaches no node, edge or path.
func TestSupabaseDatabaseURL(t *testing.T) {
	const pass = "Zq9-PlantedDBPassword-77"
	cases := []struct {
		name, url, service, path string
	}{
		{"direct", "postgresql://postgres:" + pass + "@db." + supabaseRef + ".supabase.co:5432/postgres", "supabase", "https://" + supabaseRef + ".supabase.co"},
		{"pooler", "postgres://postgres." + supabaseRef + ":" + pass + "@aws-0-eu-central-1.pooler.supabase.com:6543/postgres", "supabase", "https://" + supabaseRef + ".supabase.co"},
		{"pooler without project user", "postgres://postgres:" + pass + "@aws-0-eu-central-1.pooler.supabase.com:6543/postgres", graph.ServiceUnknown, ""},
		{"other host", "postgres://admin:" + pass + "@db.example.com:5432/app", graph.ServiceUnknown, ""},
		{"lookalike", "postgres://postgres:" + pass + "@db." + supabaseRef + ".supabase.co.evil.example/app", graph.ServiceUnknown, ""},
	}
	for _, c := range cases {
		nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "x", env("DATABASE_URL", c.url))}, testSalt)
		creds := nodesOfKind(nodes, graph.KindCredential)
		if len(creds) != 1 {
			t.Fatalf("%s: %d credentials", c.name, len(creds))
		}
		cr := creds[0]
		if cr.Type != graph.CredentialDBCredential || cr.Service != c.service {
			t.Errorf("%s: %s/%s, want db_credential/%s", c.name, cr.Type, cr.Service, c.service)
		}
		e, ok := edgeTo(edges, graph.EdgeAuthenticatesTo, cr.ID)
		switch {
		case c.path == "" && ok:
			t.Errorf("%s: an authenticates_to edge for an unknown service", c.name)
		case c.path != "":
			res, _ := nodeWithID(nodes, e.To)
			if !ok || res.Path != c.path {
				t.Errorf("%s: service resource %q, want %q", c.name, res.Path, c.path)
			}
		}
		raw, _ := json.Marshal(struct {
			N []graph.Node
			E []graph.Edge
		}{nodes, edges})
		if strings.Contains(string(raw), pass) || strings.Contains(string(raw), "PlantedDB") {
			t.Errorf("%s: the password reached the graph", c.name)
		}
	}
}

// The same rule in a committed file: the repository scan passes the host
// after the «@» to the identifier, and the password is never part of it.
func TestSupabaseDatabaseURLCommitted(t *testing.T) {
	const pass = "Zq9-PlantedDBPassword-77"
	content := []byte("DATABASE_URL=postgresql://postgres:" + pass + "@db." + supabaseRef + ".supabase.co:5432/postgres\n")
	var hosts []string
	id := graph.Identifier(testSalt)
	ms := secrets.MatchRefs(content, secrets.BuiltinPatterns(), nil, func(pid, secret, host string) secrets.Ref {
		hosts = append(hosts, host)
		return id(pid, secret, host)
	})
	if len(ms) != 1 || ms[0].Ref == nil {
		t.Fatalf("matches %+v", ms)
	}
	if r := ms[0].Ref; r.Service != "supabase" || r.ServicePath != "https://"+supabaseRef+".supabase.co" {
		t.Errorf("ref %+v", r)
	}
	for _, h := range hosts {
		if strings.Contains(h, pass) {
			t.Errorf("the host passed on carries the password: %q", h)
		}
	}
	if secrets.DBHostAfter("postgres://u:p@", 15) != "" {
		t.Error("no host after the «@»")
	}
}
