package graph

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// AssumeStartupFileRunsLater is the assumption of a way through a startup
// file (BV-249, CTO-59 §4.3): what the agent leaves there runs later, outside
// its control.
const AssumeStartupFileRunsLater = "startup_file_runs_later"

// startupTargets are the startup files of HostOS under home, as paths the
// inference can test: a folder entry gets a synthetic file name in it.
func startupTargets(home string) []string {
	files, err := knowledge.StartupFiles(HostOS)
	if err != nil || home == "" {
		return nil
	}
	var out []string
	for _, f := range files {
		rel := strings.TrimPrefix(f.Path, "~/")
		if strings.HasSuffix(rel, "/") {
			rel += probeName
		}
		p := home
		for _, seg := range strings.Split(strings.TrimSuffix(rel, "/"), "/") {
			p = joinLike(p, seg)
		}
		out = append(out, p)
	}
	return out
}

// startupReaches is rule (b-iii) (BV-249, CTO-59 §4.3): an agent that writes a
// startup file without a person reaches every credential of the computer,
// because what it writes there runs later and can read them. One reaches edge
// per credential and way, through the capability that writes the file, with
// the way's assumptions and startup_file_runs_later. ways gives the write
// ways of a file: (capability, assumptions).
func startupReaches(client Node, all []Node, home string, ways func(file string) []startupWay) []Edge {
	seen := map[string]startupWay{}
	var order []string
	for _, f := range startupTargets(home) {
		for _, w := range ways(f) {
			if _, ok := seen[w.cap.ID]; !ok {
				seen[w.cap.ID] = w
				order = append(order, w.cap.ID)
			}
		}
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		for _, id := range order {
			w := seen[id]
			out = append(out, Edge{
				ID:          "edge_" + EdgeReaches + "_" + keyHash(EdgeReaches, client.ID, cred.ID, w.cap.ID, AssumeStartupFileRunsLater),
				Type:        EdgeReaches,
				From:        client.ID,
				To:          cred.ID,
				Via:         w.cap.ID,
				Source:      w.cap.Source,
				Certainty:   CertaintyInferred,
				DerivedFrom: []string{client.ID, w.cap.ID, cred.ID},
				Assumptions: append(append([]string{}, w.assumptions...), AssumeStartupFileRunsLater),
			})
		}
	}
	return out
}

// startupWay is a way to write a startup file: its capability and assumptions.
type startupWay struct {
	cap         Node
	assumptions []string
}
