package mcp

import (
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/state"
)

// Binding ties each allow_list.mcp_servers entry to the destination the
// server had when the entry was accepted (decision 2026-10-27, blindaje,
// B9). The first run that sees an entry apply records the server's
// destination_ref (the v2 graph's, BV-91) in state.baselines.mcp_servers.
// If the server then points somewhere else, the entry stops applying and the
// warning returns, until the entry is accepted again: a new accepted_at,
// later than the recorded first_seen, records the new destination. The
// records of a server stay while its entry is in allow_list, even in a run
// that does not see the server; removing the entry removes them.
//
// Without the salt no destination can be checked: no entry applies
// (fail-secure).
type Binding struct {
	// Salt is the machine-local salt that keys destination_ref.
	Salt []byte

	// Baseline is state.baselines.mcp_servers as read.
	Baseline []state.MCPServerBaseline

	// Now dates the destinations recorded in this run.
	Now time.Time
}

// bind returns, by index in current, whether the allow-list entry of each
// allow-listed server applies, and the new baseline: one entry per
// allow-listed server, the recorded destination kept or a new one recorded.
func (b Binding) bind(current []platform.MCPServer, allow map[string]config.AllowEntryID) (map[int]bool, []state.MCPServerBaseline) {
	applies := map[int]bool{}
	if len(b.Salt) == 0 {
		return applies, b.Baseline // nothing checked, nothing forgotten
	}
	type key struct{ client, name string }
	recorded := map[key][]state.MCPServerBaseline{}
	for _, e := range b.Baseline {
		if e.DestinationRef != "" {
			k := key{strings.ToLower(e.Client), strings.ToLower(e.Name)}
			recorded[k] = append(recorded[k], e)
		}
	}
	stamp := b.Now.UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	var out []state.MCPServerBaseline
	keep := func(e state.MCPServerBaseline) {
		if id := e.Client + "\x00" + e.Name + "\x00" + e.DestinationRef; !seen[id] {
			seen[id] = true
			out = append(out, e)
		}
	}
	present := map[key]bool{}
	for i, srv := range current {
		if !srv.LoadsMCP {
			continue
		}
		k := key{strings.ToLower(srv.Client), strings.ToLower(srv.Name)}
		a, ok := allow[k.client+":"+k.name]
		if !ok {
			continue
		}
		present[k] = true
		ref := graph.DestinationRef(b.Salt, srv)
		if ref == "" {
			continue // nothing to bind to: the entry does not apply
		}
		prev := recorded[k]
		rec := state.MCPServerBaseline{
			Client: srv.Client, Name: srv.Name, CommandSHA256: b.commandRef(srv),
			DestinationRef: ref, FirstSeen: stamp,
		}
		switch {
		case len(prev) == 0 || reaccepted(a.AcceptedAt, prev):
			applies[i] = true
			keep(rec)
		case hasRef(prev, ref):
			applies[i] = true
			for _, e := range prev {
				if e.DestinationRef == ref {
					keep(e)
				}
			}
		default:
			// It points somewhere else since the entry was accepted: the
			// entry does not apply, and the accepted destination stays.
			for _, e := range prev {
				keep(e)
			}
		}
	}
	// A server absent from this run keeps its accepted destinations while
	// its entry stays in allow_list (BV-94b): coming back pointing
	// elsewhere, it is warned about again.
	for k, prev := range recorded {
		if _, ok := allow[k.client+":"+k.name]; ok && !present[k] {
			for _, e := range prev {
				keep(e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Client != y.Client {
			return x.Client < y.Client
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.DestinationRef < y.DestinationRef
	})
	return applies, out
}

// reaccepted reports whether the entry was accepted (accepted_at) after the
// destinations were recorded.
func reaccepted(acceptedAt string, prev []state.MCPServerBaseline) bool {
	at, err := time.Parse(time.RFC3339, acceptedAt)
	if err != nil {
		return false
	}
	for _, e := range prev {
		if seen, err := time.Parse(time.RFC3339, e.FirstSeen); err != nil || !at.After(seen) {
			return false
		}
	}
	return true
}

func hasRef(prev []state.MCPServerBaseline, ref string) bool {
	for _, e := range prev {
		if e.DestinationRef == ref {
			return true
		}
	}
	return false
}

// commandRef is the state's command_sha256: HMAC-SHA256 with the local
// salt of the command, its arguments and the env key names, never a bare
// SHA-256 (arguments may hold a secret).
func (b Binding) commandRef(srv platform.MCPServer) string {
	parts := append([]string{srv.Command}, srv.Args...)
	parts = append(parts, srv.EnvKeys...)
	return hash.Keyed(b.Salt, []byte(strings.Join(parts, "\x00")))
}
