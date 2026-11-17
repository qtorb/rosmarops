package pipeline

import (
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// Profile is in with one measured profile of Claude Code (CTO-50 §1, BV-236
// §4) added to Claude Code's user settings, the way Claude Code merges a
// pasted block. Every other agent keeps its configuration. ok is false when
// there is no Claude Code configuration folder to apply it to.
func Profile(in Input, id string) (Input, bool) {
	s, ok := profiles.Apply(in.Settings, id, in.RepoRoots...)
	if !ok {
		return in, false
	}
	out := in
	out.Settings = s
	return out, true
}

// DeclaredProfiles are the measured profiles of the agents whose settings are
// declared (BV-248 decision 9, CTO-56 §3): Cursor's P0 and P1 and Grok Bot's
// «Only the cloud» and «With your computer», as the answers each profile
// sets. The other answers stay the person's.
var DeclaredProfiles = []struct {
	Product, ID string
	Answers     map[string]string
}{
	{declared.Cursor, profiles.P0, map[string]string{declared.C1RunMode: declared.ModeAllowlist, declared.C2Allowlist: declared.ListEmpty, declared.C3ReadAccess: declared.ReadWorkspace, declared.C4ExternalFileOn: declared.On}},
	{declared.Cursor, profiles.P1, map[string]string{declared.C1RunMode: declared.ModeAllowlist, declared.C2Allowlist: declared.ListNoReads, declared.C3ReadAccess: declared.ReadWorkspace, declared.C4ExternalFileOn: declared.On}},
	{declared.GrokBot, "cloud", map[string]string{declared.B0Uses: declared.Yes, declared.B1LocalExecution: declared.LocalNever, declared.B2AutoReview: declared.On, declared.B3AskFirst: declared.AskFirstAll}},
	{declared.GrokBot, "local", map[string]string{declared.B0Uses: declared.Yes, declared.B1LocalExecution: declared.LocalAsk, declared.B2AutoReview: declared.On, declared.B3AskFirst: declared.AskFirstAll}},
}

// DeclaredProfile is in with a declared profile's answers in place of the
// person's for that product. For Grok Bot, its settings.json is not read in
// the pass (the profile sets B1 and B2); its app folder is kept. ok is false
// when the product is not on this computer.
func DeclaredProfile(in Input, product, id string) (Input, bool) {
	for _, p := range DeclaredProfiles {
		if p.Product != product || p.ID != id {
			continue
		}
		out := in
		answers := map[string]string{}
		for k, v := range in.Declared[product] {
			answers[k] = v
		}
		for k, v := range p.Answers {
			answers[k] = v
		}
		decl := map[string]map[string]string{}
		for k, v := range in.Declared {
			decl[k] = v
		}
		decl[product] = answers
		out.Declared = decl
		switch product {
		case declared.Cursor:
			if !in.Cursor.Exists {
				return in, false
			}
			out.Cursor.Declared = answers
		case declared.GrokBot:
			if !declared.ResolveGrokBot(graph.GrokBotFacts(in.GrokBot), in.Declared[product]).Present {
				return in, false
			}
			out.GrokBot = platform.GrokBotScan{Present: true}
		}
		return out, true
	}
	return in, false
}

// GrokProfile is in with a measured Grok Build template (BV-249 §3.2: P2)
// pasted in its user config.toml; every other agent keeps its configuration.
// ok is false when there is no Grok Build folder or the template is not
// measured.
func GrokProfile(in Input, id string) (Input, bool) {
	if !in.Grok.Exists {
		return in, false
	}
	s, ok := profiles.GrokApply(in.Grok, id)
	if !ok {
		return in, false
	}
	out := in
	out.Grok = s
	return out, true
}
