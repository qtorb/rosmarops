package windows

import (
	"context"
	"fmt"
	"os"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/skills"
)

// ListSkills implements platform.Platform (decision 2026-10-28, K1): the
// discovery is internal/skills, on this machine's home and environment.
func (w *WindowsPlatform) ListSkills(ctx context.Context, repoRoots []string) (platform.SkillScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.SkillScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return skills.Discover(ctx, home, nil, repoRoots), nil
}
