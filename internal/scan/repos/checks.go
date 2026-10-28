package repos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Finding text — approved by human owner (review required before commit).
// ---------------------------------------------------------------------------

const (
	// secret_tracked --------------------------------------------------------

	whySecretTracked = "A secret credential was found in a file that is currently tracked by git. " +
		"Even if the file is later removed, the credential remains in the repository's history " +
		"and can be extracted by anyone with read access to the repository — including future " +
		"collaborators, forks, and hosted services."

	actionSecretTracked = "Revoke and rotate the credential immediately — assume it is compromised. " +
		"Remove the secret from the file and purge it from git history using `git filter-repo` " +
		"(preferred) or BFG Repo-Cleaner. Then add the file or pattern to .gitignore to prevent " +
		"future accidental commits. Deleting the file in a new commit is not sufficient; the " +
		"credential is still accessible in the old commit."

	// secret_in_history ------------------------------------------------------

	whySecretInHistory = "A secret credential was found in a commit that is part of this " +
		"repository's history. Anyone with read access to the repository can retrieve it using " +
		"standard git commands, regardless of whether the file has since been modified or deleted. " +
		"Rotating the secret without cleaning history leaves the old credential exposed indefinitely."

	actionSecretInHistory = "Revoke and rotate the credential immediately — assume it is compromised. " +
		"Clean the commit from history using `git filter-repo` (preferred over the deprecated " +
		"`git filter-branch`). Force-push the cleaned history to all remotes and coordinate with " +
		"collaborators to re-clone or rebase. Once confirmed rotated, add an entry under " +
		"`allow_list.secrets_in_history` in rosmarops.config.json with `pattern_type`, `commit_sha`, " +
		"and a note confirming the rotation; future runs will silently accept it."

	// env_tracked ------------------------------------------------------------

	whyEnvTracked = ".env files are designed to hold per-environment configuration including API keys, " +
		"database passwords, and service credentials. Tracking one in git exposes these values to " +
		"everyone with access to the repository — including public forks and every CI/CD system " +
		"that checks out the repo."

	actionEnvTracked = "Remove the file from git tracking immediately: run `git rm --cached <file>` and " +
		"commit the removal. Add the pattern to .gitignore (e.g., .env, .env.*) to prevent future " +
		"commits. If the file contained real credentials, treat them as compromised and rotate them. " +
		"Keep a .env.example with placeholder values for documentation purposes."

	// lifecycle_script_with_network ------------------------------------------

	whyLifecycleNetwork = "A package.json lifecycle script (preinstall, postinstall, or similar) contains " +
		"what appears to be a network command. These scripts run automatically during `npm install`. " +
		"A compromised or malicious package that includes a network lifecycle script can exfiltrate " +
		"data or download and execute additional payloads on every developer machine at install time."

	actionLifecycleNetwork = "Review the script to confirm the network call is intentional and necessary. " +
		"Prefer fetching resources at build time under your explicit control, not at install time. " +
		"If the call is in a dependency you do not control, consider whether that dependency can be " +
		"replaced. You can also set `npm config set ignore-scripts true` globally and opt in only " +
		"for trusted packages."

	// workflow_dangerous_pattern ---------------------------------------------

	whyWorkflowDangerous = "A GitHub Actions workflow contains a pattern commonly associated with " +
		"privilege escalation or untrusted code execution. Workflows triggered by " +
		"`pull_request_target` run with access to repository secrets while the checked-out code " +
		"comes from an untrusted PR. Untrusted input injected into `run:` steps allows an attacker " +
		"to execute arbitrary commands in the workflow context."

	actionWorkflowDangerous = "Review the workflow file immediately. If it uses `pull_request_target`, " +
		"verify it never checks out PR code or passes PR-controlled values into `run:` steps. Use " +
		"`pull_request` (without `_target`) for workflows that operate on untrusted code. Sanitize " +
		"all user-controlled inputs before using them in shell commands. See the GitHub Actions " +
		"security hardening guide for a complete checklist."

	// workflow_new_or_modified -----------------------------------------------

	whyWorkflowModified = "A GitHub Actions workflow file has appeared or changed since the last " +
		"baseline. Workflow files define CI/CD pipelines that execute in response to repository " +
		"events. A modified workflow could introduce privilege escalation, secret exfiltration, or " +
		"supply-chain attacks that affect everyone who pushes to the repository."

	actionWorkflowModified = "Review the workflow diff to confirm you made this change intentionally. " +
		"If yes, run `rosmarops baseline` to record the new state; future runs will accept it. " +
		"If no, revert the workflow file, inspect the git log for unauthorized commits, and rotate " +
		"any secrets the workflow had access to."

	// git_hook_with_network --------------------------------------------------

	whyHookNetwork = "A git hook contains what appears to be a network command. Git hooks run " +
		"automatically on git operations (commits, pushes, checkouts). A hook that makes network " +
		"calls could exfiltrate code, credentials, or commit messages to an external server, or " +
		"download and execute additional payloads. Malicious hooks can be introduced via compromised " +
		"developer tooling, supply-chain attacks, or unauthorized access to your machine."

	actionHookNetwork = "Review the hook script to confirm the network call is intentional and expected. " +
		"If unexpected, treat your machine as potentially compromised and audit recent software installs. " +
		"Inspect the hook's modification timestamp and compare it to when you last intentionally " +
		"modified it. If the call is legitimate (e.g., a CI notification hook), add an entry under " +
		"`allow_list.git_hooks` in rosmarops.config.json with the hook path and a note."

	// git_hook_new_or_modified -----------------------------------------------

	whyHookModified = "A git hook has appeared or changed since the last baseline. Git hooks run " +
		"automatically on git operations and can execute arbitrary code on your machine. A hook " +
		"modified without your knowledge could intercept commits, exfiltrate data, or silently " +
		"alter code before it is committed."

	actionHookModified = "Review the hook script to confirm you made this change intentionally. " +
		"If yes, run `rosmarops baseline` to record the new state; future runs will accept it. " +
		"If no, restore the original hook, inspect when and how it was modified, and audit recent " +
		"software installs that may have modified it."

	// core_hookspath_redirected ----------------------------------------------

	whyCoreHooksPath = "The git configuration key `core.hooksPath` is set to a non-standard location. " +
		"This redirects ALL git hooks for the repository to a directory that may be outside your " +
		"control. An attacker who can write to the redirected path can inject arbitrary code that " +
		"runs on every git operation — commit, push, checkout — without any visible config-file change."

	actionCoreHooksPath = "Confirm you set `core.hooksPath` intentionally. If yes, verify the hooks " +
		"directory is inside the repository and contains only scripts you wrote. Run `rosmarops baseline` " +
		"to record this configuration; future runs will accept it. If no, run " +
		"`git config --unset core.hooksPath` to restore default hook discovery, inspect .git/config " +
		"for other unexpected changes, and audit recent software installs."
)

// ---------------------------------------------------------------------------
// Patterns for structural/behavior checks
// ---------------------------------------------------------------------------

// hookNetworkPattern matches common network-capable commands in hook scripts.
// The trailing \b prevents partial-word matches (e.g. "ncurses" does not match "nc").
var hookNetworkPattern = regexp.MustCompile(
	`\b(curl|wget|ncat|netcat|socat|sftp|telnet)\b` +
		`|` +
		`\bnc\s` + // "nc " (netcat, space required to avoid "include")
		`|` +
		`\bssh\s`, // "ssh " (with space, to avoid "ssh-keygen" etc.)
)

// lifecycleNetworkPattern matches network-capable commands in npm lifecycle scripts.
var lifecycleNetworkPattern = regexp.MustCompile(
	`\b(curl|wget|ncat|netcat|socat|sftp|telnet|fetch)\b` +
		`|` +
		`\bnc\s` +
		`|` +
		`\bssh\s`,
)

// dangerousWorkflowPatterns is the list of dangerous GitHub Actions patterns.
// Each entry has a short ID (used in finding title) and the detection regexp.
// All patterns use RE2 syntax.
var dangerousWorkflowPatterns = []struct {
	id      string
	pattern *regexp.Regexp
	desc    string
}{
	{
		"pull_request_target",
		regexp.MustCompile(`(?m)^\s+pull_request_target\s*:`),
		"pull_request_target trigger (runs with secrets, code from untrusted PR)",
	},
	{
		"script_injection",
		// Matches ${{ github.event.<untrusted-fields> }} in workflow expressions.
		regexp.MustCompile(`\$\{\{[^}]*github\.event\.(pull_request\.head|issue\.body|comment\.body|review\.body|commits\[)[^}]*\}\}`),
		"untrusted GitHub context value in workflow expression (potential script injection)",
	},
	{
		"curl_pipe_shell",
		regexp.MustCompile(`curl\b[^|]*\|\s*(ba)?sh`),
		"curl output piped to shell (remote code execution risk)",
	},
}

// envFileSafeList is the set of .env-prefixed filenames that are explicitly
// safe — they are placeholder/example files, not real credential stores.
var envFileSafeList = map[string]bool{
	".env.example":  true,
	".env.sample":   true,
	".env.template": true,
	".env.test":     true,
	".env.schema":   true,
}

// standardHookNames is the complete list of git hook names we monitor.
var standardHookNames = map[string]bool{
	"pre-commit": true, "prepare-commit-msg": true, "commit-msg": true,
	"post-commit": true, "pre-rebase": true, "post-checkout": true,
	"post-merge": true, "pre-push": true, "pre-receive": true,
	"update": true, "post-receive": true, "post-update": true,
	"reference-transaction": true, "push-to-checkout": true,
	"pre-auto-gc": true, "post-rewrite": true, "sendemail-validate": true,
	"fsmonitor-watchman": true, "p4-changelist": true, "p4-prepare-changelist": true,
	"p4-post-changelist": true, "p4-pre-submit": true, "post-index-change": true,
	"pre-merge-commit": true,
}

// ---------------------------------------------------------------------------
// Check 1: secrets in currently tracked files
// ---------------------------------------------------------------------------

func checkTrackedSecrets(ctx context.Context, repoRoot string, patterns []secrets.Pattern) []finding.Finding {
	trace := traceOf(ctx)
	start := time.Now()
	entries, err := gitLsFilesStaged(ctx, repoRoot)
	trace.lsFilesStagedMs = time.Since(start).Milliseconds()
	trace.trackedFiles = len(entries)
	if err != nil {
		slog.Warn("repos: git ls-files failed", "repo", repoRoot, "err", err)
		markError(ctx)
		return nil
	}
	// Files that differ from the index are read from disk and never cached.
	// If the check itself fails, every file is read from disk.
	cache := cacheOf(ctx)
	start = time.Now()
	dirty, err := gitLsFilesModified(ctx, repoRoot)
	trace.lsFilesModifiedMs = time.Since(start).Milliseconds()
	if err != nil {
		slog.Warn("repos: git ls-files -m failed; content cache not used", "repo", repoRoot, "err", err)
		cache = nil
	}

	var findings []finding.Finding
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		relPath := e.path
		if isExcludedDir(relPath) {
			continue
		}

		absPath := repoRelPath(repoRoot, relPath)
		matches, ok := trackedMatches(trace, cache, e, dirty[relPath], absPath, patterns, identifyOf(ctx).identify)
		if !ok {
			continue
		}
		findings = append(findings, trackedFindings(repoRoot, relPath, matches, identifyOf(ctx))...)
	}
	return findings
}

// trackedMatches returns the secret matches of one tracked file as it is on
// disk, as the check always did; ok is false when the file is not read
// (unreadable or over the size limit). The cache is used only for a file
// that matches its index entry: a regular file (mode 100644 or 100755) at
// stage 0, with tag H (neither assume-unchanged nor skip-worktree), not
// reported by `git ls-files -m`, still a regular file on disk. Its key is
// the blob id and the size on disk.
func trackedMatches(trace *repoTrace, cache *contentCache, e indexEntry, dirty bool, absPath string, patterns []secrets.Pattern, identify secrets.Identify) ([]secrets.SecretMatch, bool) {
	var key *blobKey
	if cache != nil && !dirty && e.stage == "0" && e.tag == "H" && (e.mode == "100644" || e.mode == "100755") {
		if info, err := os.Lstat(absPath); err == nil && info.Mode().IsRegular() && info.Size() <= MaxFileBytes {
			key = &blobKey{blob: e.blob, size: info.Size()}
			if m, ok := cache.blob(*key); ok {
				trace.blobHits++
				return m, true
			}
			trace.blobMisses++
		}
	}
	readStart := time.Now()
	content, err := readFileSized(absPath)
	trace.readTime += time.Since(readStart)
	if err != nil {
		slog.Debug("repos: could not read tracked file", "path", absPath, "err", err)
		return nil, false
	}
	if content == nil {
		trace.filesSkippedSize++
		return nil, false // oversized
	}
	trace.filesRead++
	trace.bytesRead += int64(len(content))
	if trace.patternTime == nil {
		trace.patternTime = map[string]time.Duration{}
	}
	matchStart := time.Now()
	m := secrets.MatchRefs(content, patterns, trace.patternTime, identify)
	trace.matchTime += time.Since(matchStart)
	if key != nil {
		cache.putBlob(*key, m)
	}
	return m, true
}

// trackedFindings emits the findings of one tracked file of this repo.
func trackedFindings(repoRoot, relPath string, matches []secrets.SecretMatch, ic identifyCtx) []finding.Finding {
	var findings []finding.Finding
	for _, m := range matches {
		id := repoFindingID("secret_tracked", repoRoot, relPath+":"+m.PatternID)
		f := finding.NewFinding(id, finding.SeverityRed, "secret_tracked",
			fmt.Sprintf("Secret in tracked file: %s (%s)", relPath, m.Type))
		f.GroupDetail = m.PatternID
		f.WhyItMatters = whySecretTracked
		f.SuggestedAction = actionSecretTracked
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			RepoPath:        repoRoot,
			File:            relPath,
			RedactedPreview: m.RedactedPreview,
		}
		if m.Ref != nil && ic.identify != nil {
			f.Committed = []finding.CommittedSecret{{
				PatternID: m.PatternID, Ref: *m.Ref, Repo: repoRoot, Presence: finding.PresenceTracked,
				File: relPath, Line: m.Line, HasRemote: ic.hasRemote,
			}}
		}
		findings = append(findings, f)
	}
	return findings
}

// ---------------------------------------------------------------------------
// Check 2: secrets in git commit history
// ---------------------------------------------------------------------------

func checkHistorySecrets(
	ctx context.Context,
	repoRoot string,
	patterns []secrets.Pattern,
	allowHistory []config.AllowEntrySecretHistory,
) []finding.Finding {
	// A repository with no commits yet has no history: nothing to scan,
	// and not an error (BV-33).
	if has, err := gitHasHEAD(ctx, repoRoot); err == nil && !has {
		return nil
	}
	since := time.Now().UTC().AddDate(0, -HistoryMaxAgeMonths, 0)
	hashes, err := gitLogHashes(ctx, repoRoot, HistoryMaxCommits, since)
	if err != nil {
		slog.Warn("repos: git log failed for history scan", "repo", repoRoot, "err", err)
		markError(ctx)
		return nil
	}
	trace := traceOf(ctx)
	trace.historyListed = len(hashes)

	// Matches per commit: from the cache, or from one git process for every
	// commit not cached. done is how many commits, from the most recent,
	// are complete: their diff was read to its end (or git printed none,
	// having no added or modified file).
	cache := cacheOf(ctx)
	matches := make(map[string][]secrets.SecretMatch, len(hashes))
	var pending []string
	for _, h := range hashes {
		if m, ok := cache.commit(h); ok {
			matches[h] = m
			trace.commitHits++
		} else {
			pending = append(pending, h)
			if cache != nil {
				trace.commitMisses++
			}
		}
	}
	complete := map[string]bool{}
	if len(pending) > 0 {
		next := 0 // index in pending of the next commit git may print
		ic := identifyOf(ctx)
		take := func(h string, added []byte, files []fileSpan) error {
			// Commits git skipped before h printed no diff: complete.
			for next < len(pending) && pending[next] != h {
				complete[pending[next]] = true
				cache.putCommit(pending[next], nil) // no diff: no match
				next++
			}
			if next == len(pending) {
				return fmt.Errorf("git printed unexpected commit %s", safeShort(h))
			}
			m := secrets.MatchRefs(added, patterns, nil, ic.identify)
			if ic.identify != nil {
				for i := range m {
					m[i].File = fileAt(files, m[i].Offset)
				}
			}
			matches[h], complete[h] = m, true
			cache.putCommit(h, m)
			next++
			return nil
		}
		var lastHash string
		var lastAdded []byte
		var lastFiles []fileSpan
		err := gitStreamDiffs(ctx, repoRoot, pending, func(r io.Reader) error {
			var err error
			lastHash, lastAdded, lastFiles, err = readDiffStreamFiles(r, take)
			return err
		})
		// The last commit is complete only if git exited cleanly.
		if err == nil && lastHash != "" {
			err = take(lastHash, lastAdded, lastFiles)
		}
		if err == nil {
			for _, h := range pending[next:] {
				complete[h] = true // no diff printed: nothing added or modified
				cache.putCommit(h, nil)
			}
		} else {
			slog.Debug("repos: git log --stdin failed", "repo", repoRoot, "err", err)
			markError(ctx)
		}
	}
	for h := range matches {
		complete[h] = true
	}

	// Build allow-set: lowercase(patternType+":"+commitSHA) → present.
	// commitSHA in the allow-list may be abbreviated (7–40 chars).
	allowSet := make(map[string]config.AllowEntrySecretHistory, len(allowHistory))
	for _, a := range allowHistory {
		key := strings.ToLower(a.PatternType) + ":" + strings.ToLower(a.CommitSHA)
		allowSet[key] = a
	}
	hidden := map[string]bool{} // pattern:commit already counted (B9)

	type seenKey struct{ patternID, commitSHA string }
	seen := make(map[seenKey]int) // index in findings + 1
	var findings []finding.Finding
	ic := identifyOf(ctx)

	// Commits in log order, up to the first one not complete (the scan was
	// cut there, as the per-commit loop stopped).
	for _, h := range hashes {
		if !complete[h] {
			break
		}
		trace.historyScanned++
		for _, m := range matches[h] {
			// Allow-list: check both full SHA and 7-char prefix. What it
			// hides is counted, one warning per pattern and commit (B9).
			a, allowed := allowSet[strings.ToLower(m.PatternID)+":"+strings.ToLower(h)]
			if !allowed && len(h) >= 7 {
				a, allowed = allowSet[strings.ToLower(m.PatternID)+":"+strings.ToLower(h[:7])]
			}
			if allowed {
				if k := m.PatternID + ":" + h; !hidden[k] {
					hidden[k] = true
					allowHit(ctx, config.AllowHit{List: config.ListSecretsInHistory, ID: a.PatternType + ":" + a.CommitSHA, Note: a.Note})
				}
				continue
			}
			sk := seenKey{m.PatternID, h}
			if i := seen[sk]; i > 0 {
				findings[i-1].Committed = appendCommitted(findings[i-1].Committed, ic, repoRoot, h, m)
				continue
			}
			seen[sk] = len(findings) + 1

			id := repoFindingID("secret_in_history", repoRoot, h+":"+m.PatternID)
			f := finding.NewFinding(id, finding.SeverityRed, "secret_in_history",
				fmt.Sprintf("Secret in git history: %s (%s)", m.Type, safeShort(h)))
			f.GroupDetail = m.PatternID
			f.WhyItMatters = whySecretInHistory
			f.SuggestedAction = actionSecretInHistory
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RepoPath:        repoRoot,
				CommitSHA:       h,
				RedactedPreview: m.RedactedPreview,
			}
			f.Committed = appendCommitted(nil, ic, repoRoot, h, m)
			findings = append(findings, f)
		}
	}
	return findings
}

// ---------------------------------------------------------------------------
// Check 3: .env-like files tracked by git
// ---------------------------------------------------------------------------

func checkEnvTracked(ctx context.Context, repoRoot string) []finding.Finding {
	files, err := gitLsFiles(ctx, repoRoot)
	if err != nil {
		slog.Warn("repos: git ls-files failed for env check", "repo", repoRoot, "err", err)
		markError(ctx)
		return nil
	}

	var findings []finding.Finding
	for _, relPath := range files {
		base := strings.ToLower(filepath.Base(filepath.FromSlash(relPath)))
		if !strings.HasPrefix(base, ".env") {
			continue
		}
		if envFileSafeList[base] {
			continue // .env.example, .env.sample, etc.
		}
		// File looks like a real .env: ".env", ".env.local", ".env.production", etc.
		id := repoFindingID("env_tracked", repoRoot, relPath)
		f := finding.NewFinding(id, finding.SeverityRed, "env_tracked",
			fmt.Sprintf("Environment file tracked by git: %s", relPath))
		f.GroupDetail = relPath
		f.WhyItMatters = whyEnvTracked
		f.SuggestedAction = actionEnvTracked
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			RepoPath: repoRoot,
			File:     relPath,
		}
		findings = append(findings, f)
	}
	return findings
}

// ---------------------------------------------------------------------------
// Check 4: lifecycle scripts with network calls in package.json
// ---------------------------------------------------------------------------

// packageJSON is the minimal subset of package.json we parse.
type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// lifecycleHookNames is the ordered list of npm lifecycle hooks we check.
// These run automatically without user interaction during `npm install`.
var lifecycleHookNames = []string{
	"preinstall", "install", "postinstall",
	"prepare", "prepublish",
	"prepack", "postpack",
}

func checkLifecycleNetwork(repoRoot string) []finding.Finding {
	pkgPath := filepath.Join(repoRoot, "package.json")
	content, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil // no package.json — not applicable
	}

	var pkg packageJSON
	if err := json.Unmarshal(content, &pkg); err != nil {
		slog.Warn("repos: could not parse package.json", "path", pkgPath, "err", err)
		return nil
	}

	var findings []finding.Finding
	for _, hookName := range lifecycleHookNames {
		script, ok := pkg.Scripts[hookName]
		if !ok || !lifecycleNetworkPattern.MatchString(script) {
			continue
		}
		id := repoFindingID("lifecycle_script_with_network", repoRoot, hookName)
		f := finding.NewFinding(id, finding.SeverityYellow, "lifecycle_script_with_network",
			fmt.Sprintf("Lifecycle script '%s' contains a network command", hookName))
		f.GroupDetail = hookName
		f.WhyItMatters = whyLifecycleNetwork
		f.SuggestedAction = actionLifecycleNetwork
		f.Confidence = "medium"
		f.Evidence = finding.Evidence{
			RepoPath: repoRoot,
			File:     "package.json",
			Extra:    map[string]string{"script_name": hookName, "script": script},
		}
		findings = append(findings, f)
	}
	return findings
}

// ---------------------------------------------------------------------------
// Check 5: workflow files — dangerous patterns + new/modified
// ---------------------------------------------------------------------------

func checkWorkflows(
	ctx context.Context,
	repoRoot string,
	baseline *state.RepoBaseline,
) ([]finding.Finding, []state.WorkflowFile) {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		// No .github/workflows — not applicable. Not an error.
		return nil, nil
	}

	// Build baseline lookup: relPath → sha256.
	blByPath := make(map[string]string)
	if baseline != nil {
		for _, wf := range baseline.WorkflowFiles {
			blByPath[wf.Path] = wf.SHA256
		}
	}

	var (
		findings      []finding.Finding
		workflowFiles []state.WorkflowFile
	)

	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		relPath := filepath.ToSlash(filepath.Join(".github", "workflows", name))
		absPath := filepath.Join(workflowDir, name)

		content, err := readFileSized(absPath)
		if err != nil || content == nil {
			continue
		}

		currentSHA := hash.Bytes(content)
		workflowFiles = append(workflowFiles, state.WorkflowFile{
			Path:   relPath,
			SHA256: currentSHA,
		})

		// Dangerous pattern check (always, regardless of baseline).
		for _, dp := range dangerousWorkflowPatterns {
			if !dp.pattern.Match(content) {
				continue
			}
			id := repoFindingID("workflow_dangerous_pattern", repoRoot, relPath+":"+dp.id)
			f := finding.NewFinding(id, finding.SeverityRed, "workflow_dangerous_pattern",
				fmt.Sprintf("Dangerous pattern in workflow: %s (%s)", name, dp.desc))
			f.GroupDetail = relPath + ":" + dp.id
			f.WhyItMatters = whyWorkflowDangerous
			f.SuggestedAction = actionWorkflowDangerous
			f.Confidence = "medium"
			f.Evidence = finding.Evidence{
				RepoPath: repoRoot,
				File:     relPath,
				Extra:    map[string]string{"pattern": dp.id},
			}
			findings = append(findings, f)
		}

		// New/modified check (only if baseline exists for this repo).
		if baseline == nil {
			continue
		}
		prevSHA, inBaseline := blByPath[relPath]
		switch {
		case !inBaseline:
			id := repoFindingID("workflow_new_or_modified", repoRoot, relPath)
			f := finding.NewFinding(id, finding.SeverityYellow, "workflow_new_or_modified",
				fmt.Sprintf("New workflow file: %s", name))
			f.GroupDetail = relPath
			f.WhyItMatters = whyWorkflowModified
			f.SuggestedAction = actionWorkflowModified
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RepoPath:      repoRoot,
				File:          relPath,
				CurrentSHA256: currentSHA[:16] + "…",
			}
			findings = append(findings, f)
		case prevSHA != currentSHA:
			id := repoFindingID("workflow_new_or_modified", repoRoot, relPath)
			f := finding.NewFinding(id, finding.SeverityYellow, "workflow_new_or_modified",
				fmt.Sprintf("Workflow file changed: %s", name))
			f.GroupDetail = relPath
			f.WhyItMatters = whyWorkflowModified
			f.SuggestedAction = actionWorkflowModified
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RepoPath:       repoRoot,
				File:           relPath,
				PreviousSHA256: prevSHA[:16] + "…",
				CurrentSHA256:  currentSHA[:16] + "…",
			}
			findings = append(findings, f)
		}
	}
	return findings, workflowFiles
}

// ---------------------------------------------------------------------------
// Check 6: git hooks — network calls + new/modified
// ---------------------------------------------------------------------------

func checkHooks(
	ctx context.Context,
	repoRoot string,
	baseline *state.RepoBaseline,
	allowHooks []config.AllowEntryPath,
) ([]finding.Finding, []state.HookBaseline) {
	// Resolve the effective hooks directory.
	hooksRelDir, err := gitHooksDir(ctx, repoRoot)
	if err != nil {
		slog.Warn("repos: could not resolve hooks dir", "repo", repoRoot, "err", err)
		return nil, nil
	}

	var hooksAbsDir string
	if filepath.IsAbs(hooksRelDir) {
		hooksAbsDir = hooksRelDir
	} else {
		hooksAbsDir = filepath.Join(repoRoot, hooksRelDir)
	}

	entries, err := os.ReadDir(hooksAbsDir)
	if err != nil {
		return nil, nil // no hooks dir — fine
	}

	// Build allow-set: lowercase(path) → present.
	allowSet := make(map[string]config.AllowEntryPath, len(allowHooks))
	for _, a := range allowHooks {
		allowSet[strings.ToLower(a.Path)] = a
	}

	// Build baseline lookup: hook name → sha256.
	blByName := make(map[string]string)
	if baseline != nil {
		for _, h := range baseline.Hooks {
			blByName[h.Name] = h.SHA256
		}
	}

	var (
		ffindings     []finding.Finding
		hookBaselines []state.HookBaseline
	)

	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sample") {
			continue // git's own example hooks — skip
		}
		if !standardHookNames[name] {
			continue // not a known hook name
		}

		absPath := filepath.Join(hooksAbsDir, name)
		content, err := readFileSized(absPath)
		if err != nil || content == nil {
			continue
		}

		currentSHA := hash.Bytes(content)
		hookBaselines = append(hookBaselines, state.HookBaseline{
			Name:   name,
			SHA256: currentSHA,
		})

		// Allow-list: every finding of this hook is dropped if its path or
		// name is allowed, and counted (B9).
		entry, allowed := allowSet[strings.ToLower(absPath)]
		if !allowed {
			entry, allowed = allowSet[strings.ToLower(name)]
		}
		before := len(ffindings)

		// Network call check (always, regardless of baseline).
		if hookNetworkPattern.Match(content) {
			id := repoFindingID("git_hook_with_network", repoRoot, name)
			f := finding.NewFinding(id, finding.SeverityRed, "git_hook_with_network",
				fmt.Sprintf("Git hook contains a network command: %s", name))
			f.GroupDetail = name
			f.WhyItMatters = whyHookNetwork
			f.SuggestedAction = actionHookNetwork
			f.Confidence = "medium"
			f.Evidence = finding.Evidence{
				RepoPath: repoRoot,
				File:     absPath,
			}
			ffindings = append(ffindings, f)
		}

		// New/modified check (only if baseline exists for this repo).
		prevSHA, inBaseline := blByName[name]
		switch {
		case baseline == nil:
		case !inBaseline:
			id := repoFindingID("git_hook_new_or_modified", repoRoot, name)
			f := finding.NewFinding(id, finding.SeverityYellow, "git_hook_new_or_modified",
				fmt.Sprintf("New git hook: %s", name))
			f.GroupDetail = name
			f.WhyItMatters = whyHookModified
			f.SuggestedAction = actionHookModified
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RepoPath:      repoRoot,
				File:          absPath,
				CurrentSHA256: currentSHA[:16] + "…",
			}
			ffindings = append(ffindings, f)
		case prevSHA != currentSHA:
			id := repoFindingID("git_hook_new_or_modified", repoRoot, name)
			f := finding.NewFinding(id, finding.SeverityYellow, "git_hook_new_or_modified",
				fmt.Sprintf("Git hook changed: %s", name))
			f.GroupDetail = name
			f.WhyItMatters = whyHookModified
			f.SuggestedAction = actionHookModified
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				RepoPath:       repoRoot,
				File:           absPath,
				PreviousSHA256: prevSHA[:16] + "…",
				CurrentSHA256:  currentSHA[:16] + "…",
			}
			ffindings = append(ffindings, f)
		}
		if allowed {
			for range ffindings[before:] {
				allowHit(ctx, config.AllowHit{List: config.ListGitHooks, ID: entry.Path, Note: entry.Note})
			}
			ffindings = ffindings[:before]
		}
	}
	return ffindings, hookBaselines
}

// ---------------------------------------------------------------------------
// Check 7: core.hooksPath redirected
// ---------------------------------------------------------------------------

func checkCoreHooksPath(ctx context.Context, repoRoot string, baseline *state.RepoBaseline) ([]finding.Finding, string) {
	val, err := gitConfigGet(ctx, repoRoot, "core.hooksPath")
	if err != nil || val == "" {
		return nil, ""
	}

	var findings []finding.Finding

	// Flag absolute paths — they may point to attacker-controlled locations.
	//
	// Known limitation (v0.2 TODO): relative paths such as ".malicious-hooks" inside the
	// repository root do NOT fire here unless they differ from a previously-recorded baseline.
	// This means a fresh `core.hooksPath = .malicious-hooks` on a machine with no prior
	// baseline is silently skipped. v0.2 should emit a YELLOW INFO finding for any non-default
	// core.hooksPath value (i.e. anything other than ""), including relative paths, so that
	// even a first-run machine surfaces the redirection for the operator's awareness.
	if filepath.IsAbs(val) {
		id := repoFindingID("core_hookspath_redirected", repoRoot, val)
		f := finding.NewFinding(id, finding.SeverityRed, "core_hookspath_redirected",
			"core.hooksPath redirected to an absolute path")
		f.GroupDetail = val
		f.WhyItMatters = whyCoreHooksPath
		f.SuggestedAction = actionCoreHooksPath
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			RepoPath: repoRoot,
			Extra:    map[string]string{"core_hooks_path": val},
		}
		findings = append(findings, f)
		return findings, val
	}

	// Flag if the value changed from baseline.
	if baseline != nil && baseline.CoreHooksPath != "" && baseline.CoreHooksPath != val {
		id := repoFindingID("core_hookspath_redirected", repoRoot, "changed:"+val)
		f := finding.NewFinding(id, finding.SeverityRed, "core_hookspath_redirected",
			"core.hooksPath changed since last baseline")
		f.GroupDetail = "changed:" + val
		f.WhyItMatters = whyCoreHooksPath
		f.SuggestedAction = actionCoreHooksPath
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			RepoPath:       repoRoot,
			PreviousSHA256: hash.Bytes([]byte(baseline.CoreHooksPath))[:16] + "…",
			CurrentSHA256:  hash.Bytes([]byte(val))[:16] + "…",
			Extra:          map[string]string{"core_hooks_path": val, "previous": baseline.CoreHooksPath},
		}
		findings = append(findings, f)
	}

	return findings, val
}

// ---------------------------------------------------------------------------
// Finding ID helper
// ---------------------------------------------------------------------------

// repoFindingID builds a stable finding ID for a repo check.
// Format: <category>:<sha256(repoRoot+":"+detail)[:16]>
func repoFindingID(category, repoRoot, detail string) string {
	h := hash.Bytes([]byte(repoRoot + ":" + detail))
	return category + ":" + h[:16]
}

// appendCommitted adds the committed secret of a history match, when the
// scan identifies secrets.
func appendCommitted(cs []finding.CommittedSecret, ic identifyCtx, repoRoot, commit string, m secrets.SecretMatch) []finding.CommittedSecret {
	if ic.identify == nil || m.Ref == nil {
		return cs
	}
	return append(cs, finding.CommittedSecret{
		PatternID: m.PatternID, Ref: *m.Ref, Repo: repoRoot, Presence: finding.PresenceHistoryOnly,
		File: m.File, Commit: commit, HasRemote: ic.hasRemote,
	})
}

// identifyCtx is the run's Identify and whether this repository declares a
// remote, carried by the repository's context.
type identifyCtx struct {
	identify  secrets.Identify
	hasRemote bool
}

type identifyKey struct{}

type allowHitKey struct{}

// allowHit reports one warning an allow-list entry hid (B9) to the scan's
// OnAllowHit, if any.
func allowHit(ctx context.Context, h config.AllowHit) {
	if f, ok := ctx.Value(allowHitKey{}).(func(config.AllowHit)); ok {
		f(h)
	}
}

func withIdentify(ctx context.Context, ic identifyCtx) context.Context {
	return context.WithValue(ctx, identifyKey{}, ic)
}

// identifyOf is the repository's identifyCtx; empty when the scan does not
// identify secrets.
func identifyOf(ctx context.Context) identifyCtx {
	ic, _ := ctx.Value(identifyKey{}).(identifyCtx)
	return ic
}

// hasRemote reports whether .git/config declares a remote (a remote.<name>.url
// entry), read at rest: not whether it is public or anything was pushed.
func hasRemote(ctx context.Context, repoRoot string) bool {
	out, err := runGit(ctx, repoRoot, "config", "--get-regexp", `^remote\..*\.url$`)
	return err == nil && strings.TrimSpace(out) != ""
}
