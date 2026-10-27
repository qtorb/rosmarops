// Package credfiles is the credential files collector (decision 2026-10-24,
// D1 and D2): .env files of every repository and a fixed list of the user's
// credential files. It runs no git, no subprocess and opens no socket. Every
// value read is a SecretValue: used only for fingerprints and claims, never
// logged or persisted.
package credfiles

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/globals"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// MaxFilesPerRepo is the per-repository limit of .env files read (decision
// 2026-10-24, D2). Hardcoded.
const MaxFilesPerRepo = 50

// MaxFileBytes is the content-scan size limit (invariant 6).
const MaxFileBytes = 2 * 1024 * 1024

// format is how a credential file is parsed.
type format int

const (
	formatEnv            format = iota // KEY=VALUE lines (.env)
	formatNpmrc                        // npmrc: the _authToken keys only
	formatINI                          // ini key = value lines (~/.aws/credentials)
	formatGitCredentials               // one URL per line, scheme://user:secret@host
	formatGhHosts                      // GitHub CLI hosts.yml: oauth_token keys
	formatRailway                      // Railway CLI config JSON: user.token, user.accessToken, user.refreshToken
	formatLines                        // any other file: each line, key=value or key: value when it looks so
	formatVSCodeSettings               // VS Code user settings.json (JSONC): terminal.integrated.env.* only
	formatCodexAuth                    // Codex auth.json: OPENAI_API_KEY, tokens.*_token, personal_access_token
)

// userFile is one of the user's credential files.
type userFile struct {
	path   string
	format format
}

// Env reads environment variables; os.LookupEnv outside tests.
type Env func(string) (string, bool)

// userFiles lists the user's credential files (decision 2026-10-24, D1), as
// documented, or as candidates where no documentation could be verified
// (reported in BV-32):
//   - ~/.npmrc, or npm_config_userconfig (npm's docs: "$HOME/.npmrc (or the
//     userconfig param, if set in the environment or on the command line)");
//   - ~/.aws/credentials, or AWS_SHARED_CREDENTIALS_FILE (awscli's
//     config-vars topic: "The shared credentials file has a default
//     location of ~/.aws/credentials. You can change the location of the
//     shared credentials file by setting the AWS_SHARED_CREDENTIALS_FILE
//     environment variable.");
//   - GitHub CLI's hosts.yml in its configuration folder: GH_CONFIG_DIR,
//     $XDG_CONFIG_HOME/gh, $AppData/GitHub CLI (Windows) or
//     $HOME/.config/gh (`gh help environment`). The file name hosts.yml is
//     not in the documentation: unverified;
//   - the Railway CLI's config files (BV-36), from the vendor's source:
//     https://github.com/railwayapp/cli, commit
//     b62467ef1b5303e3e340283ed96425dcf9612993, src/config.rs,
//     root_config_path():
//     Environment::Production => ".railway/config.json",
//     Environment::Staging => ".railway/config-staging.json",
//     Environment::Dev => ".railway/config-dev.json",
//     let home_dir = dirs::home_dir().context("Unable to get home directory")?;
//     (the keys read are documented on parseRailway);
//   - unverified candidates: ~/.git-credentials and
//     $XDG_CONFIG_HOME/git/credentials (git credential-store),
//     ~/.supabase/access-token.
func userFiles(home string, env Env) []userFile {
	if home == "" {
		return nil
	}
	var out []userFile
	if p := globals.UserNpmrcPath(home); p != "" {
		out = append(out, userFile{p, formatNpmrc})
	}
	aws := filepath.Join(home, ".aws", "credentials")
	if v, ok := env("AWS_SHARED_CREDENTIALS_FILE"); ok && v != "" {
		aws = v
	}
	out = append(out, userFile{aws, formatINI})

	ghDir := filepath.Join(home, ".config", "gh")
	if v, ok := env("XDG_CONFIG_HOME"); ok && v != "" {
		ghDir = filepath.Join(v, "gh")
	} else if v, ok := env("AppData"); ok && v != "" && runtime.GOOS == "windows" {
		ghDir = filepath.Join(v, "GitHub CLI")
	}
	if v, ok := env("GH_CONFIG_DIR"); ok && v != "" {
		ghDir = v
	}
	out = append(out, userFile{filepath.Join(ghDir, "hosts.yml"), formatGhHosts})

	out = append(out, userFile{filepath.Join(home, ".git-credentials"), formatGitCredentials})
	gitXDG := filepath.Join(home, ".config", "git", "credentials")
	if v, ok := env("XDG_CONFIG_HOME"); ok && v != "" {
		gitXDG = filepath.Join(v, "git", "credentials")
	}
	out = append(out, userFile{gitXDG, formatGitCredentials})
	for _, name := range railwayConfigFiles {
		out = append(out, userFile{filepath.Join(home, ".railway", name), formatRailway})
	}
	out = append(out, userFile{filepath.Join(home, ".supabase", "access-token"), formatLines})

	// VS Code's user settings (decision 2026-10-24, memoria, §2.1: the
	// v1 check vscode_terminal_env_secret becomes a collector source):
	// %APPDATA%\Code\User on Windows, $XDG_CONFIG_HOME/Code/User or
	// ~/.config/Code/User elsewhere
	// (https://code.visualstudio.com/docs/configure/settings#_settings-file-locations).
	vscodeDir := filepath.Join(home, ".config", "Code", "User")
	if v, ok := env("XDG_CONFIG_HOME"); ok && v != "" {
		vscodeDir = filepath.Join(v, "Code", "User")
	}
	if v, ok := env("AppData"); ok && v != "" && runtime.GOOS == "windows" {
		vscodeDir = filepath.Join(v, "Code", "User")
	}
	out = append(out, userFile{filepath.Join(vscodeDir, "settings.json"), formatVSCodeSettings})

	// Agents detected and not evaluated (decision 2026-10-27, agentes
	// nuevos, N1): Codex's auth.json (keys on parseCodexAuth), and Hermes
	// Agent's .env and every profile's (Hermes docs, user-guide/configuration
	// and user-guide/profiles: <H>\.env, <H>\profiles\<name>\.env).
	adEnv := agentdirs.Env(env)
	out = append(out, userFile{filepath.Join(agentdirs.CodexHome(home, adEnv), "auth.json"), formatCodexAuth})
	hermes := agentdirs.HermesHome(home, adEnv)
	out = append(out, userFile{filepath.Join(hermes, ".env"), formatEnv})
	for _, profile := range profileDirs(filepath.Join(hermes, "profiles")) {
		out = append(out, userFile{filepath.Join(profile, ".env"), formatEnv})
	}
	return out
}

// profileDirs lists the folders directly under dir, sorted; links are not
// followed.
func profileDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Type()&os.ModeSymlink == 0 && !repos.IsForcedExclusion(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// Collect reads the user's credential files that exist and, for every repo
// root, the files named .env or .env.* in the root and one folder below it
// (folders in the forced exclusion list are not entered; symbolic links are
// not followed). At most MaxFilesPerRepo files per repository are read, the
// first in lexicographic order of their relative path; the rest go to
// coverage as file_count_limit_exceeded.
func Collect(home string, repoRoots []string, env Env) platform.CredentialFileScan {
	if env == nil {
		env = os.LookupEnv
	}
	var scan platform.CredentialFileScan
	for _, f := range userFiles(home, env) {
		exists := readFile(&scan, f.path, f.format)
		slog.Info("credentials: user_file", "path", f.path, "exists", exists)
	}
	maxPerRepo, total := 0, 0
	for _, root := range repoRoots {
		files, symlinks := envFiles(root)
		for _, l := range symlinks {
			scan.Failed = append(scan.Failed, platform.CredentialFailure{
				Path: l, Reason: graph.Reason(graph.ReasonSourceUnsupported, "symbolic link not followed"),
			})
		}
		slog.Info("credentials: repo", "repo", root, "env_files", len(files))
		maxPerRepo, total = max(maxPerRepo, len(files)), total+len(files)
		if len(files) > MaxFilesPerRepo {
			scan.Failed = append(scan.Failed, platform.CredentialFailure{
				Path: root, Reason: graph.Reason(graph.ReasonFileCountLimitExceeded, root),
			})
			files = files[:MaxFilesPerRepo]
		}
		for _, f := range files {
			readFile(&scan, f, formatEnv)
		}
	}
	slog.Info("credentials: summary", "repos", len(repoRoots), "max_env_files_per_repo", maxPerRepo, "total_env_files", total)
	return scan
}

// isEnvName reports a file named .env or .env.<anything>.
func isEnvName(name string) bool {
	return name == ".env" || strings.HasPrefix(name, ".env.")
}

// envFiles lists the .env files of a repository in its root and in each
// folder directly under it, sorted by path, and the symbolic links with such
// a name (not followed). Folders in the forced exclusion list, and links to
// folders, are not entered.
func envFiles(root string) (files, symlinks []string) {
	add := func(dir string) (subdirs []string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			switch {
			case e.Type()&os.ModeSymlink != 0:
				if isEnvName(e.Name()) {
					symlinks = append(symlinks, p)
				}
			case e.IsDir():
				if !repos.IsForcedExclusion(e.Name()) {
					subdirs = append(subdirs, p)
				}
			case e.Type().IsRegular() && isEnvName(e.Name()):
				files = append(files, p)
			}
		}
		return subdirs
	}
	for _, sub := range add(root) {
		add(sub)
	}
	sort.Strings(files)
	sort.Strings(symlinks)
	return files, symlinks
}

// readFile reads one credential file if it exists (os.Lstat: a symbolic link
// is not followed) and adds its entries. It reports whether the file exists.
func readFile(scan *platform.CredentialFileScan, path string, f format) bool {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return false
	case err != nil:
		scan.Failed = append(scan.Failed, platform.CredentialFailure{Path: path, Reason: graph.Reason(graph.ReasonReadError, err.Error())})
		return true
	case info.Mode()&os.ModeSymlink != 0:
		scan.Failed = append(scan.Failed, platform.CredentialFailure{Path: path, Reason: graph.Reason(graph.ReasonSourceUnsupported, "symbolic link not followed")})
		return true
	case !info.Mode().IsRegular():
		return true
	case info.Size() > MaxFileBytes:
		scan.Failed = append(scan.Failed, platform.CredentialFailure{
			Path: path, Reason: graph.Reason(graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), MaxFileBytes)),
		})
		return true
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		scan.Failed = append(scan.Failed, platform.CredentialFailure{Path: path, Reason: graph.Reason(graph.ReasonReadError, err.Error())})
		return true
	}
	if f == formatRailway || f == formatVSCodeSettings || f == formatCodexAuth {
		parse := parseRailway
		switch f {
		case formatVSCodeSettings:
			parse = parseVSCodeSettings
		case formatCodexAuth:
			parse = parseCodexAuth
		}
		entries, err := parse(path, raw)
		if err != nil {
			scan.Failed = append(scan.Failed, platform.CredentialFailure{Path: path, Reason: graph.Reason(graph.ReasonParseError, err.Error())})
			return true
		}
		scan.Read = append(scan.Read, path)
		scan.Entries = append(scan.Entries, entries...)
		return true
	}
	scan.Read = append(scan.Read, path)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), MaxFileBytes+1)
	for n := 1; sc.Scan(); n++ {
		if key, value, ok := parseLine(f, sc.Text()); ok {
			scan.Entries = append(scan.Entries, platform.CredentialEntry{
				Path: path, Line: n, Key: key, Value: secrets.NewSecretValue(value),
			})
		}
	}
	return true
}

// railwayConfigFiles are the Railway CLI's config files in ~/.railway, one
// per environment (Production, Staging, Dev).
var railwayConfigFiles = []string{"config.json", "config-staging.json", "config-dev.json"}

// railwayKeys are the keys read under "user" in a Railway CLI config file,
// with the credential kind of each. Source: https://github.com/railwayapp/cli,
// commit b62467ef1b5303e3e340283ed96425dcf9612993, src/config.rs:
//
//	#[serde(rename_all = "camelCase")]
//	pub struct RailwayUser {
//	    pub id: Option<String>,
//	    pub token: Option<String>,
//	    pub access_token: Option<String>,
//	    pub refresh_token: Option<String>,
//	    pub token_expires_at: Option<i64>,
//	}
//
//	#[serde(rename_all = "camelCase")]
//	pub struct RailwayConfig {
//	    pub projects: BTreeMap<String, LinkedProject>,
//	    pub user: RailwayUser,
var railwayKeys = []struct{ key, kind string }{
	{"token", "railway_cli_token"},
	{"accessToken", "railway_cli_access_token"},
	{"refreshToken", "railway_cli_refresh_token"},
}

// parseRailway returns the non-empty user.token, user.accessToken and
// user.refreshToken of a Railway CLI config file, located by JSON Pointer.
// Keys match exactly, as serde does. No other field is kept: the rest of
// the file (projects, editor, sandboxes...) is skipped. The error text never
// holds file content.
func parseRailway(path string, raw []byte) ([]platform.CredentialEntry, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, errors.New("not a JSON object")
	}
	userRaw, ok := root["user"]
	if !ok {
		return nil, nil
	}
	var user map[string]json.RawMessage
	if err := json.Unmarshal(userRaw, &user); err != nil {
		return nil, errors.New("user is not a JSON object")
	}
	var out []platform.CredentialEntry
	for _, k := range railwayKeys {
		v, ok := user[k.key]
		if !ok {
			continue
		}
		var s *string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("user.%s is not a string", k.key)
		}
		if s == nil || strings.TrimSpace(*s) == "" {
			continue
		}
		out = append(out, platform.CredentialEntry{
			Path: path, Key: k.key, Pointer: "/user/" + k.key, Kind: k.kind,
			Value: secrets.NewSecretValue(*s),
		})
	}
	return out, nil
}

// codexAuthKeys are the secrets read from Codex's auth.json, by JSON
// Pointer, with the credential kind of each. Source:
// https://github.com/openai/codex, commit
// b89952d582beaae3af3ddd7b679286cbb094f8b9,
// codex-rs/login/src/auth/storage.rs (AuthDotJson: #[serde(rename =
// "OPENAI_API_KEY")] openai_api_key, tokens: Option<TokenData>,
// personal_access_token) and codex-rs/login/src/token_data.rs (TokenData:
// id_token, serialized as the raw JWT, access_token, refresh_token).
var codexAuthKeys = []struct{ pointer, kind string }{
	{"/OPENAI_API_KEY", "codex_openai_api_key"},
	{"/tokens/access_token", "codex_access_token"},
	{"/tokens/refresh_token", "codex_refresh_token"},
	{"/tokens/id_token", "codex_id_token"},
	{"/personal_access_token", "codex_personal_access_token"},
}

// parseCodexAuth returns the non-empty string values of codexAuthKeys in a
// Codex auth.json, located by JSON Pointer. Keys match exactly, as serde
// does; nothing else of the file is kept (account ids, Bedrock keys, the
// agent identity). The error text never holds file content.
func parseCodexAuth(path string, raw []byte) ([]platform.CredentialEntry, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, errors.New("not a JSON object")
	}
	var out []platform.CredentialEntry
	for _, k := range codexAuthKeys {
		parts := strings.Split(strings.TrimPrefix(k.pointer, "/"), "/")
		obj, v, ok := root, json.RawMessage(nil), true
		for i, part := range parts {
			v, ok = obj[part]
			if !ok || i == len(parts)-1 {
				break
			}
			obj = nil
			if err := json.Unmarshal(v, &obj); err != nil {
				return nil, fmt.Errorf("%s is not a JSON object", strings.Join(parts[:i+1], "."))
			}
			if obj == nil {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		var s *string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("%s is not a string", strings.Join(parts, "."))
		}
		if s == nil || strings.TrimSpace(*s) == "" {
			continue
		}
		out = append(out, platform.CredentialEntry{
			Path: path, Key: parts[len(parts)-1], Pointer: k.pointer, Kind: k.kind,
			Value: secrets.NewSecretValue(*s),
		})
	}
	return out, nil
}

// vscodeTerminalEnv are the settings with the terminal's environment
// variables, one per platform
// (https://code.visualstudio.com/docs/terminal/profiles#_configuring-profiles).
var vscodeTerminalEnv = []string{"terminal.integrated.env.windows", "terminal.integrated.env.linux", "terminal.integrated.env.osx"}

// parseVSCodeSettings returns the string values of the
// terminal.integrated.env.* objects of a VS Code settings file (JSONC),
// located by JSON Pointer. Nothing else of the file is kept; the error
// text never holds file content.
func parseVSCodeSettings(path string, raw []byte) ([]platform.CredentialEntry, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(jsonc.Strip(raw), &root); err != nil || root == nil {
		return nil, errors.New("not a JSON object")
	}
	esc := strings.NewReplacer("~", "~0", "/", "~1")
	var out []platform.CredentialEntry
	for _, setting := range vscodeTerminalEnv {
		v, ok := root[setting]
		if !ok {
			continue
		}
		var vars map[string]any
		if err := json.Unmarshal(v, &vars); err != nil {
			return nil, fmt.Errorf("%s is not a JSON object", setting)
		}
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s, ok := vars[k].(string)
			if !ok || strings.TrimSpace(s) == "" {
				continue // null unsets a variable
			}
			out = append(out, platform.CredentialEntry{
				Path: path, Key: k, Pointer: "/" + esc.Replace(setting) + "/" + esc.Replace(k),
				Value: secrets.NewSecretValue(s),
			})
		}
	}
	return out, nil
}

// parseLine returns the key and value of one line, or ok false for a line
// that holds none (blank, comment, section header, other npmrc keys).
func parseLine(f format, line string) (key, value string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
		return "", "", false
	}
	switch f {
	case formatEnv:
		t = strings.TrimSpace(strings.TrimPrefix(t, "export "))
		k, v, found := strings.Cut(t, "=")
		if !found {
			return "", "", false
		}
		return strings.TrimSpace(k), unquote(strings.TrimSpace(v)), true
	case formatNpmrc:
		k, v, found := strings.Cut(t, "=")
		if !found || !strings.HasSuffix(strings.TrimSpace(k), "_authToken") {
			return "", "", false
		}
		return "_authToken", unquote(strings.TrimSpace(v)), true
	case formatINI:
		if strings.HasPrefix(t, "[") {
			return "", "", false
		}
		k, v, found := strings.Cut(t, "=")
		if !found {
			return "", "", false
		}
		return strings.TrimSpace(k), strings.TrimSpace(v), true
	case formatGitCredentials:
		// scheme://user:password@host/... (git-credential-store).
		_, rest, found := strings.Cut(t, "://")
		if !found {
			return "", "", false
		}
		userinfo, _, found := strings.Cut(rest, "@")
		if !found {
			return "", "", false
		}
		_, pw, found := strings.Cut(userinfo, ":")
		if !found || pw == "" {
			return "", "", false
		}
		if dec, err := percentDecode(pw); err == nil {
			pw = dec
		}
		return "password", pw, true
	case formatGhHosts:
		k, v, found := strings.Cut(t, ":")
		if !found || strings.TrimSpace(k) != "oauth_token" {
			return "", "", false
		}
		return "oauth_token", unquote(strings.TrimSpace(v)), true
	default:
		for _, sep := range []string{"=", ":"} {
			if k, v, found := strings.Cut(t, sep); found {
				return strings.Trim(strings.TrimSpace(k), `"`), unquote(strings.TrimRight(strings.TrimSpace(v), ",")), true
			}
		}
		return "", t, true
	}
}

// unquote removes one pair of matching surrounding quotes.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// percentDecode decodes %XX escapes (git-credential-store writes the
// password URL-encoded).
func percentDecode(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("bad escape")
		}
		var v byte
		for _, c := range []byte(s[i+1 : i+3]) {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v |= c - 'A' + 10
			default:
				return "", fmt.Errorf("bad escape")
			}
		}
		b.WriteByte(v)
		i += 2
	}
	return b.String(), nil
}

// AddEnvFiles reads the envFile of Cursor's MCP servers (decision
// 2026-10-25, Cursor, K10) into scan, as .env files: each path once, and
// not again when the repository scan already read it.
func AddEnvFiles(scan platform.CredentialFileScan, paths []string) platform.CredentialFileScan {
	done := map[string]bool{}
	for _, p := range scan.Read {
		done[graph.CanonicalPath(p)] = true
	}
	for _, f := range scan.Failed {
		done[graph.CanonicalPath(f.Path)] = true
	}
	for _, p := range paths {
		key := graph.CanonicalPath(p)
		if done[key] {
			continue
		}
		done[key] = true
		exists := readFile(&scan, p, formatEnv)
		slog.Info("credentials: mcp env_file", "path", p, "exists", exists)
	}
	return scan
}
