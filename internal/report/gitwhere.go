package report

import (
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// committedPlace is one committed_in edge as the «where» line of cred_in_git
// shows it (BV-219): the repository's root, the path inside it and, for a key
// only in the history, the short commit that added it.
type committedPlace struct {
	repo, file, commit string
	tracked            bool
}

// committedPlaces are the committed_in edges of g by edge id. The edge's
// source is "<file>:<line>" when tracked and "<short commit>:<file>" when
// history_only (graph.committedSource).
func committedPlaces(g graph.Graph) map[string]committedPlace {
	repos := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindResource && n.Type == graph.ResourceGitRepository {
			repos[n.ID] = n.Path
		}
	}
	out := map[string]committedPlace{}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeCommittedIn {
			continue
		}
		p := committedPlace{repo: repos[e.To], tracked: e.Presence == finding.PresenceTracked}
		if p.tracked {
			p.file = e.Source
			if i := strings.LastIndexByte(e.Source, ':'); i > 0 {
				p.file = e.Source[:i]
			}
		} else if i := strings.IndexByte(e.Source, ':'); i > 0 {
			p.commit, p.file = e.Source[:i], e.Source[i+1:]
			if len(p.commit) > 7 {
				p.commit = p.commit[:7]
			}
		} else {
			p.file = e.Source
		}
		out[e.ID] = p
	}
	return out
}

// gitPlaces are the committed_in places of a cred_in_git finding, tracked
// ones first, each kind by repository and file. Empty for any other rule or
// when the report carries no graph (read back from report.v2.json).
func (c htmlCtx) gitPlaces(f rules.Finding) []committedPlace {
	if f.Rule != rules.RuleCredInGit {
		return nil
	}
	var out []committedPlace
	for _, id := range f.Evidence.Edges {
		if p, ok := c.v.committed[id]; ok {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.tracked != b.tracked {
			return a.tracked
		}
		if a.repo != b.repo {
			return a.repo < b.repo
		}
		return a.file < b.file
	})
	return out
}

// gitHistoryOnly reports a cred_in_git finding whose every repository has
// the key only in its history: none tracks it today (BV-219). A tracked
// place anywhere wins: the title then says it is saved in git.
func (c htmlCtx) gitHistoryOnly(f rules.Finding) bool {
	ps := c.gitPlaces(f)
	return len(ps) > 0 && !ps[0].tracked
}

// findingWhere writes the «where» line of a finding: for cred_in_git with
// its committed places, html.f.cred_in_git.where.tracked or .where.history
// of the first one (a tracked one first), then how many more; for any other
// finding, the «Found in» line of its locations (BV-153).
func (c htmlCtx) findingWhere(f rules.Finding) {
	ps := c.gitPlaces(f)
	if len(ps) == 0 {
		c.whereLine(c.rowLocations(f))
		return
	}
	first := ps[0]
	key := "html.f.cred_in_git.where.tracked"
	if !first.tracked {
		key = "html.f.cred_in_git.where.history"
	}
	p := map[string]string{
		"project": userPath(first.repo, c.home, "~"),
		"file":    untrusted.PlainText(first.file),
		"commit":  untrusted.PlainText(first.commit),
	}
	c.w(`<p class="where">%s`, c.withCode(key, p, "file", "commit"))
	if len(ps) > 1 {
		c.w(` %s`, c.t("html.tech.more", map[string]string{"count": strconv.Itoa(len(ps) - 1)}))
	}
	c.w(`</p>`)
}

// withCode is key's text, escaped, with the values of names inside <code>.
func (c htmlCtx) withCode(key string, p map[string]string, names ...string) string {
	q := copyParams(p)
	for i, n := range names {
		q[n] = "\x00" + strconv.Itoa(i) + "\x00"
	}
	out := html.EscapeString(c.cat.Text(key, q))
	for i, n := range names {
		out = strings.ReplaceAll(out, "\x00"+strconv.Itoa(i)+"\x00", "<code>"+html.EscapeString(p[n])+"</code>")
	}
	return out
}
