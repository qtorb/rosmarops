// Package company reads what an administrator sets for RosmarOps on a managed
// computer: the company configuration (company.json) and the administrator
// fingerprint list (binaries.sha256). Both live in a machine folder that only
// administrators can write: <ProgramData>\RosmarOps on Windows, resolved with
// the Windows API and never from the environment, and /etc/rosmarops on Linux.
// macOS has no company mode.
//
// RosmarOps only reads these files, and only after checking, before opening
// them, that they and their folder are owned by and writable only by
// administrators. A file that fails the check does not exist for RosmarOps.
// Nothing here writes, creates or changes the permissions of a file, and
// nothing here uses the network.
package company

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// The file names in the company folder.
const (
	ConfigName = "company.json"
	ListName   = "binaries.sha256"
)

// maxFileBytes caps what is read from either file: both are a few lines.
const maxFileBytes = 64 << 10

// The state of the company configuration, as report.v2's company.state names
// it; Absent is no company.json at all.
const (
	Absent    = ""
	Trusted   = "trusted"
	Untrusted = "untrusted"
	Invalid   = "invalid"
)

// testRoot replaces the machine folder in test builds only, set with
// -ldflags "-X github.com/qtorb/rosmarops/internal/company.testRoot=<dir>".
// A release build never sets it (the release workflow sets main.version
// only), so a real run always reads the fixed machine folder.
var testRoot string

// Dir is the company folder of this computer: <ProgramData>\RosmarOps on
// Windows, /etc/rosmarops on Linux. It is "" where there is no company mode
// (macOS) or the folder cannot be resolved.
func Dir() string {
	if testRoot != "" {
		return testRoot
	}
	return machineDir()
}

// ErrUntrusted is a file, or its folder, that someone other than an
// administrator could change, or that is a link.
var ErrUntrusted = errors.New("company: not owned and writable only by administrators")

// Config mirrors schemas/company.v1.json.
type Config struct {
	SchemaVersion int `json:"schema_version"`
	Summary       struct {
		Enabled bool `json:"enabled"`
	} `json:"summary"`
	FirstRun *struct {
		RepoRoots []string `json:"repo_roots"`
	} `json:"first_run,omitempty"`
}

// Loaded is the company configuration as this run found it.
type Loaded struct {
	// State is Absent, Trusted, Untrusted or Invalid.
	State string
	// Path is company.json's path; empty when there is no company folder.
	Path string
	// SHA256 is the fingerprint of the file read (Trusted only).
	SHA256 string
	// Config is the configuration (Trusted only).
	Config Config
}

// SummaryEnabled reports a trusted configuration that turns the summary on.
func (l Loaded) SummaryEnabled() bool { return l.State == Trusted && l.Config.Summary.Enabled }

// RepoRoots are first_run.repo_roots of a trusted configuration, as written
// (each starts with "~/").
func (l Loaded) RepoRoots() []string {
	if l.State != Trusted || l.Config.FirstRun == nil {
		return nil
	}
	return append([]string(nil), l.Config.FirstRun.RepoRoots...)
}

var compiledConfig *schemaval.Schema

// LoadConfig reads company.json from dir. The ownership check runs before
// the file is opened: a file that fails it is Untrusted and is not read. A
// file that is not valid against company.v1 is Invalid. dir "" (no company
// mode) gives Absent.
func LoadConfig(dir string) Loaded {
	if dir == "" {
		return Loaded{}
	}
	path := filepath.Join(dir, ConfigName)
	out := Loaded{Path: path}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return Loaded{}
	}
	raw, err := readTrusted(path)
	if err != nil {
		out.State = Untrusted
		return out
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		out.State = Invalid
		return out
	}
	out.State, out.SHA256, out.Config = Trusted, sha256Hex(raw), cfg
	return out
}

// parseConfig validates raw against company.v1 and decodes it.
func parseConfig(raw []byte) (Config, error) {
	if compiledConfig == nil {
		s, err := schemaval.Compile(schemas.CompanyV1Schema)
		if err != nil {
			return Config{}, fmt.Errorf("compile company schema: %w", err)
		}
		compiledConfig = s
	}
	res, err := compiledConfig.Validate(raw)
	if err != nil {
		return Config{}, fmt.Errorf("validate company configuration: %w", err)
	}
	if !res.Valid() {
		return Config{}, errors.New("company configuration fails schema validation")
	}
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode company configuration: %w", err)
	}
	return cfg, nil
}

// ResolveRoots turns first_run.repo_roots ("~/source") into folders under
// home. A root that would leave home ("~/../x") is refused, and so is the
// whole list with it.
func ResolveRoots(home string, roots []string) ([]string, error) {
	var out []string
	for _, r := range roots {
		rel, ok := strings.CutPrefix(r, "~/")
		if !ok {
			return nil, fmt.Errorf("company: repo root %q does not start with ~/", r)
		}
		for _, part := range strings.FieldsFunc(rel, func(c rune) bool { return c == '/' || c == '\\' }) {
			if part == ".." {
				return nil, fmt.Errorf("company: repo root %q leaves the user folder", r)
			}
		}
		out = append(out, filepath.Join(home, filepath.FromSlash(rel)))
	}
	return out, nil
}

// Entry is one line of the administrator fingerprint list.
type Entry struct {
	SHA256  string
	Version string
}

// listLine is "<64 lowercase hex><two spaces><semver>".
var listLine = regexp.MustCompile(`^([a-f0-9]{64})  ([0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?)$`)

// ParseList reads binaries.sha256: one accepted binary per line, lines that
// start with # ignored. A single malformed line invalidates the whole list.
func ParseList(raw []byte) ([]Entry, error) {
	var out []Entry
	for i, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "#") {
			continue
		}
		m := listLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("company: binaries.sha256 line %d is malformed", i+1)
		}
		out = append(out, Entry{SHA256: m[1], Version: m[2]})
	}
	return out, nil
}

// LoadList reads the administrator fingerprint list from dir. ok is false
// when there is none: no company folder, no file, a file or folder that fails
// the ownership check, or a malformed line.
func LoadList(dir string) (list []Entry, ok bool) {
	if dir == "" {
		return nil, false
	}
	raw, err := readTrusted(filepath.Join(dir, ListName))
	if err != nil {
		return nil, false
	}
	list, err = ParseList(raw)
	return list, err == nil
}

// Lists reports whether the list holds sha with exactly version.
func Lists(list []Entry, sha, version string) bool {
	for _, e := range list {
		if e.SHA256 == sha && e.Version == version {
			return true
		}
	}
	return false
}

// readTrusted reads path after the ownership check of the file and its
// folder (and, on Linux, every folder up to the root).
func readTrusted(path string) ([]byte, error) {
	if err := checkTrusted(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("company: %s is larger than %d bytes", path, maxFileBytes)
	}
	return os.ReadFile(path)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
