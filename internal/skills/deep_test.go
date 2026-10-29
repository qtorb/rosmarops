package skills_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/skills"
)

// BV-101b, decision 2a/2d: Hermes ships ~306 skills as skills\<category>\<skill>\
// SKILL.md. RosmarOps must discover SKILL.md at any depth (cap 8) in Hermes
// skills folders, prune Hermes's own excluded dirs, list each skill's scripts\,
// and leave a skill nested at depth 9 out. Other agents stay at one level.
func TestDiscover_HermesDeepSkills(t *testing.T) {
	home := t.TempDir()
	hermes := filepath.Join(home, "hermes")
	skillsDir := filepath.Join(hermes, "skills")

	// 306 category skills (3 categories × 102), every 20th with a scripts\ file.
	const categories, perCategory = 3, 102
	want := categories * perCategory // 306
	withScripts := 0
	for c := 0; c < categories; c++ {
		for s := 0; s < perCategory; s++ {
			dir := filepath.Join(skillsDir, fmt.Sprintf("cat%d", c), fmt.Sprintf("skill%03d", s))
			write(t, filepath.Join(dir, "SKILL.md"), 10)
			if (c*perCategory+s)%20 == 0 {
				write(t, filepath.Join(dir, "scripts", "run.sh"), 10)
				withScripts++
			}
		}
	}

	// A skill whose SKILL.md sits at depth 8 (allowed) and one at depth 9 (out).
	// Depth is counted from the skills root (depth 0); its children are depth 1,
	// so the SKILL.md's folder is at depth = number of path components below the
	// root.
	depth8 := filepath.Join(skillsDir, "a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8") // depth 8
	write(t, filepath.Join(depth8, "SKILL.md"), 10)
	depth9 := filepath.Join(skillsDir, "b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9") // depth 9
	write(t, filepath.Join(depth9, "SKILL.md"), 10)

	// An excluded dir (Hermes's EXCLUDED_SKILL_DIRS) with a skill inside: skipped.
	write(t, filepath.Join(skillsDir, ".archive", "old", "SKILL.md"), 10)
	// node_modules too (also a forced exclusion).
	write(t, filepath.Join(skillsDir, "node_modules", "pkg", "SKILL.md"), 10)
	// A support dir sitting inside a skill is not re-scanned for sub-skills.
	write(t, filepath.Join(skillsDir, "cat0", "skill000", "references", "nested", "SKILL.md"), 10)

	env := envOf(map[string]string{"HERMES_HOME": hermes})
	scan := skills.Discover(context.Background(), home, env, nil)

	// Count the SKILL.md RosmarOps attributes to Hermes.
	hermesMD := map[string]bool{}
	hermesScripts := 0
	for _, f := range scan.Instructions {
		if f.Product == "hermes" && strings.HasSuffix(f.Path, "SKILL.md") {
			hermesMD[f.Path] = true
		}
	}
	for _, f := range scan.Executables {
		if f.Product == "hermes" {
			hermesScripts++
		}
	}

	wantTotal := want + 1 // 306 category skills + the depth-8 one
	if len(hermesMD) != wantTotal {
		t.Errorf("Hermes SKILL.md discovered = %d, want %d", len(hermesMD), wantTotal)
	}
	if hermesMD[filepath.Join(depth8, "SKILL.md")] != true {
		t.Errorf("the depth-8 skill should be discovered")
	}
	if hermesMD[filepath.Join(depth9, "SKILL.md")] {
		t.Errorf("the depth-9 skill must be left out (cap %d)", 8)
	}
	for _, excluded := range []string{
		filepath.Join(skillsDir, ".archive", "old", "SKILL.md"),
		filepath.Join(skillsDir, "node_modules", "pkg", "SKILL.md"),
		filepath.Join(skillsDir, "cat0", "skill000", "references", "nested", "SKILL.md"),
	} {
		if hermesMD[excluded] {
			t.Errorf("a SKILL.md under an excluded/support dir must be skipped: %s", excluded)
		}
	}
	if hermesScripts != withScripts {
		t.Errorf("Hermes scripts files = %d, want %d", hermesScripts, withScripts)
	}
}

// Other agents still see only one level (decision 2b): a nested SKILL.md in a
// Claude Code skills folder is not discovered.
func TestDiscover_OtherAgentsStayOneLevel(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude", "skills", "flat", "SKILL.md"), 10)
	write(t, filepath.Join(home, ".claude", "skills", "cat", "nested", "SKILL.md"), 10)

	scan := skills.Discover(context.Background(), home, envOf(nil), nil)
	var claude []string
	for _, f := range scan.Instructions {
		if f.Product == "claude-code" {
			rel, _ := filepath.Rel(home, f.Path)
			claude = append(claude, filepath.ToSlash(rel))
		}
	}
	equal(t, "claude one level", claude, []string{".claude/skills/flat/SKILL.md"})
}
