package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// hermesTrust is what Hermes Agent's config.yaml says about project skills
// (decision BV-99b, item 2). Hermes loads <root>\.hermes\skills and
// <root>\.agents\skills only when skills.project_discovery is not false
// and the root is in skills.trusted_project_dirs (agent/skill_utils.py:507-513,
// 553-570, 579-584 @ 0431192d).
type hermesTrust struct {
	// discovery is false when skills.project_discovery is false: no project
	// skill is loaded.
	discovery bool
	// roots are the trusted roots, by pathKey.
	roots map[string]bool
	// unknown, when set, is the coverage reason for the project skills
	// folders not attributed to Hermes: the setting is absent or the file
	// could not be read.
	unknown string
}

// trusts reports whether Hermes loads root's project skills.
func (t hermesTrust) trusts(root string) bool {
	return t.unknown == "" && t.discovery && t.roots[pathKey(root)]
}

// readHermesTrust reads skills.project_discovery and
// skills.trusted_project_dirs from <HERMES_HOME>\config.yaml with the YAML
// subset reader (BV-97). trusted_project_dirs is a list of paths, or one
// path (_config_str_list, agent/skill_utils.py:327-333); each entry is
// expanded as Hermes does (~, ${VAR}, $VAR and %VAR%; unknown variables
// stay as written, os.path.expandvars). Hermes resolves each entry and the
// root with Path.resolve(); here they are compared cleaned, without
// resolving links, and without case on Windows paths. A relative entry
// (resolved against Hermes's working folder) matches no root.
func readHermesTrust(l agentdirs.Layout, home string, env agentdirs.Env) hermesTrust {
	if home == "" {
		return hermesTrust{}
	}
	path := filepath.Join(l.HermesHome(home, env), "config.yaml")
	fail := func(code, detail string) hermesTrust {
		return hermesTrust{unknown: graph.Reason(code, path+": "+detail)}
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		// No config.yaml: Hermes is not present (agentdirs.Detect).
		return hermesTrust{}
	case err != nil:
		return fail(graph.ReasonReadError, err.Error())
	case info.Size() > yamlsubset.MaxBytes:
		return fail(graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), yamlsubset.MaxBytes))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fail(graph.ReasonReadError, err.Error())
	}
	doc, err := yamlsubset.Parse(raw)
	if err != nil {
		return fail(graph.ReasonParseError, err.Error())
	}
	root, _ := doc.(yamlsubset.Map)
	skills, _ := root["skills"].(yamlsubset.Map)
	t := hermesTrust{discovery: true, roots: map[string]bool{}}
	// _current_project_root: only «project_discovery is False» turns it off.
	if s, ok := skills["project_discovery"].(yamlsubset.Scalar); ok {
		if b, isBool := s.Bool(); isBool && !b {
			t.discovery = false
		}
	}
	if !t.discovery {
		return t
	}
	var entries []string
	switch v := skills["trusted_project_dirs"].(type) {
	case yamlsubset.Seq:
		for _, item := range v {
			if s, ok := item.(yamlsubset.Scalar); ok && !s.IsNull() {
				entries = append(entries, s.Text)
			}
		}
	case yamlsubset.Scalar:
		if v.IsNull() {
			return fail(graph.ReasonSourceUnsupported, "skills.trusted_project_dirs is empty")
		}
		entries = []string{v.Text}
	default:
		return fail(graph.ReasonSourceUnsupported, "skills.trusted_project_dirs is absent")
	}
	for _, e := range entries {
		if e = strings.TrimSpace(e); e == "" {
			continue
		}
		if p := expandHermesPath(e, home, env); filepath.IsAbs(p) || isWindowsAbs(p) {
			t.roots[pathKey(p)] = true
		}
	}
	return t
}

// expandHermesPath expands ~ and environment variables as
// os.path.expanduser(os.path.expandvars(entry)) does.
func expandHermesPath(p, home string, env agentdirs.Env) string {
	if env == nil {
		env = os.LookupEnv
	}
	var b strings.Builder
	for i := 0; i < len(p); {
		var name string
		var n int
		switch {
		case p[i] == '$' && i+1 < len(p) && p[i+1] == '{':
			if end := strings.IndexByte(p[i+2:], '}'); end >= 0 {
				name, n = p[i+2:i+2+end], end+3
			}
		case p[i] == '$':
			j := i + 1
			for j < len(p) && (p[j] == '_' || p[j] >= '0' && p[j] <= '9' || p[j]|0x20 >= 'a' && p[j]|0x20 <= 'z') {
				j++
			}
			name, n = p[i+1:j], j-i
		case p[i] == '%':
			if end := strings.IndexByte(p[i+1:], '%'); end > 0 {
				name, n = p[i+1:i+1+end], end+2
			}
		}
		if v, ok := env(name); name != "" && ok {
			b.WriteString(v)
			i += n
			continue
		}
		b.WriteByte(p[i])
		i++
	}
	p = b.String()
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// isWindowsAbs reports a drive-letter absolute path (C:\x, C:/x) or a UNC
// path, on any platform.
func isWindowsAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') ||
		strings.HasPrefix(p, `\\`)
}

// pathKey is a path for comparison: cleaned, with forward slashes, and
// lowercased when it is a Windows path (case-insensitive file system), as
// graph's canonical paths.
func pathKey(p string) string {
	windows := strings.Contains(p, `\`) || isWindowsAbs(p)
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimRight(filepath.ToSlash(filepath.Clean(filepath.FromSlash(p))), "/")
	if windows {
		p = strings.ToLower(p)
	}
	return p
}
