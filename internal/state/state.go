// Package state loads and persists RosmarOps's internal state between runs.
// The state file is the baseline for drift detection. It is written atomically
// (temp-file + fsync + rename) to avoid corruption on crash or power loss.
// Hand-editing the state file is discouraged; use `rosmarops baseline` instead.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/schemas"
)

// State mirrors schemas/state.v2.json exactly.
type State struct {
	SchemaVersion int    `json:"schema_version"`
	EngineVersion string `json:"engine_version"`
	LastRun       string `json:"last_run"`
	LastRunStatus string `json:"last_run_status,omitempty"`

	// LocalSalt is the machine-local salt (hex). It keys credential
	// fingerprints and host_id and must never leave the device.
	LocalSalt string `json:"local_salt"`

	ConsecutiveGreen int       `json:"consecutive_green"`
	Integrity        Integrity `json:"integrity"`
	Baselines        Baselines `json:"baselines"`
}

// Integrity holds hashes used for the self-integrity check on startup.
type Integrity struct {
	VigilBinarySHA256 string `json:"vigil_binary_sha256"`
	ConfigSHA256      string `json:"config_sha256"`
}

// Baselines holds the last-known state of every audited surface.
type Baselines struct {
	VSCodeExtensions     []VSCodeExtension   `json:"vscode_extensions,omitempty"`
	VSCodeSettingsSHA256 string              `json:"vscode_settings_sha256,omitempty"`
	MCPServers           []MCPServerBaseline `json:"mcp_servers,omitempty"`
	Repos                []RepoBaseline      `json:"repos,omitempty"`
	ShellProfileSHA256   string              `json:"shell_profile_sha256,omitempty"`
	AVExclusions         []AVExclusion       `json:"av_exclusions,omitempty"`
	GlobalPackages       []GlobalPackage     `json:"global_packages,omitempty"`
	ScheduledTasks       []ScheduledTaskBase `json:"scheduled_tasks,omitempty"`
	NpmrcSHA256          string              `json:"npmrc_sha256,omitempty"`
}

// VSCodeExtension is a baselined VS Code extension entry.
type VSCodeExtension struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	FirstSeen string `json:"first_seen,omitempty"`
}

// MCPServerBaseline is a baselined MCP server entry across any AI coding agent.
type MCPServerBaseline struct {
	Client        string `json:"client"`
	Name          string `json:"name"`
	CommandSHA256 string `json:"command_sha256"`
	FirstSeen     string `json:"first_seen,omitempty"`
}

// RepoBaseline holds per-repo baselined artifacts.
type RepoBaseline struct {
	Path              string         `json:"path"`
	PackageSetsSHA256 string         `json:"package_sets_sha256,omitempty"`
	WorkflowFiles     []WorkflowFile `json:"workflow_files,omitempty"`
	Hooks             []HookBaseline `json:"hooks,omitempty"`
	CoreHooksPath     string         `json:"core_hooks_path,omitempty"`
}

// WorkflowFile is a baselined GitHub Actions workflow file.
type WorkflowFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// HookBaseline is a baselined git hook.
type HookBaseline struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// AVExclusion is a baselined Windows Defender exclusion.
type AVExclusion struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// GlobalPackage is a baselined globally-installed package.
type GlobalPackage struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ScheduledTaskBase is a baselined OS scheduled task.
type ScheduledTaskBase struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// StatePath returns the canonical path for the state file.
func StatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".rosmarops", "state.json"), nil
}

// SchemaVersion is the state schema version RosmarOps writes. Version 1 files are
// still read and migrated in memory (see LoadState).
const SchemaVersion = 2

// compiledV1 and compiledV2 are the parsed state schemas, initialized once on
// first use.
var compiledV1, compiledV2 *schemaval.Schema

func getSchema(version int) (*schemaval.Schema, error) {
	cache, raw := &compiledV2, schemas.StateV2Schema
	if version == 1 {
		cache, raw = &compiledV1, schemas.StateSchema
	}
	if *cache != nil {
		return *cache, nil
	}
	s, err := schemaval.Compile(raw)
	if err != nil {
		return nil, fmt.Errorf("compile state v%d schema: %w", version, err)
	}
	*cache = s
	return s, nil
}

// zeroSHA256 is a placeholder hash (64 hex zeros) used before `rosmarops baseline`
// has been run. It satisfies the schema's pattern constraint while signalling
// "no baseline established yet" to runtime logic.
const zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

// saltBytes is the length of the machine-local salt.
const saltBytes = 32

// NewSalt returns a fresh machine-local salt: 32 random bytes from
// crypto/rand, hex-encoded (64 lowercase characters).
func NewSalt() (string, error) {
	b := make([]byte, saltBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate local salt: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Salt returns the decoded machine-local salt.
func (s State) Salt() ([]byte, error) {
	b, err := hex.DecodeString(s.LocalSalt)
	if err != nil || len(b) != saltBytes {
		return nil, fmt.Errorf("state local_salt is not %d hex-encoded bytes", saltBytes)
	}
	return b, nil
}

// DefaultState returns a minimal valid v2 State for a first-time init, with a
// fresh local salt. Integrity hashes are set to zeroSHA256 until
// `rosmarops baseline` populates them.
// NeverRun is the last_run of a state no run has updated yet.
const NeverRun = "0001-01-01T00:00:00Z"

// HasRun reports a state an earlier run already updated (decision
// 2026-10-26, integridad y revisión semanal, J1).
func (s State) HasRun() bool { return s.LastRun != "" && s.LastRun != NeverRun }

func DefaultState(engineVersion string) (State, error) {
	salt, err := NewSalt()
	if err != nil {
		return State{}, err
	}
	return State{
		SchemaVersion:    SchemaVersion,
		EngineVersion:    engineVersion,
		LastRun:          NeverRun,
		LocalSalt:        salt,
		ConsecutiveGreen: 0,
		Integrity: Integrity{
			VigilBinarySHA256: zeroSHA256,
			ConfigSHA256:      zeroSHA256,
		},
		Baselines: Baselines{},
	}, nil
}

// LoadState reads and validates the state file from path.
// Returns an error if the file is missing, malformed, or fails schema validation.
//
// A schema_version 1 file is validated against the v1 schema and migrated in
// memory to v2 by generating a local salt; it is written as v2 on the next
// SaveState. Any version other than 1 or 2 (e.g. state written by a newer
// RosmarOps) is a hard error.
func LoadState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("read state %s: %w", path, err)
	}

	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return State{}, fmt.Errorf("parse state %s: %w", path, err)
	}

	version := 0
	if m, ok := probe.(map[string]any); ok {
		if v, ok := m["schema_version"].(float64); ok && v == float64(int(v)) {
			version = int(v)
		}
	}
	if version != 1 && version != SchemaVersion {
		return State{}, fmt.Errorf("state %s: unsupported schema_version (expected 1 or %d); run `rosmarops baseline` after upgrading", path, SchemaVersion)
	}

	if err := validate(raw, version); err != nil {
		return State{}, fmt.Errorf("state %s %w", path, err)
	}

	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("decode state %s: %w", path, err)
	}

	if version == 1 {
		salt, err := NewSalt()
		if err != nil {
			return State{}, fmt.Errorf("migrate state %s to v2: %w", path, err)
		}
		st.SchemaVersion = SchemaVersion
		st.LocalSalt = salt
	}

	return st, nil
}

// validate checks raw state JSON against the given schema version.
func validate(raw []byte, version int) error {
	schema, err := getSchema(version)
	if err != nil {
		return err
	}
	result, err := schema.Validate(raw)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !result.Valid() {
		errs := result.Errors()
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.String()
		}
		return fmt.Errorf("fails schema validation:\n  %s", joinLines(msgs))
	}
	return nil
}

// SaveState writes st to path atomically: temp file in the same directory,
// fsync, then rename. st must be a valid v2 state (as returned by LoadState or
// DefaultState); it is validated before anything is written. On any error the
// original file (if present) is untouched.
// Invariant: path must be under ~/.rosmarops/ — enforced by the caller.
func SaveState(path string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	// Never write a state file that the next LoadState would reject.
	if err := validate(data, SchemaVersion); err != nil {
		return fmt.Errorf("state to save %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Best-effort cleanup on failure.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp state to %s: %w", path, err)
	}

	success = true
	return nil
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}
