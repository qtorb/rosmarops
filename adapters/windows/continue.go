package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// Continue (BV-277) is detected, not scored: its repository is no longer
// maintained (continuedev/continue 5522c6f, README.md:19). RosmarOps reads
// only its MCP servers, through the shared MCP pipeline, from:
//   - the global config.yaml, or config.json when there is no config.yaml
//     (core/config/profile/doLoadConfig.ts:103-127);
//   - each project's .continue/mcpServers folder, read recursively: YAML
//     block files and JSON files (core/config/loadLocalAssistants.ts:103-125,
//     core/context/mcp/json/loadJsonMcpConfigs.ts:39-150).
//
// Its permissions file (permissions.yaml), its .env files and its other
// block folders are never opened.

// Formats of Continue's MCP files.
const (
	// formatContinueYAML: a YAML file whose top-level mcpServers is a list of
	// servers (config.yaml and the block files).
	formatContinueYAML fileFormat = iota + 100
	// formatContinueConfigJSON: the legacy config.json, whose servers are
	// experimental.modelContextProtocolServers[].transport.
	formatContinueConfigJSON
	// formatContinueJSON: a JSON file in .continue/mcpServers, in one of the
	// three shapes Continue accepts.
	formatContinueJSON
)

// continueMaxFiles bounds the files read from one project's
// .continue/mcpServers folder; more are left unread.
const continueMaxFiles = 200

// continueSources are the sources of Continue's MCP servers on this computer.
func continueSources(dir string, repoRoots []string) []sourceSpec {
	var out []sourceSpec
	global := sourceSpec{
		path: filepath.Join(dir, "config.yaml"), client: agentdirs.Continue,
		format: formatContinueYAML, loadsMCP: true, configDir: dir, scope: platform.MCPScopeUser,
	}
	if !fileExists(global.path) {
		global.path, global.format = filepath.Join(dir, "config.json"), formatContinueConfigJSON
	}
	out = append(out, global)
	for _, root := range repoRoots {
		for _, p := range continueBlockFiles(filepath.Join(root, ".continue", "mcpServers")) {
			format := formatContinueJSON
			if ext := strings.ToLower(filepath.Ext(p)); ext == ".yaml" || ext == ".yml" {
				format = formatContinueYAML
			}
			out = append(out, sourceSpec{
				path: p, client: agentdirs.Continue, format: format, loadsMCP: true,
				configDir: dir, scope: platform.MCPScopeProject, scopePath: root,
			})
		}
	}
	return out
}

// continueBlockFiles lists the .yaml, .yml and .json files under dir, in
// order. It follows no link, skips the forced exclusions (invariant 5) and
// stops at continueMaxFiles.
func continueBlockFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && repos.IsForcedExclusion(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			if len(out) >= continueMaxFiles {
				return filepath.SkipAll
			}
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// continueEntries parses a Continue MCP file into its servers by JSON
// pointer.
func continueEntries(format fileFormat, path string, raw []byte) (map[string]namedEntry, error) {
	switch format {
	case formatContinueYAML:
		return continueYAMLEntries(raw)
	case formatContinueConfigJSON:
		return continueConfigJSONEntries(raw)
	default:
		return continueJSONEntries(path, raw)
	}
}

// continueYAMLEntries reads the mcpServers list of a YAML file. An entry
// that is a hub reference (uses:) is fetched by Continue over the network
// and cannot be read at rest, so it is skipped.
func continueYAMLEntries(raw []byte) (map[string]namedEntry, error) {
	doc, err := yamlsubset.ParseKeysWithMapItems(raw, "mcpServers")
	if err != nil {
		return nil, err
	}
	out := map[string]namedEntry{}
	top, _ := doc.(yamlsubset.Map)
	list, ok := top["mcpServers"].(yamlsubset.Seq)
	if !ok {
		if v, present := top["mcpServers"]; present && !isNullScalar(v) {
			return nil, errors.New("mcpServers is not a list")
		}
		return out, nil
	}
	for i, item := range list {
		m, ok := item.(yamlsubset.Map)
		if !ok {
			return nil, fmt.Errorf("mcpServers[%d] is not a mapping", i)
		}
		if _, hub := m["uses"]; hub {
			continue
		}
		e := mcpServerEntry{
			Type:    yamlText(m["type"]),
			Command: yamlText(m["command"]),
			URL:     yamlText(m["url"]),
			Args:    yamlList(m["args"]),
			Env:     continueValues(yamlMap(m["env"])),
		}
		headers := map[string]string{}
		if ro, ok := m["requestOptions"].(yamlsubset.Map); ok {
			headers = yamlMap(ro["headers"])
		}
		if k := yamlText(m["apiKey"]); k != "" {
			headers["apiKey"] = k
		}
		e.Headers = continueValues(headers)
		name := yamlText(m["name"])
		if name == "" {
			name = strconv.Itoa(i)
		}
		out["/mcpServers/"+strconv.Itoa(i)] = namedEntry{name: name, entry: continueType(e)}
	}
	return out, nil
}

// continueConfigJSON is the part of the legacy config.json RosmarOps reads.
type continueConfigJSON struct {
	Experimental struct {
		Servers []struct {
			Transport continueJSONServer `json:"transport"`
		} `json:"modelContextProtocolServers"`
	} `json:"experimental"`
}

// continueJSONServer is one server in Continue's JSON files.
type continueJSONServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func (s continueJSONServer) entry() mcpServerEntry {
	return continueType(mcpServerEntry{
		Type: s.Type, Command: s.Command, Args: s.Args, URL: s.URL,
		Env: continueValues(s.Env), Headers: continueValues(s.Headers),
	})
}

func continueConfigJSONEntries(raw []byte) (map[string]namedEntry, error) {
	var cfg continueConfigJSON
	if err := json.Unmarshal(jsonc.Strip(raw), &cfg); err != nil {
		return nil, err
	}
	out := map[string]namedEntry{}
	for i, s := range cfg.Experimental.Servers {
		ptr := "/experimental/modelContextProtocolServers/" + strconv.Itoa(i) + "/transport"
		out[ptr] = namedEntry{name: "MCP Server " + strconv.Itoa(i+1), entry: s.Transport.entry()}
	}
	return out, nil
}

// continueJSONEntries reads a JSON file of .continue/mcpServers in the
// first of Continue's three shapes it fits (loadJsonMcpConfigs.ts:97-150):
// Claude Code's (projects, and optionally mcpServers), Claude Desktop's
// (mcpServers), or one server named after the file.
func continueJSONEntries(path string, raw []byte) (map[string]namedEntry, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(jsonc.Strip(raw), &top); err != nil {
		return nil, err
	}
	out := map[string]namedEntry{}
	servers := func(prefix string, msg json.RawMessage) error {
		var m map[string]continueJSONServer
		if err := json.Unmarshal(msg, &m); err != nil {
			return err
		}
		for name, s := range m {
			out[prefix+pointerEscape(name)] = namedEntry{name: name, entry: s.entry()}
		}
		return nil
	}
	if p, ok := top["projects"]; ok {
		if msg, ok := top["mcpServers"]; ok {
			if err := servers("/mcpServers/", msg); err != nil {
				return nil, err
			}
		}
		var projects map[string]struct {
			MCPServers json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(p, &projects); err != nil {
			return nil, err
		}
		for name, pr := range projects {
			if len(pr.MCPServers) == 0 {
				continue
			}
			if err := servers("/projects/"+pointerEscape(name)+"/mcpServers/", pr.MCPServers); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	if msg, ok := top["mcpServers"]; ok {
		return out, servers("/mcpServers/", msg)
	}
	var one continueJSONServer
	if err := json.Unmarshal(jsonc.Strip(raw), &one); err != nil {
		return nil, err
	}
	if one.Command == "" && one.URL == "" {
		return nil, errors.New("not a supported MCP JSON shape")
	}
	out[""] = namedEntry{name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), entry: one.entry()}
	return out, nil
}

// continueType maps Continue's server types to the shared ones:
// streamable-http is http, and a server with a url and no command is
// remote.
func continueType(e mcpServerEntry) mcpServerEntry {
	switch {
	case e.Type == "streamable-http":
		e.Type = "http"
	case e.Type == "stdio", e.Type == "sse", e.Type == "http", e.Type == "websocket":
	case strings.TrimSpace(e.Command) == "" && strings.TrimSpace(e.URL) != "":
		e.Type = "http"
	}
	return e
}

// continueValues drops values that are only a template (${{ secrets.X }},
// ${{ inputs.X }}, or a JSON ${VAR}): they name a value kept elsewhere, not
// the value itself. The key stays, as a name with no value.
func continueValues(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		t := strings.TrimSpace(v)
		if (strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}")) || (strings.HasPrefix(t, "${") && strings.HasSuffix(t, "}") && !strings.ContainsAny(t[2:len(t)-1], "${} ")) {
			v = ""
		}
		out[k] = v
	}
	return out
}

func isNullScalar(v any) bool {
	s, ok := v.(yamlsubset.Scalar)
	return ok && s.IsNull()
}

func yamlText(v any) string {
	if s, ok := v.(yamlsubset.Scalar); ok && !s.IsNull() {
		return s.Text
	}
	return ""
}

func yamlList(v any) []string {
	seq, ok := v.(yamlsubset.Seq)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(seq))
	for _, it := range seq {
		if s, ok := it.(yamlsubset.Scalar); ok {
			out = append(out, s.Text)
		}
	}
	return out
}

func yamlMap(v any) map[string]string {
	m, ok := v.(yamlsubset.Map)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, it := range m {
		out[k] = yamlText(it)
	}
	return out
}
