package graph

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Identifier is the Identify of the repository scan (decision 2026-10-24,
// memoria, §2.2): the salted fingerprint of a secret and the service it
// opens, as the credential collector computes them, so the same key in a
// repository and in a .env file is the same node. The value is used only
// here and never kept.
func Identifier(salt []byte) secrets.Identify {
	return func(patternID, secret string) secrets.Ref {
		credType, service, known := CredentialKind(patternID)
		if !known {
			credType, service = CredentialGenericSecret, ServiceUnknown
		}
		ident := identifyService(patternID, service, secret)
		if ident.observed {
			credType = CredentialAPIKey
		}
		return secrets.Ref{
			Fingerprint: Fingerprint(salt, secret), CredType: credType, Service: ident.service,
			ServicePath: ident.path, ObservedScope: ident.observedScope, Observed: ident.observed,
		}
	}
}

// GitRepositoryID is the id of a git_repository resource: type + canonical
// root.
func GitRepositoryID(root string) string {
	return "res_git_repository_" + keyHash(ResourceGitRepository, canonicalPath(root))
}

// committedInputs are the committed secrets of the repository findings, as
// credential inputs: a tracked one with its file on disk as location
// ("<repo>/<file>:<line>"), a history-only one without location.
func committedInputs(fs []finding.Finding) []credentialInput {
	var out []credentialInput
	for _, f := range fs {
		for i := range f.Committed {
			c := f.Committed[i]
			in := credentialInput{committed: &c}
			if c.Presence == finding.PresenceTracked {
				in.location = filepath.Join(c.Repo, filepath.FromSlash(c.File)) + ":" + strconv.Itoa(c.Line)
			}
			out = append(out, in)
		}
	}
	return out
}

// committedSource is the source of a committed_in edge: file:line for
// tracked, <short commit>:<file> for history_only.
func committedSource(c *finding.CommittedSecret) string {
	if c.Presence == finding.PresenceTracked {
		return c.File + ":" + strconv.Itoa(c.Line)
	}
	short := c.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return short + ":" + c.File
}

// committedInput adds one committed secret: its credential node (or the
// existing one with its fingerprint), its location when tracked, the
// git_repository resource and the committed_in edge, one per credential
// and repository: tracked when any occurrence is in the current tree, with
// the smallest source of that presence; has_remote when the repository
// declares a remote.
func committedInput(creds map[string]*Node, identities map[string]serviceIdentity, locations map[string]map[string]bool,
	edges map[string]Edge, repos map[string]Node, c *finding.CommittedSecret, loc string, families map[string]string, seen string,
) {
	r := c.Ref
	id := "cred_" + keyHash(r.Fingerprint)
	if _, ok := creds[id]; !ok {
		creds[id] = &Node{ID: id, Kind: KindCredential, Type: r.CredType, Service: r.Service, Fingerprint: r.Fingerprint, DiscoveredAt: seen}
		identities[id] = serviceIdentity{service: r.Service, path: r.ServicePath, observedScope: r.ObservedScope, observed: r.Observed}
		locations[id] = make(map[string]bool)
	}
	if loc != "" {
		locations[id][loc] = true
	}
	rid := GitRepositoryID(c.Repo)
	if _, ok := repos[rid]; !ok {
		family := families[c.Repo]
		if family == "" {
			family = c.Repo
		}
		repos[rid] = Node{ID: rid, Kind: KindResource, Type: ResourceGitRepository, Path: c.Repo, Family: family, DiscoveredAt: seen}
	}
	eid := edgeID(EdgeCommittedIn, id, rid)
	src := committedSource(c)
	e, ok := edges[eid]
	switch {
	case !ok:
		e = Edge{ID: eid, Type: EdgeCommittedIn, From: id, To: rid, Source: src, Certainty: CertaintyObserved, Presence: c.Presence}
	case e.Presence == finding.PresenceHistoryOnly && c.Presence == finding.PresenceTracked:
		e.Presence, e.Source = c.Presence, src
	case e.Presence == c.Presence && src < e.Source:
		e.Source = src
	}
	e.HasRemote = e.HasRemote || c.HasRemote
	edges[eid] = e
}

// firstSource is the source of an authenticates_to edge: the credential's
// first location, or, for a key only in git history, its smallest
// committed_in source.
func firstSource(locs []string, edges map[string]Edge, credID string) string {
	if len(locs) > 0 {
		return locs[0]
	}
	var srcs []string
	for _, e := range edges {
		if e.Type == EdgeCommittedIn && e.From == credID {
			srcs = append(srcs, e.To+"#"+e.Source)
		}
	}
	sort.Strings(srcs)
	if len(srcs) == 0 {
		return ""
	}
	_, src, _ := strings.Cut(srcs[0], "#")
	return src
}
