package rules

import (
	"encoding/json"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
)

// RuleCredInGit is a credential committed in a git repository (decision
// 2026-10-24, memoria, §2.4, M8): it has no agent; the key is the
// credential.
const RuleCredInGit = "cred_in_git"

// ActionRotateCredential is the only action that cuts cred_in_git: rotate
// the key at its provider and record it in allow_list.rotated_credentials
// (M9). Complete, magnitude 3.
const ActionRotateCredential = "rotate_credential"

// AssumeRemoteVisibilityUnknown: a remote is declared, not whether it is
// public or the commits were pushed (§2.2).
const AssumeRemoteVisibilityUnknown = "remote_visibility_unknown"

// PostureEnvFilesTracked counts the .env files tracked by git without any
// recognisable secret (§2.1).
const PostureEnvFilesTracked = "posture.env_files_tracked"

// States of a test-data entry (M10).
const (
	FixtureApplied          = "applied"
	FixtureExpired          = "expired"
	FixtureIgnoredScope     = "ignored_scope"     // guard 1
	FixtureFingerprintMatch = "fingerprint_match" // guard 2
)

// Silenced is the test data the user silenced (M10–M11): the findings it
// silences do not count in the score and are always shown.
type Silenced struct {
	Count   int               `json:"count"`
	Entries []SilencedEntry   `json:"entries"`
	Expired int               `json:"-"`
	Guards  []FixtureGuardHit `json:"-"`
}

// SilencedEntry is one allow_list.test_fixtures entry and what it did.
type SilencedEntry struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Note      string `json:"note"`
	ExpiresOn string `json:"expires_on"`
	State     string `json:"state"`
	Count     int    `json:"count"`
}

// TestData are the credentials treated as test data by default (decision
// 2026-10-26, ruido del informe, point 1): every location of the key is in
// a test or template file. They give no cred_in_git finding and do not
// count in the score; they are always shown, with their files.
type TestData struct {
	Count int
	Files []TestDataFile
}

// TestDataFile is one file of a key treated as test data.
type TestDataFile struct {
	Repo string
	Path string
}

// testDirs are the path segments of a test file (point 1), any case.
var testDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "testdata": true, "fixtures": true,
	"fixture": true, "__fixtures__": true, "spec": true, "specs": true,
}

// testDataNames are the file names of a test or template file (point 1).
var testDataNames = []string{
	"*_test.*", "*.test.*", "*.spec.*", "test_*.py",
	".env.example", ".env.sample", ".env.template", "template.env", "*.example", "*.sample", "*.template",
}

// isTestDataFile reports a repository-relative file that is a test or
// template file by the criterion of point 1: a test segment, or a test or
// template name. Compared without case, as Windows names are.
func isTestDataFile(rel string) bool {
	segs := strings.Split(strings.ToLower(strings.ReplaceAll(rel, `\`, "/")), "/")
	for _, s := range segs {
		if testDirs[s] {
			return true
		}
	}
	for _, pat := range testDataNames {
		if ok, _ := path.Match(pat, segs[len(segs)-1]); ok {
			return true
		}
	}
	return false
}

// repoRelative is the repository and repository-relative path of an
// absolute file inside one of roots.
func repoRelative(roots []string, file string) (string, string, bool) {
	cf := graph.CanonicalPath(file)
	for _, r := range roots {
		cr := strings.TrimSuffix(graph.CanonicalPath(r), "/")
		if cr != "" && strings.HasPrefix(cf, cr+"/") {
			return r, strings.ReplaceAll(file, `\`, "/")[len(cr)+1:], true
		}
	}
	return "", "", false
}

// FixtureGuardHit is one guard that kept an entry from applying somewhere:
// FixtureIgnoredScope or FixtureFingerprintMatch, with the entry.
type FixtureGuardHit struct {
	Guard string
	Repo  string
	Path  string
}

// agentConfigFiles are the repository-relative agent configuration files
// a test-data entry never silences (guard 1), besides .env* files and the
// files the credential collector reads.
var agentConfigFiles = map[string]bool{
	".mcp.json": true, ".claude/settings.json": true, ".claude/settings.local.json": true,
	".cursor/mcp.json": true, ".cursor/sandbox.json": true, ".cursor/cli.json": true, ".cursor/permissions.json": true,
	".cursor/hooks.json": true, ".vscode/settings.json": true, ".vscode/mcp.json": true,
}

// occurrence is one committed secret, by credential.
type occurrence struct {
	finding.CommittedSecret
	cred string
}

// gitInput is the part of Input cred_in_git reads.
func (in Input) occurrences(g graph.Graph) map[string][]occurrence {
	out := map[string][]occurrence{}
	for _, c := range in.Committed {
		id := "cred_" + graph.KeyHash(c.Ref.Fingerprint)
		out[id] = append(out[id], occurrence{c, id})
	}
	if len(in.Committed) > 0 {
		return out
	}
	// Without the scan's occurrences, the committed_in edges themselves.
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeCommittedIn {
			continue
		}
		c := finding.CommittedSecret{Repo: nodes[e.To].Path, Presence: e.Presence, HasRemote: e.HasRemote}
		src := e.Source
		if e.Presence == finding.PresenceHistoryOnly {
			_, src, _ = strings.Cut(src, ":")
		} else if i := strings.LastIndexByte(src, ':'); i > 0 {
			src = src[:i]
		}
		c.File = src
		out[e.From] = append(out[e.From], occurrence{c, e.From})
	}
	return out
}

// credentialSource reports a committed file guard 1 protects: a .env*
// file, an agent configuration file, or a file the credential collector
// reads (a location of a credential that is not one of its tracked
// commits).
func credentialSource(o occurrence, collectorFiles map[string]bool) bool {
	base := strings.ToLower(path.Base(o.File))
	if strings.HasPrefix(base, ".env") || agentConfigFiles[strings.ToLower(o.File)] {
		return true
	}
	return collectorFiles[graph.CanonicalPath(filepath.Join(o.Repo, filepath.FromSlash(o.File)))]
}

// gitFindings evaluates cred_in_git (§2.4) and the test-data entries
// (§3): one finding per committed credential not declared rotated; red
// for a high class, yellow for medium or unclassified; a low credential or
// one committed only in templates is posture. A finding every occurrence
// of which a test-data entry covers, with no other location of its key, is
// silenced instead; one whose every location is a test or template file is
// test data by default (decision 2026-10-26, point 1), counted in TestData.
// The map returned holds the keys of both: they give no finding at all.
func gitFindings(in Input, nodes map[string]graph.Node, scopes map[string]string, observed map[string]bool, addPosture func(code, cred, class string)) ([]Finding, Silenced, TestData, map[string]bool) {
	silencedCreds := map[string]bool{}
	g := in.Graph
	occ := in.occurrences(g)
	rotated := map[string]bool{}
	for _, r := range in.Rotated {
		rotated[r.ID] = true
	}
	edges := map[string][]graph.Edge{}
	for _, e := range g.Edges {
		if e.Type == graph.EdgeCommittedIn {
			edges[e.From] = append(edges[e.From], e)
		}
	}
	// Files the collector reads: credential locations that are not a
	// tracked commit.
	tracked := map[string]bool{}
	for _, os := range occ {
		for _, o := range os {
			if o.Presence == finding.PresenceTracked {
				tracked[o.Repo+"\x00"+o.File+"\x00"+strconv.Itoa(o.Line)] = true
			}
		}
	}
	roots := append([]string(nil), in.RepoRoots...)
	for _, os := range occ {
		for _, o := range os {
			roots = append(roots, o.Repo)
		}
	}
	collectorFiles := map[string]bool{}
	collectorLoc := map[string]bool{}            // credential → has a location outside its commits
	collectorOther := map[string]bool{}          // … one that is not a test or template file in a repository
	collectorTest := map[string][]TestDataFile{} // … the ones that are
	for _, n := range g.Nodes {
		if n.Kind != graph.KindCredential {
			continue
		}
		mine := map[string]bool{}
		for _, o := range occ[n.ID] {
			if o.Presence == finding.PresenceTracked {
				mine[filepath.Join(o.Repo, filepath.FromSlash(o.File))+":"+strconv.Itoa(o.Line)] = true
			}
		}
		for _, loc := range n.Locations {
			if mine[loc] {
				continue
			}
			collectorLoc[n.ID] = true
			for _, f := range graph.CredentialFiles(graph.Node{Locations: []string{loc}}) {
				collectorFiles[graph.CanonicalPath(f)] = true
			}
			if repo, rel, ok := repoRelative(roots, memberFile(loc)); ok && isTestDataFile(rel) {
				collectorTest[n.ID] = append(collectorTest[n.ID], TestDataFile{Repo: repo, Path: rel})
			} else {
				collectorOther[n.ID] = true
			}
		}
	}

	today := in.Now.Format("2006-01-02")
	var sil Silenced
	var td TestData
	tdFiles := map[TestDataFile]bool{}
	type entryHits struct {
		count     int
		scope, fp bool
	}
	hits := make([]entryHits, len(in.Fixtures))
	active := make([]bool, len(in.Fixtures))
	for i, e := range in.Fixtures {
		active[i] = e.ExpiresOn >= today
		if !active[i] {
			sil.Expired++
		}
	}
	covers := func(e config.AllowEntryTestFixture, o occurrence) bool {
		return graph.CanonicalPath(e.Repo) == graph.CanonicalPath(o.Repo) && graph.MatchRelGlob(e.Path, o.File)
	}

	var out []Finding
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		cred := nodes[id]
		if rotated[id] {
			continue
		}
		class := classOf(cred, scopes[id])
		os := occ[id]
		allTpl := len(os) > 0
		for _, o := range os {
			allTpl = allTpl && isTemplate(o.File)
		}
		// Test data by default (decision 2026-10-26, point 1): every
		// location in git and in the collector is a test or template file.
		testData := len(os) > 0 && !collectorOther[id]
		for _, o := range os {
			testData = testData && isTestDataFile(o.File)
		}
		countTestData := func() { // no finding at all, as a silenced key
			td.Count++
			silencedCreds[id] = true
			for _, o := range os {
				tdFiles[TestDataFile{Repo: o.Repo, Path: o.File}] = true
			}
			for _, f := range collectorTest[id] {
				tdFiles[f] = true
			}
		}
		if class == ClassLow || allTpl {
			if allTpl {
				addPosture(PostureInTemplatesOnly, id, class)
				if class != ClassLow && testData {
					countTestData()
				}
			}
			continue
		}
		// Test data: every occurrence covered by an active entry, none a
		// credential source (guard 1), and the key nowhere else (guard 2).
		matched := map[int]bool{}
		allCovered := len(os) > 0
		for _, o := range os {
			covered := false
			for i, e := range in.Fixtures {
				if !active[i] || !covers(e, o) {
					continue
				}
				if credentialSource(o, collectorFiles) {
					hits[i].scope = true
					continue
				}
				matched[i], covered = true, true
			}
			allCovered = allCovered && covered
		}
		if len(matched) > 0 {
			if allCovered && !collectorLoc[id] {
				sil.Count++
				silencedCreds[id] = true
				for i := range matched {
					hits[i].count++
				}
				continue
			}
		}
		if testData {
			countTestData()
			continue
		}
		for i := range matched {
			hits[i].fp = true // guard 2: the same key outside test data
		}
		out = append(out, gitFinding(in, cred, edges[id], nodes, scopes[id], class, observed[id], today))
	}

	for i, e := range in.Fixtures {
		state := FixtureApplied
		switch {
		case !active[i]:
			state = FixtureExpired
		case hits[i].count > 0:
		case hits[i].fp:
			state = FixtureFingerprintMatch
		case hits[i].scope:
			state = FixtureIgnoredScope
		}
		sil.Entries = append(sil.Entries, SilencedEntry{Repo: e.Repo, Path: e.Path, Note: e.Note, ExpiresOn: e.ExpiresOn, State: state, Count: hits[i].count})
		if active[i] && hits[i].scope {
			sil.Guards = append(sil.Guards, FixtureGuardHit{Guard: FixtureIgnoredScope, Repo: e.Repo, Path: e.Path})
		}
		if active[i] && hits[i].fp {
			sil.Guards = append(sil.Guards, FixtureGuardHit{Guard: FixtureFingerprintMatch, Repo: e.Repo, Path: e.Path})
		}
	}
	if sil.Entries == nil {
		sil.Entries = []SilencedEntry{}
	}
	for f := range tdFiles {
		td.Files = append(td.Files, f)
	}
	sort.Slice(td.Files, func(i, j int) bool {
		a, b := td.Files[i], td.Files[j]
		return a.Repo < b.Repo || a.Repo == b.Repo && a.Path < b.Path
	})
	return out, sil, td, silencedCreds
}

// gitFinding is the cred_in_git finding of one credential: its repositories
// grouped by family, the rotate_credential action.
func gitFinding(in Input, cred graph.Node, edges []graph.Edge, nodes map[string]graph.Node, scope, class string, observed bool, today string) Finding {
	sev := SeverityYellow
	if class == ClassHigh {
		sev = SeverityRed
	}
	var repos, edgeIDs, locations, assumptions []string
	remote := false
	var leg []way
	for _, e := range edges {
		repo := nodes[e.To].Path
		repos = append(repos, repo)
		edgeIDs = append(edgeIDs, e.ID)
		remote = remote || e.HasRemote
		leg = append(leg, way{id: e.ID, code: "committed", target: cred.ID})
		if e.Presence == finding.PresenceHistoryOnly {
			locations = append(locations, repo)
		}
	}
	for _, loc := range cred.Locations {
		for _, r := range repos {
			if graph.Within(r, memberFile(loc)) {
				locations = append(locations, loc)
				break
			}
		}
	}
	if remote {
		assumptions = append(assumptions, AssumeRemoteVisibilityUnknown)
	}
	repoNodes := make([]string, 0, len(edges))
	for _, e := range edges {
		repoNodes = append(repoNodes, e.To)
	}
	f := Finding{
		ID: findingID(RuleCredInGit, cred.ID), Rule: RuleCredInGit, RuleVersion: RuleVersion,
		Severity: sev, Confidence: ConfidenceHigh, class: class, credType: cred.Type, credID: cred.ID, observed: observed,
		legs: [][]way{leg},
		Params: Params{
			Service: cred.Service, Scope: scope, Class: class, Count: len(sortedUnique(repos)),
			CredID: cred.ID, CredType: cred.Type, HasRemote: &remote,
		},
		Evidence: Evidence{
			Nodes:       append([]string{cred.ID}, sortedUnique(repoNodes)...),
			Edges:       sortedUnique(edgeIDs),
			Locations:   sortedUnique(locations),
			Assumptions: sortedUnique(assumptions),
		},
	}
	f.Group = group(in, repos)
	value, _ := json.MarshalIndent(map[string]string{"id": cred.ID, "date": today, "note": ""}, "", "  ")
	act := Action{
		Code: ActionRotateCredential, File: in.ConfigFile, FileExists: in.ConfigFile != "",
		Key: "allow_list.rotated_credentials", Value: string(value),
		Completeness: CompletenessFull, Magnitude: 3, Cuts: sortedUnique(edgeIDs), points: 2, files: 1,
	}
	act.ID = actionID(act)
	f.Actions = []Action{act}
	return f
}

// IsMachineFinding reports a finding with no agent (cred_in_git): it
// weighs in every evaluated agent's score.
func IsMachineFinding(f Finding) bool { return f.Rule == RuleCredInGit }
