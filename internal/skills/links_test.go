package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/skills"
)

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// BV-99b, item 1: the typical `npx skills add` (vercel-labs/skills): the
// canonical copy in <repo>\.agents\skills\demo, with scripts\run.ps1, and a
// link to it from <repo>\.claude\skills\demo. Claude Code gets the skill by
// its canonical paths, Cursor (which loads both folders) once, and nothing
// goes to coverage. Relative and absolute targets alike.
func TestDiscover_NpxSkillsAddLink(t *testing.T) {
	for _, relative := range []bool{true, false} {
		tmp := t.TempDir()
		repo := filepath.Join(tmp, "repo")
		canon := filepath.Join(repo, ".agents", "skills", "demo")
		write(t, filepath.Join(canon, "SKILL.md"), 1)
		write(t, filepath.Join(canon, "scripts", "run.ps1"), 1)
		target := canon
		if relative {
			target = filepath.Join("..", "..", ".agents", "skills", "demo")
		}
		symlink(t, target, filepath.Join(repo, ".claude", "skills", "demo"))

		scan := skills.Discover(context.Background(), "", envOf(nil), []string{repo})
		equal(t, "instructions", listed(tmp, scan.Instructions), []string{
			"antigravity repo/.agents/skills/demo/SKILL.md",
			"claude-code repo/.agents/skills/demo/SKILL.md",
			"cline repo/.agents/skills/demo/SKILL.md",
			"codex repo/.agents/skills/demo/SKILL.md",
			"copilot repo/.agents/skills/demo/SKILL.md",
			"cursor repo/.agents/skills/demo/SKILL.md",
		})
		equal(t, "executables", listed(tmp, scan.Executables), []string{
			"antigravity repo/.agents/skills/demo/scripts/run.ps1",
			"claude-code repo/.agents/skills/demo/scripts/run.ps1",
			"cline repo/.agents/skills/demo/scripts/run.ps1",
			"codex repo/.agents/skills/demo/scripts/run.ps1",
			"copilot repo/.agents/skills/demo/scripts/run.ps1",
			"cursor repo/.agents/skills/demo/scripts/run.ps1",
		})
		equal(t, "failed", failures(tmp, scan.Failed), nil)
	}
}

// BV-99b, item 1: user folders. A link in ~\.claude\skills to a skill in
// ~\.agents\skills counts; a link in a repository to a user skill counts too.
func TestDiscover_LinkToUserSkill(t *testing.T) {
	tmp := t.TempDir()
	home, repo := filepath.Join(tmp, "home"), filepath.Join(tmp, "repo")
	canon := filepath.Join(home, ".agents", "skills", "u")
	write(t, filepath.Join(canon, "SKILL.md"), 1)
	symlink(t, canon, filepath.Join(home, ".claude", "skills", "u"))
	symlink(t, canon, filepath.Join(repo, ".cursor", "skills", "u"))
	scan := skills.Discover(context.Background(), home, envOf(nil), []string{repo})
	equal(t, "instructions", listed(tmp, scan.Instructions), []string{
		"claude-code home/.agents/skills/u/SKILL.md",
		"codex home/.agents/skills/u/SKILL.md",
		"copilot home/.agents/skills/u/SKILL.md",
		"cursor home/.agents/skills/u/SKILL.md",
	})
	equal(t, "failed", failures(tmp, scan.Failed), nil)
}

// BV-99b, item 1: any other link stays unread: to a folder outside the
// skills folders, to a skill in another repository's .agents\skills, to a
// skill in another folder of its own repository, to a link (no chains), and
// to a skill the pass did not list.
func TestDiscover_LinksElsewhereNotFollowed(t *testing.T) {
	tmp := t.TempDir()
	repo, other := filepath.Join(tmp, "repo"), filepath.Join(tmp, "other")
	write(t, filepath.Join(tmp, "outside", "x", "SKILL.md"), 1)
	write(t, filepath.Join(other, ".agents", "skills", "o", "SKILL.md"), 1)
	write(t, filepath.Join(repo, ".cursor", "skills", "c", "SKILL.md"), 1)
	write(t, filepath.Join(repo, ".agents", "skills", "real", "SKILL.md"), 1)
	symlink(t, filepath.Join(repo, ".agents", "skills", "real"), filepath.Join(repo, ".agents", "skills", "hop"))
	claude := filepath.Join(repo, ".claude", "skills")
	symlink(t, filepath.Join(tmp, "outside", "x"), filepath.Join(claude, "outside"))
	symlink(t, filepath.Join(other, ".agents", "skills", "o"), filepath.Join(claude, "other-repo"))
	symlink(t, filepath.Join(repo, ".cursor", "skills", "c"), filepath.Join(claude, "cursor-folder"))
	symlink(t, filepath.Join(repo, ".agents", "skills", "hop"), filepath.Join(claude, "chain"))
	symlink(t, filepath.Join(repo, ".agents", "skills", "absent"), filepath.Join(claude, "dangling"))

	scan := skills.Discover(context.Background(), "", envOf(nil), []string{repo, other})
	for _, f := range scan.Instructions {
		if f.Product == "claude-code" {
			t.Errorf("Claude Code got %s through a link", f.Path)
		}
	}
	var got []string
	for _, f := range failures(tmp, scan.Failed) {
		if strings.HasPrefix(f, "claude-code ") {
			got = append(got, f)
		}
	}
	reason := " " + graph.ReasonSourceUnsupported
	equal(t, "Claude Code's failures", got, []string{
		"claude-code repo/.claude/skills/chain" + reason,
		"claude-code repo/.claude/skills/cursor-folder" + reason,
		"claude-code repo/.claude/skills/dangling" + reason,
		"claude-code repo/.claude/skills/other-repo" + reason,
		"claude-code repo/.claude/skills/outside" + reason,
	})
}

// BV-99b, item 2: Hermes Agent gets <repo>\.hermes\skills and
// <repo>\.agents\skills only when the root is in
// skills.trusted_project_dirs and project_discovery is not false. Absent
// or unreadable, they are not attributed and go to its coverage; its user
// skills do not change.
func TestDiscover_HermesTrustedProjectDirs(t *testing.T) {
	for _, c := range []struct {
		name, config string
		want         []string // Hermes's instructions
		failed       string   // coverage code of Hermes's project folders
	}{
		{"trusted list", "skills:\n  trusted_project_dirs:\n    - REPO\n", []string{"hermes-home/skills/u/SKILL.md", "repo/.agents/skills/a/SKILL.md", "repo/.hermes/skills/h/SKILL.md"}, ""},
		{"trusted scalar, trailing slash", "skills:\n  trusted_project_dirs: 'REPO/'\n", []string{"hermes-home/skills/u/SKILL.md", "repo/.agents/skills/a/SKILL.md", "repo/.hermes/skills/h/SKILL.md"}, ""},
		{"trusted through a variable", "skills:\n  trusted_project_dirs: ['${PROJ}/repo']\n", []string{"hermes-home/skills/u/SKILL.md", "repo/.agents/skills/a/SKILL.md", "repo/.hermes/skills/h/SKILL.md"}, ""},
		{"another root", "skills:\n  trusted_project_dirs: ['REPO-other']\n", []string{"hermes-home/skills/u/SKILL.md"}, ""},
		{"empty list", "skills:\n  trusted_project_dirs: []\n", []string{"hermes-home/skills/u/SKILL.md"}, ""},
		{"discovery off", "skills:\n  project_discovery: false\n  trusted_project_dirs: ['REPO']\n", []string{"hermes-home/skills/u/SKILL.md"}, ""},
		{"setting absent", "approvals:\n  mode: manual\n", []string{"hermes-home/skills/u/SKILL.md"}, graph.ReasonSourceUnsupported},
		{"outside the YAML subset", "skills: &a\n  trusted_project_dirs: ['REPO']\n", []string{"hermes-home/skills/u/SKILL.md"}, graph.ReasonParseError},
	} {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			home, repo, hermes := filepath.Join(tmp, "home"), filepath.Join(tmp, "repo"), filepath.Join(tmp, "hermes-home")
			write(t, filepath.Join(hermes, "skills", "u", "SKILL.md"), 1)
			write(t, filepath.Join(repo, ".hermes", "skills", "h", "SKILL.md"), 1)
			write(t, filepath.Join(repo, ".agents", "skills", "a", "SKILL.md"), 1)
			cfg := []byte(strings.ReplaceAll(c.config, "REPO", filepath.ToSlash(repo)))
			if err := os.WriteFile(filepath.Join(hermes, "config.yaml"), cfg, 0o600); err != nil {
				t.Fatal(err)
			}
			scan := skills.Discover(context.Background(), home, envOf(map[string]string{"HERMES_HOME": hermes, "PROJ": tmp}), []string{repo})
			var got []string
			for _, f := range scan.Instructions {
				if f.Product == "hermes" {
					rel, _ := filepath.Rel(tmp, f.Path)
					got = append(got, filepath.ToSlash(rel))
				}
			}
			equal(t, "Hermes's skills", got, c.want)
			var want []string
			if c.failed != "" {
				want = []string{"hermes repo/.agents/skills " + c.failed, "hermes repo/.hermes/skills " + c.failed}
			}
			equal(t, "failed", failures(tmp, scan.Failed), want)
		})
	}
}
