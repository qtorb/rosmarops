package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// BV-230: our own checks must not add to the public download counter of the
// release. release.yml, and every repository script it runs, download no
// release asset but SHA256SUMS.txt; the jobs after the release take the
// binary from this run's release job (an Actions artifact).

var (
	// ghReleaseDownload is one `gh release download` command.
	ghReleaseDownload = regexp.MustCompile(`gh\s+release\s+download\b[^\n]*`)
	// downloadPattern is one --pattern (or -p) of it.
	downloadPattern = regexp.MustCompile(`(?:--pattern|-p)[ =]+("[^"]*"|'[^']*'|\S+)`)
	// assetFetch is any other way to fetch a release asset's bytes.
	assetFetch = regexp.MustCompile(`(?i)releases/download/|browser_download_url|releases/assets/|application/octet-stream`)
	// repoScript is a repository script a workflow runs.
	repoScript = regexp.MustCompile(`\./((?:\.github/scripts|scripts)/[A-Za-z0-9_./-]+\.(?:ps1|sh))`)
)

// releaseAssetDownloads lists what text downloads from a release other than
// SHA256SUMS.txt.
func releaseAssetDownloads(text string) []string {
	var bad []string
	for _, cmd := range ghReleaseDownload.FindAllString(text, -1) {
		pats := downloadPattern.FindAllStringSubmatch(cmd, -1)
		if len(pats) == 0 {
			bad = append(bad, cmd+" (no pattern: every asset)")
		}
		for _, p := range pats {
			if strings.Trim(p[1], `"'`) != "SHA256SUMS.txt" {
				bad = append(bad, cmd)
			}
		}
	}
	return append(bad, assetFetch.FindAllString(text, -1)...)
}

func TestReleaseDownloadsOnlyChecksums(t *testing.T) {
	root := filepath.Join("..", "..")
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"release.yml": string(wf)}
	for _, m := range repoScript.FindAllStringSubmatch(string(wf), -1) {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m[1])))
		if err != nil {
			t.Fatalf("release.yml runs %s: %v", m[1], err)
		}
		files[m[1]] = string(raw)
	}
	if _, ok := files[".github/scripts/check-release-exe.ps1"]; !ok {
		t.Error("release.yml no longer checks the post-release rosmarops.exe with check-release-exe.ps1")
	}
	for name, text := range files {
		for _, b := range releaseAssetDownloads(text) {
			t.Errorf("%s downloads a release asset other than SHA256SUMS.txt: %s", name, b)
		}
	}
	// The post-release jobs take rosmarops.exe from this run's artifact.
	if n := strings.Count(string(wf), "name: rosmarops-exe-release"); n < 3 {
		t.Errorf("rosmarops-exe-release appears %d times; want the upload and the two downloads (sample-report, e2e-realistic)", n)
	}
}

// TestReleaseDownloadsCheckCatches: the check sees what the old jobs did.
func TestReleaseDownloadsCheckCatches(t *testing.T) {
	for _, bad := range []string{
		`gh release download "$env:TAG" --repo "$env:GITHUB_REPOSITORY" --pattern rosmarops.exe --pattern SHA256SUMS.txt --dir dist`,
		`gh release download v1 --dir dist`,
		`gh release download v1 -p 'rosmarops-*'`,
		`curl -LO https://github.com/qtorb/rosmarops/releases/download/v1/rosmarops.exe`,
		`gh api repos/o/r/releases/assets/1 -H "Accept: application/octet-stream"`,
	} {
		if len(releaseAssetDownloads(bad)) == 0 {
			t.Errorf("not caught: %s", bad)
		}
	}
	if got := releaseAssetDownloads(`gh release download $Tag --repo $Repo --pattern SHA256SUMS.txt --dir $sums`); len(got) != 0 {
		t.Errorf("SHA256SUMS.txt alone flagged: %v", got)
	}
}
