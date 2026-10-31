package report

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/results"
)

// ResultsFileName is the closed package's name (AP-17 §2):
// rosmarops-results-<YYYY-MM-DD>-<host_id[:8]>.locked.json. The date is the
// run's local date, as the report's own file names are.
func ResultsFileName(now time.Time, hostID string) string {
	h := hostID
	if len(h) > 8 {
		h = h[:8]
	}
	return fmt.Sprintf("rosmarops-results-%s-%s.locked.json", now.Format("2006-01-02"), h)
}

// WriteResultsPackage builds the space-view v1 content, validates it, seals it
// as a rosmarops-results v1 package with the kit key and writes it to dir with
// mode 0600 (BV-119, AP-17 §2). dir is the reports directory (invariant 2: no
// new write target). It is only called when a kit is present on this computer
// and a graph was built; without a kit nothing is written and the report is
// unchanged.
//
// kitKey is the kit's AES-256 key; kitID and keyID identify it in the clear
// header. host_id comes from the graph. The caller holds the kit material;
// this package never reads the kit itself.
func WriteResultsPackage(v V2, g *graph.Graph, home, version string, kitKey []byte, kitID, keyID string, now time.Time, dir string) (string, error) {
	if g == nil {
		return "", nil // no graph: no content to seal
	}
	sv, err := BuildSpaceView(v, g, home, version)
	if err != nil {
		return "", fmt.Errorf("build space view: %w", err)
	}
	content, err := RenderSpaceViewJSON(sv)
	if err != nil {
		return "", err
	}
	header := results.Header{
		KitID: kitID, KeyID: keyID, HostID: g.Provenance.HostID,
		CreatedAt: now.UTC().Format(time.RFC3339), RosmaropsVersion: version,
	}
	pkg, err := results.Seal(kitKey, header, content)
	if err != nil {
		return "", fmt.Errorf("seal results package: %w", err)
	}
	raw, err := results.Marshal(pkg)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, ResultsFileName(now, g.Provenance.HostID))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write results package: %w", err)
	}
	return path, nil
}
