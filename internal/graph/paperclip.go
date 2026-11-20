package graph

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Paperclip in the graph (CTO-75 §5, BV-264). Each instance is an
// orchestrator agent; RosmarOps cannot read which agents it has configured
// (that lives in its database), so what it can do comes from its product
// defaults in the knowledge base:
//   - a launches edge to each installed agent Paperclip supports, inferred,
//     with the assumption orchestrator_agent_configured;
//   - its secrets (secrets/master.key next to db/, .env, and llm.apiKey and
//     database.connectionString when set) as credentials identified by their
//     path: never opened, never read; any agent that reads them reaches them,
//     so the existing cred_readable applies;
//   - its API port as an inbound_channel on api_server that wakes it, and its
//     heartbeat scheduler (on by default) as an agent_schedule that wakes it,
//     inferred from that default;
//   - writes_config_of to the skills of the agents it writes skills for.

// AssumeOrchestratorAgentConfigured: a launches edge assumes the
// orchestrator has the agent configured, which is only readable in its
// database.
const AssumeOrchestratorAgentConfigured = "orchestrator_agent_configured"

// PaperclipChannelPlatform is the inbound_channel platform of Paperclip's API.
const PaperclipChannelPlatform = "api_server"

// PaperclipAgentID is the orchestrator node of a Paperclip instance folder.
func PaperclipAgentID(instanceDir string) string {
	return "agent_orchestrator_" + keyHash(AgentOrchestrator, knowledge.PaperclipProduct, canonicalPath(instanceDir))
}

// PathFingerprint is the fingerprint of a credential identified by where it
// is: the salted hash of its normalized location (a file, or file#pointer),
// never of its content.
func PathFingerprint(salt []byte, location string) string {
	file, pointer, _ := strings.Cut(location, "#")
	norm := canonicalPath(file)
	if pointer != "" {
		norm += "#" + pointer
	}
	return Fingerprint(salt, "path:"+norm)
}

// AddPaperclip adds scan's instances to f. Agents and skills must already be
// in f (launches and writes_config_of point at them). kb is the Paperclip
// base; salt the machine's salt.
func AddPaperclip(f Fragment, scan platform.PaperclipScan, kb knowledge.PaperclipBase, home string, salt []byte, now time.Time) Fragment {
	if len(scan.Instances) == 0 {
		return f
	}
	seen := now.UTC().Format(time.RFC3339)
	b := newFragmentBuilder()
	for _, in := range scan.Instances {
		id := PaperclipAgentID(in.Dir)
		b.node(Node{
			ID: id, Kind: KindAgent, Type: AgentOrchestrator, Name: clientDisplayName(knowledge.PaperclipProduct),
			Platform: knowledge.PaperclipProduct, ConfigPath: in.Dir, DiscoveredAt: seen,
		})
		for _, c := range paperclipCredentials(in, kb, salt, seen) {
			b.node(c)
		}
		if in.ConfigStatus == platform.MCPSourceRead {
			ch := Node{
				ID:   "res_inbound_channel_" + keyHash(ResourceInboundChannel, id, PaperclipChannelPlatform, canonicalPath(in.ConfigPath)),
				Kind: KindResource, Type: ResourceInboundChannel, Path: in.ConfigPath, DiscoveredAt: seen,
				Platform: PaperclipChannelPlatform, Status: "configured", Origin: "config", Approvers: paperclipApprovers(in, scan.Service),
			}
			b.node(ch)
			b.edge(Edge{ID: edgeID(EdgeWakes, ch.ID, id), Type: EdgeWakes, From: ch.ID, To: id, Source: in.ConfigPath + "#/server", Certainty: CertaintyObserved})
		}
		if d, ok := kb.Default("heartbeat_scheduler"); ok && d.Value == "on" {
			sch := Node{
				ID:   "res_agent_schedule_" + keyHash(ResourceAgentSchedule, id, "heartbeat"),
				Kind: KindResource, Type: ResourceAgentSchedule, Path: in.Dir, DiscoveredAt: seen,
			}
			b.node(sch)
			b.edge(Edge{ID: edgeID(EdgeWakes, sch.ID, id), Type: EdgeWakes, From: sch.ID, To: id, Source: kb.Ref("heartbeat_scheduler"), Certainty: CertaintyInferred})
		}
		for _, n := range f.Nodes {
			if n.Kind != KindAgent || n.Type != AgentAIClient {
				continue
			}
			a, ok := kb.Adapter(n.Platform)
			if !ok {
				continue
			}
			b.edge(Edge{
				ID: edgeID(EdgeLaunches, id, n.ID), Type: EdgeLaunches, From: id, To: n.ID,
				Source: kb.Ref(a.ID), Certainty: CertaintyInferred,
				ApprovalOverride: a.ApprovalOverride, EnvPassthrough: a.EnvPassthrough, Basis: "product_default",
				DerivedFrom: []string{id, n.ID}, Assumptions: baseAssumptions(AssumeOrchestratorAgentConfigured),
			})
		}
		for _, e := range paperclipSkillWrites(f, id, kb, home) {
			b.edge(e)
		}
	}
	return Merge(f, b.fragment())
}

// paperclipCredentials are an instance's secrets, identified by their path:
// secrets/master.key when db/ is there too (together they open every secret
// Paperclip keeps), .env, and llm.apiKey and database.connectionString when
// config.json sets them. None is opened; the owner and mode are the file's.
func paperclipCredentials(in platform.PaperclipInstance, kb knowledge.PaperclipBase, salt []byte, seen string) []Node {
	files := map[string]platform.PaperclipFile{}
	for _, f := range in.Files {
		files[f.Role] = f
	}
	var out []Node
	add := func(location string, file platform.PaperclipFile) {
		fp := PathFingerprint(salt, location)
		out = append(out, Node{
			ID: "cred_" + keyHash(fp), Kind: KindCredential, Type: CredentialGenericSecret, Service: ServiceUnknown,
			Locations: []string{location}, DiscoveredAt: seen,
			IdentifiedBy: IdentifiedByPath, PathFingerprint: fp, FileOwner: ownerOr(file.Owner), FileMode: file.Mode,
		})
	}
	for _, s := range kb.PathSecrets {
		switch {
		case s.Pointer != "":
			set := (s.ID == "llm_api_key" && in.LLMAPIKeySet) || (s.ID == "database_connection_string" && in.ConnectionStringSet)
			if set {
				add(in.ConfigPath+"#"+s.Pointer, files[platform.PaperclipConfig])
			}
		case s.ID == "master_key":
			if k, db := files[platform.PaperclipMasterKey], files[platform.PaperclipDB]; k.Exists && db.Exists {
				add(k.Path, k)
			}
		case s.ID == "agent_jwt_secret":
			if e := files[platform.PaperclipEnv]; e.Exists {
				add(e.Path, e)
			}
		}
	}
	return out
}

func ownerOr(o string) string {
	if o == "" {
		return OwnerUnknown
	}
	return o
}

// paperclipApprovers is who can give Paperclip orders through its API
// (CTO-75 §5): in local_trusted every local process is an administrator; in
// authenticated mode anyone can sign up unless sign-up is turned off.
func paperclipApprovers(in platform.PaperclipInstance, svc *platform.PaperclipService) string {
	mode, signUpOff := PaperclipEffective(in, svc)
	if mode != "authenticated" || !signUpOff {
		return "anyone_admitted"
	}
	return "listed"
}

// PaperclipEffective is an instance's deployment mode and whether sign-up is
// off, as its config.json and a service unit's PAPERCLIP_* variables set them
// (the unit wins: it is what Paperclip starts with). Product defaults fill in
// what neither says: local_trusted, sign-up open.
func PaperclipEffective(in platform.PaperclipInstance, svc *platform.PaperclipService) (mode string, signUpOff bool) {
	mode = in.DeploymentMode
	if in.SignUpDisabled != nil {
		signUpOff = *in.SignUpDisabled
	}
	if svc != nil {
		if v := svc.Env["PAPERCLIP_DEPLOYMENT_MODE"]; v != "" {
			mode = v
		}
		if v := svc.Env["PAPERCLIP_AUTH_DISABLE_SIGN_UP"]; v != "" {
			signUpOff = strings.EqualFold(v, "true")
		}
	}
	if mode == "" {
		mode = "local_trusted"
	}
	return mode, signUpOff
}

// paperclipSkillWrites are writes_config_of edges from the orchestrator to the
// skill files of the agents Paperclip writes skills for (CTO-75 §2: Cursor's
// ~/.cursor/skills, Gemini CLI's ~/.gemini/skills), when those agents and
// their skills are in the graph.
func paperclipSkillWrites(f Fragment, orch string, kb knowledge.PaperclipBase, home string) []Edge {
	product := map[string]string{}
	for _, n := range f.Nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient {
			product[n.ID] = n.Platform
		}
	}
	var out []Edge
	for _, s := range kb.Skills {
		dir := canonicalPath(filepath.Join(home, filepath.FromSlash(s.Path)))
		for _, n := range f.Nodes {
			if n.Kind != KindResource || (n.Type != ResourceAgentInstructions && n.Type != ResourceAgentExecutable) {
				continue
			}
			if product[n.Configures] != s.Agent || !strings.HasPrefix(canonicalPath(n.Path), dir+"/") {
				continue
			}
			out = append(out, Edge{
				ID: edgeID(EdgeWritesConfigOf, orch, n.ID), Type: EdgeWritesConfigOf, From: orch, To: n.ID,
				Source: kb.Ref("skills/" + s.Agent), Certainty: CertaintyInferred,
				DerivedFrom: []string{orch, n.ID}, Assumptions: baseAssumptions(),
			})
		}
	}
	return out
}
