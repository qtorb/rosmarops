package repos

// The tracked-file and history checks as they were before BV-31: one read
// of every tracked file from disk, and one `git show` per commit. Kept in
// tests only, as the oracle the cached, single-process, pooled scan must
// equal (BV-31).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

func legacyCheckTrackedSecrets(ctx context.Context, repoRoot string, patterns []secrets.Pattern) []finding.Finding {
	files, err := gitLsFiles(ctx, repoRoot)
	if err != nil {
		slog.Warn("repos: git ls-files failed", "repo", repoRoot, "err", err)
		markError(ctx)
		return nil
	}

	var findings []finding.Finding
	for _, relPath := range files {
		if ctx.Err() != nil {
			break
		}
		if isExcludedDir(relPath) {
			continue
		}

		absPath := repoRelPath(repoRoot, relPath)
		content, err := readFileSized(absPath)
		if err != nil {
			slog.Debug("repos: could not read tracked file", "path", absPath, "err", err)
			continue
		}
		if content == nil {
			continue // oversized
		}

		matches := secrets.Match(content, patterns)
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
			findings = append(findings, f)
		}
	}
	return findings
}

func legacyCheckHistorySecrets(
	ctx context.Context,
	repoRoot string,
	patterns []secrets.Pattern,
	allowHistory []config.AllowEntrySecretHistory,
) []finding.Finding {
	since := time.Now().UTC().AddDate(0, -HistoryMaxAgeMonths, 0)
	hashes, err := gitLogHashes(ctx, repoRoot, HistoryMaxCommits, since)
	if err != nil {
		slog.Warn("repos: git log failed for history scan", "repo", repoRoot, "err", err)
		markError(ctx)
		return nil
	}
	trace := traceOf(ctx)
	trace.historyListed = len(hashes)

	// Build allow-set: lowercase(patternType+":"+commitSHA) → present.
	// commitSHA in the allow-list may be abbreviated (7–40 chars).
	allowSet := make(map[string]bool, len(allowHistory))
	for _, a := range allowHistory {
		key := strings.ToLower(a.PatternType) + ":" + strings.ToLower(a.CommitSHA)
		allowSet[key] = true
	}

	type seenKey struct{ patternID, commitSHA string }
	seen := make(map[seenKey]bool)
	var findings []finding.Finding

	for _, h := range hashes {
		if ctx.Err() != nil {
			break
		}

		trace.historyScanned++
		diff, err := gitShowDiff(ctx, repoRoot, h)
		if err != nil {
			slog.Debug("repos: git show diff failed", "commit", safeShort(h), "err", err)
			markError(ctx)
			continue
		}

		added := extractAddedLines(diff)
		if len(added) == 0 {
			continue
		}

		matches := secrets.Match(added, patterns)
		for _, m := range matches {
			sk := seenKey{m.PatternID, h}
			if seen[sk] {
				continue
			}
			seen[sk] = true

			// Allow-list: check both full SHA and 7-char prefix.
			if allowSet[strings.ToLower(m.PatternID)+":"+strings.ToLower(h)] ||
				(len(h) >= 7 && allowSet[strings.ToLower(m.PatternID)+":"+strings.ToLower(h[:7])]) {
				continue
			}

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
			findings = append(findings, f)
		}
	}
	return findings
}
