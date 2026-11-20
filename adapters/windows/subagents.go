package windows

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// subagentHeaderKeys are the only header fields read: those that give a
// subagent tools or permissions (CTO-76 §5). Every other field, the
// description and the prompt below the header are not parsed.
var subagentHeaderKeys = []string{"tools", "permissionMode", "hooks", "mcpServers"}

// errNoHeader is a subagent file that does not start with a YAML header.
var errNoHeader = errors.New("no YAML header")

// ReadClaudeSubagents reads the headers of the user's subagents
// (<claudeDir>/agents/*.md) and of each repository's (.claude/agents/*.md).
// Only *.md files directly in those folders count, as Claude Code loads them.
func ReadClaudeSubagents(claudeDir string, repoRoots []string) []platform.ClaudeSubagent {
	var out []platform.ClaudeSubagent
	out = append(out, readSubagentDir(filepath.Join(claudeDir, "agents"), "", platform.SubagentUser)...)
	seen := map[string]bool{}
	for _, r := range repoRoots {
		dir := filepath.Join(r, ".claude", "agents")
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, readSubagentDir(dir, r, platform.SubagentProject)...)
	}
	return out
}

func readSubagentDir(dir, repo, scope string) []platform.ClaudeSubagent {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []platform.ClaudeSubagent{{Path: dir, Dir: dir, Repo: repo, Scope: scope, Status: platform.MCPSourceFailed, Reason: fmt.Sprintf("list subagents: %v", err)}}
	}
	var out []platform.ClaudeSubagent
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		s := platform.ClaudeSubagent{Path: filepath.Join(dir, e.Name()), Dir: dir, Repo: repo, Scope: scope}
		if err := readSubagentHeader(&s); err != nil {
			s.Status, s.Reason = platform.MCPSourceFailed, err.Error()
		} else {
			s.Status = platform.MCPSourceRead
		}
		out = append(out, s)
	}
	return out
}

// readSubagentHeader fills s from the file's header. It reads the file only
// up to the line that closes the header: the body is never read.
func readSubagentHeader(s *platform.ClaudeSubagent) error {
	info, err := os.Lstat(s.Path)
	if err != nil {
		return fmt.Errorf("stat subagent: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if info.Size() > yamlsubset.MaxBytes {
		return fmt.Errorf("larger than %d bytes", yamlsubset.MaxBytes)
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return fmt.Errorf("open subagent: %w", err)
	}
	defer f.Close()
	header, err := subagentHeader(f)
	if err != nil {
		return err
	}
	v, err := yamlsubset.ParseKeysWithMapItems(header, subagentHeaderKeys...)
	if err != nil {
		return fmt.Errorf("parse subagent header: %w", err)
	}
	m, _ := v.(yamlsubset.Map)
	if t, ok := m["tools"]; ok {
		// An empty tools: is the same as none written (it inherits all).
		if sc, isScalar := t.(yamlsubset.Scalar); !isScalar || !sc.IsNull() {
			s.ToolsDeclared = true
		}
	}
	if pm, ok := m["permissionMode"].(yamlsubset.Scalar); ok {
		s.PermissionMode = strings.TrimSpace(pm.Text)
	}
	s.HookCommands = countCommandHooks(m["hooks"])
	s.InlineMCPServers = inlineMCPServers(m["mcpServers"])
	return nil
}

// subagentHeader returns the lines between the opening "---" and the closing
// "---", reading no further than the closing line.
func subagentHeader(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(io.LimitReader(r, yamlsubset.MaxBytes))
	first, err := br.ReadString('\n')
	first = strings.TrimPrefix(first, "\ufeff")
	if strings.TrimRight(first, "\r\n") != "---" {
		return nil, errNoHeader
	}
	if err != nil {
		return nil, errNoHeader
	}
	var buf bytes.Buffer
	for {
		ln, err := br.ReadString('\n')
		if t := strings.TrimRight(ln, "\r\n"); t == "---" || t == "..." {
			return buf.Bytes(), nil
		}
		buf.WriteString(ln)
		if err != nil {
			return nil, errors.New("the YAML header is not closed")
		}
	}
}

// countCommandHooks counts, anywhere under hooks, the hook entries whose
// type is command (the shape of settings.json hooks).
func countCommandHooks(v any) int {
	n := 0
	switch t := v.(type) {
	case yamlsubset.Map:
		if ty, ok := t["type"].(yamlsubset.Scalar); ok && ty.Text == "command" {
			n++
		}
		for k, c := range t {
			if k != "type" {
				n += countCommandHooks(c)
			}
		}
	case yamlsubset.Seq:
		for _, c := range t {
			n += countCommandHooks(c)
		}
	}
	return n
}

// inlineMCPServers are the names of the servers mcpServers defines in place:
// a list item that is a mapping (name: {command…}), or, written as a
// mapping, each key whose value is a mapping. A bare name refers to a server
// configured elsewhere and is not inline.
func inlineMCPServers(v any) []string {
	var names []string
	add := func(m yamlsubset.Map) {
		for k, c := range m {
			if _, ok := c.(yamlsubset.Map); ok {
				names = append(names, k)
			}
		}
	}
	switch t := v.(type) {
	case yamlsubset.Seq:
		for _, item := range t {
			if m, ok := item.(yamlsubset.Map); ok {
				add(m)
			}
		}
	case yamlsubset.Map:
		add(t)
	}
	sort.Strings(names)
	return names
}
