// Package globals detects new globally-installed npm/pnpm packages and
// non-default npm registry settings in ~/.npmrc.
//
// Global packages are YELLOW (new dependency, needs review).
// A non-standard npm registry is RED (potential supply-chain attack vector).
package globals

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Finding text — approved by human owner (review required before commit).
// ---------------------------------------------------------------------------

const (
	whyGlobalPackageNew = "A globally-installed npm or pnpm package appeared that was not in your " +
		"baseline. Global packages run with your user privileges and are implicitly available to every " +
		"project on your machine without appearing in any project's package.json. A compromised or " +
		"malicious global package can intercept build steps, inject code into outputs, or exfiltrate " +
		"credentials from any project that shares your PATH."

	actionGlobalPackageNew = "Confirm you installed this package intentionally. If yes, add an entry " +
		"under `allow_list.global_packages` in rosmarops.config.json with the package name as `id` and a " +
		"note explaining why it is installed globally; future runs will silently accept it. If you did " +
		"not install it, remove it immediately with `npm uninstall -g <name>` and investigate what " +
		"installed it."

	whyNpmRegistryNonStandard = "Your global ~/.npmrc file points npm at a registry other than the " +
		"official `registry.npmjs.org`. Every `npm install` on this machine resolves packages from " +
		"this registry, which may serve different or backdoored package versions. Non-default registries " +
		"are the primary mechanism for dependency confusion attacks, package substitution attacks, and " +
		"supply-chain compromises targeting npm workflows."

	actionNpmRegistryNonStandard = "Open ~/.npmrc and confirm the registry URL is exactly what you " +
		"intended to configure. If this is a legitimate private registry (e.g., your organization's " +
		"Artifactory or Verdaccio), verify the URL matches your registry's documented endpoint and that " +
		"you set it intentionally. If you did not configure this registry, remove the `registry=` line " +
		"immediately, rotate any auth tokens stored in ~/.npmrc, and investigate what modified the file."
)

// defaultNpmRegistry is the official npm registry URL (normalized with trailing slash).
const defaultNpmRegistry = "https://registry.npmjs.org/"

// ScanResult holds the output of ScanGlobals.
type ScanResult struct {
	// Findings contains one YELLOW finding per new global package and one RED
	// finding if a non-standard npm registry is detected.
	Findings []finding.Finding

	// NewPackageBaseline is the full current global package set (npm + pnpm).
	// The caller must persist this to state.Baselines.GlobalPackages.
	NewPackageBaseline []state.GlobalPackage

	// NewNpmrcSHA is the SHA-256 of the current ~/.npmrc content (or empty
	// string if the file does not exist). The caller must persist this to
	// state.Baselines.NpmrcSHA256.
	NewNpmrcSHA string

	// AllowListedCount is the number of new packages suppressed by the allow-list.
	AllowListedCount int

	// Hits are the warnings the allow-list hid, one per warning, with the
	// entry that hid it (decision 2026-10-27, blindaje, B9).
	Hits []config.AllowHit

	// Observed are the global packages read this run (npm + pnpm), with the
	// package.json each was read from, for the threat-intel cross (BV-100).
	Observed []ObservedGlobal
}

// ScanGlobals collects global npm and pnpm packages, checks ~/.npmrc for a
// non-standard registry, and compares packages against the baseline.
//
// homeDir is the user's home directory (os.UserHomeDir()). Pass an empty string
// to skip the npmrc check.
//
// Packages are read from npm's and pnpm's global directories on disk; no
// package manager is run. A missing directory is silently skipped — not an
// error. The ScanResult will reflect only the managers found on disk.
func ScanGlobals(
	ctx context.Context,
	homeDir string,
	baseline []state.GlobalPackage,
	allowList []config.AllowEntryID,
) ScanResult {
	// Build baseline lookup: norm(manager+":"+name) → true.
	baselined := make(map[string]bool, len(baseline))
	for _, b := range baseline {
		baselined[normPkg(b.Manager, b.Name)] = true
	}

	// Build allow-list lookup: norm(name) → true (name-only, version-agnostic).
	allowed := make(map[string]config.AllowEntryID, len(allowList))
	for _, a := range allowList {
		allowed[strings.ToLower(strings.TrimSpace(a.ID))] = a
	}
	var hits []config.AllowHit

	var findings []finding.Finding
	var newPkgBL []state.GlobalPackage
	var observed []ObservedGlobal
	allowListedCount := 0

	// Collect globals from each supported manager.
	// Package directories are resolved from the home directory even when the
	// npmrc check is skipped (homeDir == "").
	resolveHome := homeDir
	if resolveHome == "" {
		resolveHome, _ = os.UserHomeDir()
	}
	managers := []struct {
		name    string
		collect func(ctx context.Context, home string) ([]GlobalPackage, error)
	}{
		{"npm", listNpmGlobals},
		{"pnpm", listPnpmGlobals},
	}

	for _, mgr := range managers {
		pkgs, err := mgr.collect(ctx, resolveHome)
		if err != nil {
			slog.Warn("globals: list failed", "manager", mgr.name, "err", err)
			continue
		}
		for _, p := range pkgs {
			newPkgBL = append(newPkgBL, state.GlobalPackage{
				Manager: mgr.name,
				Name:    p.Name,
				Version: p.Version,
			})
			observed = append(observed, ObservedGlobal{
				Manager: mgr.name,
				Name:    p.Name,
				Version: p.Version,
				Path:    p.Path,
			})
			if baselined[normPkg(mgr.name, p.Name)] {
				continue // known package — no finding
			}
			if a, ok := allowed[strings.ToLower(p.Name)]; ok {
				allowListedCount++
				hits = append(hits, config.AllowHit{List: config.ListGlobalPackages, ID: a.ID, Note: a.Note})
				continue
			}
			id := globalPkgFindingID(mgr.name, p.Name)
			f := finding.NewFinding(id, finding.SeverityYellow, "global_package_new",
				fmt.Sprintf("New global %s package: %s@%s", mgr.name, p.Name, p.Version))
			f.WhyItMatters = whyGlobalPackageNew
			f.SuggestedAction = actionGlobalPackageNew
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				Extra: map[string]string{
					"package_manager": mgr.name,
					"package_name":    p.Name,
					"package_version": p.Version,
				},
			}
			findings = append(findings, f)
		}
	}

	// Check ~/.npmrc for non-standard registry.
	var newNpmrcSHA string
	if homeDir != "" {
		npmrcPath := userNpmrcPath(homeDir)
		npmrcFindings, sha := scanNpmrc(npmrcPath)
		findings = append(findings, npmrcFindings...)
		newNpmrcSHA = sha
	}

	return ScanResult{
		Findings:           findings,
		NewPackageBaseline: newPkgBL,
		NewNpmrcSHA:        newNpmrcSHA,
		AllowListedCount:   allowListedCount,
		Hits:               hits,
		Observed:           observed,
	}
}

// ---------------------------------------------------------------------------
// Global package collectors (read from disk; see disk.go)
// ---------------------------------------------------------------------------

// GlobalPackage is a resolved global package entry.
type GlobalPackage struct {
	Name    string
	Version string
	// Path is the package.json the name and version were read from (for the
	// threat-intel cross, BV-100).
	Path string
}

// ObservedGlobal is a global package read this run, with the manager it belongs
// to and the package.json it was read from. The threat-intel cross (BV-100)
// crosses these against the embedded snapshot.
type ObservedGlobal struct {
	Manager string
	Name    string
	Version string
	Path    string
}

// listNpmGlobals lists npm's global packages from its global node_modules
// directory. Returns nil when no npm prefix can be resolved.
func listNpmGlobals(ctx context.Context, home string) ([]GlobalPackage, error) {
	dir, err := npmGlobalModulesDir(home)
	if err != nil || dir == "" {
		return nil, err
	}
	return collectModules(ctx, "npm", []string{dir}), nil
}

// listPnpmGlobals lists pnpm's global packages from its global directory.
// Returns nil when the directory does not exist.
func listPnpmGlobals(ctx context.Context, home string) ([]GlobalPackage, error) {
	dirs, err := pnpmGlobalModulesDirs(home)
	if err != nil {
		return nil, err
	}
	return collectModules(ctx, "pnpm", dirs), nil
}

// collectModules reads every node_modules directory in dirs. An unreadable
// directory or package.json is logged and skipped.
func collectModules(ctx context.Context, manager string, dirs []string) []GlobalPackage {
	var pkgs []GlobalPackage
	for _, dir := range dirs {
		if ctx.Err() != nil {
			return pkgs
		}
		found, errs := readGlobalModules(dir)
		for _, err := range errs {
			slog.Warn("globals: read failed", "manager", manager, "err", err)
		}
		pkgs = append(pkgs, found...)
	}
	return pkgs
}

// ---------------------------------------------------------------------------
// ~/.npmrc scanner
// ---------------------------------------------------------------------------

// scanNpmrc reads the npmrc file at path, checks for a non-standard registry,
// and returns any finding plus the SHA-256 of the file content.
func scanNpmrc(path string) ([]finding.Finding, string) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "" // no .npmrc — not an error
		}
		slog.Debug("globals: could not read .npmrc", "path", path, "err", err)
		return nil, ""
	}

	sha := hash.Bytes(content)
	registry := ParseNpmrcRegistry(content)
	if registry == "" {
		return nil, sha
	}

	f := finding.NewFinding(
		"npm_registry_non_standard:0",
		finding.SeverityRed,
		"npm_registry_non_standard",
		"Non-standard npm registry in ~/.npmrc: "+registry,
	)
	f.WhyItMatters = whyNpmRegistryNonStandard
	f.SuggestedAction = actionNpmRegistryNonStandard
	f.Confidence = "high"
	f.Evidence = finding.Evidence{
		Extra: map[string]string{
			"registry_url": registry,
			"npmrc_path":   path,
		},
	}
	return []finding.Finding{f}, sha
}

// ParseNpmrcRegistry scans npmrc content for a `registry=` line and returns
// the URL if it differs from the official npm registry.
// Returns empty string if no non-default registry is found.
// Exported for unit testing.
func ParseNpmrcRegistry(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue // comment
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "registry" {
			continue
		}
		url := strings.TrimSpace(val)
		// Normalize: add trailing slash for comparison.
		normalized := url
		if !strings.HasSuffix(normalized, "/") {
			normalized += "/"
		}
		if normalized != defaultNpmRegistry {
			return url
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func globalPkgFindingID(manager, name string) string {
	h := hash.Bytes([]byte(manager + ":" + name))
	return "global_package_new:" + h[:16]
}

func normPkg(manager, name string) string {
	return strings.ToLower(manager) + ":" + strings.ToLower(strings.TrimSpace(name))
}
