// Package config loads and validates the RosmarOps configuration file.
// The config file is plain JSON validated against the embedded config.v1.json
// schema on every load. No executable code is ever eval'd from config.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/schemas"
)

// Config mirrors schemas/config.v1.json exactly.
// Field tags match the JSON schema property names.
type Config struct {
	SchemaVersion       int             `json:"schema_version"`
	RepoRoots           []string        `json:"repo_roots"`
	ReportDirectory     string          `json:"report_directory,omitempty"`
	Thresholds          Thresholds      `json:"thresholds,omitempty"`
	ScheduleHint        ScheduleHint    `json:"schedule_hint,omitempty"`
	AllowList           AllowList       `json:"allow_list,omitempty"`
	ExtraSecretPatterns []SecretPattern `json:"extra_secret_patterns,omitempty"`
}

// Thresholds holds numeric noise-budget settings.
type Thresholds struct {
	MaxUncommittedFiles int `json:"max_uncommitted_files,omitempty"`
	MaxFindingsInHeader int `json:"max_findings_in_header,omitempty"`
}

// ScheduleHint describes when `rosmarops schedule` should register the OS task.
type ScheduleHint struct {
	DayOfWeek    string `json:"day_of_week,omitempty"`
	HourLocal24h int    `json:"hour_local_24h,omitempty"`
	Minute       int    `json:"minute,omitempty"`
}

// AllowList holds per-category accepted findings.
type AllowList struct {
	Extensions       []AllowEntryID            `json:"extensions,omitempty"`
	MCPServers       []AllowEntryID            `json:"mcp_servers,omitempty"`
	Packages         []AllowEntryID            `json:"packages,omitempty"`
	ScheduledTasks   []AllowEntryID            `json:"scheduled_tasks,omitempty"`
	GlobalPackages   []AllowEntryID            `json:"global_packages,omitempty"`
	AVExclusions     []AllowEntryPath          `json:"av_exclusions,omitempty"`
	GitHooks         []AllowEntryPath          `json:"git_hooks,omitempty"`
	SecretsInHistory []AllowEntrySecretHistory `json:"secrets_in_history,omitempty"`

	// RotatedCredentials and TestFixtures (decision 2026-10-24, memoria,
	// M9–M10).
	RotatedCredentials []AllowEntryRotated     `json:"rotated_credentials,omitempty"`
	TestFixtures       []AllowEntryTestFixture `json:"test_fixtures,omitempty"`
}

// AllowHit is one warning an allow_list entry hid in this run (decision
// 2026-10-27, blindaje, B9): the list, the entry's identifier as written
// (id, path, or pattern_type:commit_sha) and its note. The report counts
// them and lists each entry; it never holds a secret's value.
type AllowHit struct {
	List, ID, Note string
}

// The allow_list lists that hide warnings (B9), as the report names them.
const (
	ListExtensions         = "extensions"
	ListMCPServers         = "mcp_servers"
	ListScheduledTasks     = "scheduled_tasks"
	ListGlobalPackages     = "global_packages"
	ListAVExclusions       = "av_exclusions"
	ListGitHooks           = "git_hooks"
	ListSecretsInHistory   = "secrets_in_history"
	ListRotatedCredentials = "rotated_credentials"
)

// AllowEntryRotated declares a credential rotated by its id: it stops
// giving cred_in_git. RosmarOps cannot check it without network.
type AllowEntryRotated struct {
	ID   string `json:"id"`
	Date string `json:"date"`
	Note string `json:"note"`
}

// AllowEntryTestFixture silences test data: the secrets committed in the
// files of Repo that match Path, under the guards of the rules layer.
type AllowEntryTestFixture struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Note      string `json:"note"`
	ExpiresOn string `json:"expires_on"`
}

// MaxFixtureLife is how far ahead a test-data entry may expire: one year
// after it is added (M10). A later expires_on makes the configuration
// invalid.
const MaxFixtureLife = 366 * 24 * time.Hour

// AllowEntryID is an allow-list entry keyed by an opaque identifier (extension id, package name, …).
type AllowEntryID struct {
	ID         string `json:"id"`
	Note       string `json:"note"`
	AcceptedAt string `json:"accepted_at,omitempty"`
}

// AllowEntryPath is an allow-list entry keyed by a filesystem path.
type AllowEntryPath struct {
	Path       string `json:"path"`
	Note       string `json:"note"`
	AcceptedAt string `json:"accepted_at,omitempty"`
}

// AllowEntrySecretHistory accepts a known secret that is only in git history.
type AllowEntrySecretHistory struct {
	PatternType string `json:"pattern_type"`
	CommitSHA   string `json:"commit_sha"`
	Note        string `json:"note"`
	AcceptedAt  string `json:"accepted_at,omitempty"`
}

// SecretPattern is a user-defined regex pattern extending the built-in matchers.
type SecretPattern struct {
	Name     string `json:"name"`
	Regex    string `json:"regex"`
	Severity string `json:"severity,omitempty"`
}

// compiledSchema is the parsed config schema, initialized once on first use.
var compiledSchema *schemaval.Schema

func getSchema() (*schemaval.Schema, error) {
	if compiledSchema != nil {
		return compiledSchema, nil
	}
	s, err := schemaval.Compile(schemas.ConfigSchema)
	if err != nil {
		return nil, fmt.Errorf("compile config schema: %w", err)
	}
	compiledSchema = s
	return compiledSchema, nil
}

// DefaultConfig returns a valid Config with schema_version 1 and empty repo_roots.
// The user fills in repo_roots by editing the config file.
func DefaultConfig() Config {
	return Config{
		SchemaVersion: 1,
		RepoRoots:     []string{},
		Thresholds: Thresholds{
			MaxUncommittedFiles: 50,
			MaxFindingsInHeader: 15,
		},
		ScheduleHint: ScheduleHint{
			DayOfWeek:    "Sunday",
			HourLocal24h: 22,
			Minute:       0,
		},
	}
}

// HomeConfigPath is ~/.rosmarops/config.json, the only configuration a
// scheduled review reads, never one in the working directory (decision
// 2026-10-26, integridad y revisión semanal, J3).
func HomeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".rosmarops", "config.json"), nil
}

// ResolveConfigPath returns the path RosmarOps will use for the config file.
// It prefers ./rosmarops.config.json in the current directory, falling back to
// ~/.rosmarops/config.json.
func ResolveConfigPath() (string, error) {
	if _, err := os.Stat("rosmarops.config.json"); err == nil {
		abs, err := filepath.Abs("rosmarops.config.json")
		if err != nil {
			return "", fmt.Errorf("resolve local config path: %w", err)
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".rosmarops", "config.json"), nil
}

// LoadConfig reads the config file at path, validates it against the embedded
// schema, and returns a typed Config. Any schema violation is reported by field name.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	// Validate JSON syntax first so we get a clean parse error, not a schema error.
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	schema, err := getSchema()
	if err != nil {
		return Config{}, err
	}

	result, err := schema.Validate(raw)
	if err != nil {
		return Config{}, fmt.Errorf("validate config %s: %w", path, err)
	}
	if !result.Valid() {
		errs := result.Errors()
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.String()
		}
		return Config{}, fmt.Errorf("config %s fails schema validation:\n  %s",
			path, joinLines(msgs))
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	if err := checkFixtures(cfg, time.Now()); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// checkFixtures rejects a test-data entry whose expires_on is not a date,
// or is more than a year after now: it cannot have been added less than a
// year before it expires.
func checkFixtures(cfg Config, now time.Time) error {
	for _, f := range cfg.AllowList.TestFixtures {
		d, err := time.Parse("2006-01-02", f.ExpiresOn)
		if err != nil {
			return fmt.Errorf("allow_list.test_fixtures %s: expires_on %q is not a date", f.Path, f.ExpiresOn)
		}
		if d.Sub(now) > MaxFixtureLife {
			return fmt.Errorf("allow_list.test_fixtures %s: expires_on %s is more than one year ahead", f.Path, f.ExpiresOn)
		}
	}
	return nil
}

// WriteConfig marshals cfg as canonical JSON (2-space indent, UTF-8) and
// writes it to path. The file is created if it does not exist.
// Invariant: this function may only be called with paths under the two allowed
// write targets. The caller (rosmarops init) is responsible for enforcing this.
func WriteConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
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

// ReposUnder returns the direct subdirectories of dir that contain a .git
// entry (a directory, or a file for worktrees and submodules), as absolute
// paths in alphabetical order. Deeper levels are not searched. It only reads.
func ReposUnder(dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	repos := []string{}
	for _, e := range entries { // os.ReadDir sorts by name
		sub := filepath.Join(abs, e.Name())
		if info, err := os.Stat(sub); err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(sub, ".git")); err == nil {
			repos = append(repos, sub)
		}
	}
	return repos, nil
}
