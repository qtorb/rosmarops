package repos

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-32b: a repository's color configuration must not hide a secret from
// the history scan (colored lines no longer start with "+").
func TestCheckHistorySecrets_RepoColorConfigNeutralised(t *testing.T) {
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	writeT(t, filepath.Join(r, "k.txt"), "key = "+token(1)+"\n")
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "add")
	gitT(t, r, "config", "color.ui", "always")
	gitT(t, r, "config", "color.diff", "always")

	f := checkHistorySecrets(context.Background(), r, secrets.BuiltinPatterns(), nil)
	if len(f) != 1 || f[0].Evidence.RedactedPreview != secrets.Redact(token(1)) {
		t.Fatalf("history findings with color.ui=always: %+v", f)
	}
}
