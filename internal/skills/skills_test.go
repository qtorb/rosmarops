package skills_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/skills"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func envOf(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

// listed renders entries as "product path", path relative to base.
func listed(base string, files []platform.InstructionFile) []string {
	var out []string
	for _, f := range files {
		rel, _ := filepath.Rel(base, f.Path)
		if !f.Shared {
			rel += " (not shared)"
		}
		out = append(out, f.Product+" "+filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

func failures(base string, fails []platform.InstructionFailure) []string {
	var out []string
	for _, f := range fails {
		rel, _ := filepath.Rel(base, f.Path)
		code, _, _ := strings.Cut(f.Reason, ":")
		out = append(out, f.Product+" "+filepath.ToSlash(rel)+" "+code)
	}
	sort.Strings(out)
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got  %q\n want %q", what, got, want)
	}
}

// K1, item 1: a fake home and a fake repository with skills in several
// folders, among them a .agents\skills that two agents (and more) load:
// each folder is attributed to every agent that loads it, each SKILL.md
// as instructions and each file under scripts\ as an executable, all
// Shared.
func TestDiscover_UserAndRepoFolders(t *testing.T) {
	tmp := t.TempDir()
	home, repo := filepath.Join(tmp, "home"), filepath.Join(tmp, "repo")
	hermes := filepath.Join(tmp, "hermes-home")
	for _, p := range []string{
		// user
		"home/.claude/skills/review/SKILL.md",
		"home/.claude/skills/review/scripts/check.ps1",
		"home/.agents/skills/shared/SKILL.md",
		"home/.agents/skills/shared/scripts/lib/run.py",
		"home/.gemini/antigravity/skills/ag/SKILL.md",
		"home/.cline/skills/cl/SKILL.md",
		"hermes-home/skills/hm/SKILL.md",
		// repository
		"repo/.agents/skills/deploy/SKILL.md",
		"repo/.agents/skills/deploy/scripts/deploy.sh",
		"repo/.claude/skills/fmt/SKILL.md",
		"repo/.cursor/skills/cu/SKILL.md",
		"repo/.github/skills/gh/SKILL.md",
		"repo/.agent/skills/ag2/SKILL.md",
		"repo/.hermes/skills/h2/SKILL.md",
		"repo/.clinerules/skills/c2/SKILL.md",
		// not skills
		"repo/.agents/skills/notes.md",                // a file, not a skill folder
		"repo/.agents/skills/empty/README.md",         // no SKILL.md
		"repo/.agents/skills/node_modules/x/SKILL.md", // forced exclusion
		"repo/.agents/skills/deploy/scripts/node_modules/dep.js",
	} {
		write(t, filepath.Join(tmp, filepath.FromSlash(p)), 1)
	}
	env := envOf(map[string]string{"HERMES_HOME": hermes, "APPDATA": filepath.Join(home, "AppData", "Roaming")})
	scan := skills.Discover(context.Background(), home, env, []string{repo})

	equal(t, "instructions", listed(tmp, scan.Instructions), []string{
		"antigravity home/.gemini/antigravity/skills/ag/SKILL.md",
		"antigravity repo/.agent/skills/ag2/SKILL.md",
		"antigravity repo/.agents/skills/deploy/SKILL.md",
		"claude-code home/.claude/skills/review/SKILL.md",
		"claude-code repo/.claude/skills/fmt/SKILL.md",
		"cline home/.cline/skills/cl/SKILL.md",
		"cline repo/.agents/skills/deploy/SKILL.md",
		"cline repo/.claude/skills/fmt/SKILL.md",
		"cline repo/.clinerules/skills/c2/SKILL.md",
		"codex home/.agents/skills/shared/SKILL.md",
		"codex repo/.agents/skills/deploy/SKILL.md",
		"copilot home/.agents/skills/shared/SKILL.md",
		"copilot repo/.agents/skills/deploy/SKILL.md",
		"copilot repo/.claude/skills/fmt/SKILL.md",
		"copilot repo/.github/skills/gh/SKILL.md",
		"cursor home/.agents/skills/shared/SKILL.md",
		"cursor repo/.agents/skills/deploy/SKILL.md",
		"cursor repo/.claude/skills/fmt/SKILL.md",
		"cursor repo/.cursor/skills/cu/SKILL.md",
		"hermes hermes-home/skills/hm/SKILL.md",
		"hermes repo/.agents/skills/deploy/SKILL.md",
		"hermes repo/.hermes/skills/h2/SKILL.md",
	})
	equal(t, "executables", listed(tmp, scan.Executables), []string{
		"antigravity repo/.agents/skills/deploy/scripts/deploy.sh",
		"claude-code home/.claude/skills/review/scripts/check.ps1",
		"cline repo/.agents/skills/deploy/scripts/deploy.sh",
		"codex home/.agents/skills/shared/scripts/lib/run.py",
		"codex repo/.agents/skills/deploy/scripts/deploy.sh",
		"copilot home/.agents/skills/shared/scripts/lib/run.py",
		"copilot repo/.agents/skills/deploy/scripts/deploy.sh",
		"cursor home/.agents/skills/shared/scripts/lib/run.py",
		"cursor repo/.agents/skills/deploy/scripts/deploy.sh",
		"hermes repo/.agents/skills/deploy/scripts/deploy.sh",
	})
	equal(t, "failed", failures(tmp, scan.Failed), nil)

	// Each agent is identified as its ai_client is.
	dirs := map[string]string{}
	for _, f := range scan.Instructions {
		dirs[f.Product] = f.ConfigDir
	}
	for product, want := range map[string]string{
		"claude-code": filepath.Join(home, ".claude"),
		"cursor":      filepath.Join(home, ".cursor"),
		"codex":       filepath.Join(home, ".codex"),
		"antigravity": filepath.Join(home, ".gemini"),
		"hermes":      hermes,
		"cline":       filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings"),
		"copilot":     filepath.Join(home, ".copilot"),
	} {
		if dirs[product] != want {
			t.Errorf("%s config dir = %q, want %q", product, dirs[product], want)
		}
	}
}

// CLAUDE_CONFIG_DIR moves Claude Code's skills folder with its ai_client.
func TestDiscover_ClaudeConfigDir(t *testing.T) {
	tmp := t.TempDir()
	home, conf := filepath.Join(tmp, "home"), filepath.Join(tmp, "claude-conf")
	write(t, filepath.Join(home, ".claude", "skills", "old", "SKILL.md"), 1)
	write(t, filepath.Join(conf, "skills", "new", "SKILL.md"), 1)
	scan := skills.Discover(context.Background(), home, envOf(map[string]string{"CLAUDE_CONFIG_DIR": conf}), nil)
	equal(t, "instructions", listed(tmp, scan.Instructions), []string{"claude-code claude-conf/skills/new/SKILL.md"})
	if len(scan.Instructions) == 1 && scan.Instructions[0].ConfigDir != conf {
		t.Errorf("config dir = %q, want %q", scan.Instructions[0].ConfigDir, conf)
	}
}

// K1, item 4: at most MaxSkillsPerFolder skills per folder and
// MaxScriptFiles files per scripts folder; the folder over the limit goes
// to coverage.
func TestDiscover_CountLimits(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	dir := filepath.Join(repo, ".cursor", "skills")
	for i := 0; i <= skills.MaxSkillsPerFolder; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("s%03d", i), "SKILL.md"), 1)
	}
	scripts := filepath.Join(repo, ".hermes", "skills", "big", "scripts")
	write(t, filepath.Join(repo, ".hermes", "skills", "big", "SKILL.md"), 1)
	for i := 0; i <= skills.MaxScriptFiles; i++ {
		write(t, filepath.Join(scripts, fmt.Sprintf("f%03d.sh", i)), 1)
	}
	scan := skills.Discover(context.Background(), "", envOf(nil), []string{repo})
	var cursor, hermes int
	for _, f := range scan.Instructions {
		if f.Product == "cursor" {
			cursor++
		}
	}
	for _, f := range scan.Executables {
		if f.Product == "hermes" {
			hermes++
		}
	}
	if cursor != skills.MaxSkillsPerFolder || hermes != skills.MaxScriptFiles {
		t.Errorf("listed %d skills and %d scripts, want %d and %d", cursor, hermes, skills.MaxSkillsPerFolder, skills.MaxScriptFiles)
	}
	equal(t, "failed", failures(tmp, scan.Failed), []string{
		"cursor repo/.cursor/skills " + graph.ReasonFileCountLimitExceeded,
		"hermes repo/.hermes/skills/big/scripts " + graph.ReasonFileCountLimitExceeded,
	})
}

// K1, item 4: a file over the size limit (invariant 6) is not listed and
// goes to coverage.
func TestDiscover_SizeLimit(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	write(t, filepath.Join(repo, ".cursor", "skills", "s", "SKILL.md"), 1)
	write(t, filepath.Join(repo, ".cursor", "skills", "s", "scripts", "big.bin"), repos.MaxFileBytes+1)
	scan := skills.Discover(context.Background(), "", envOf(nil), []string{repo})
	if len(scan.Executables) != 0 {
		t.Errorf("executables = %v, want none", scan.Executables)
	}
	equal(t, "failed", failures(tmp, scan.Failed), []string{"cursor repo/.cursor/skills/s/scripts/big.bin " + graph.ReasonSizeLimitExceeded})
}

// K1, item 4: links are never followed. A skill folder, a scripts folder or
// a skills folder that is a link goes to coverage; a file that is a link is
// listed by its own path, not followed.
func TestDiscover_LinksNotFollowed(t *testing.T) {
	tmp := t.TempDir()
	repo, outside := filepath.Join(tmp, "repo"), filepath.Join(tmp, "outside")
	write(t, filepath.Join(outside, "skill", "SKILL.md"), 1)
	write(t, filepath.Join(outside, "skill", "scripts", "x.sh"), 1)
	write(t, filepath.Join(outside, "tool.sh"), 1)
	cursor := filepath.Join(repo, ".cursor", "skills")
	write(t, filepath.Join(cursor, "own", "SKILL.md"), 1)
	links := map[string]string{
		filepath.Join(cursor, "linked"):                                  filepath.Join(outside, "skill"),
		filepath.Join(cursor, "own", "scripts"):                          filepath.Join(outside, "skill", "scripts"),
		filepath.Join(repo, ".github", "skills"):                         outside,
		filepath.Join(repo, ".hermes", "skills", "h", "scripts", "t.sh"): filepath.Join(outside, "tool.sh"),
	}
	write(t, filepath.Join(repo, ".hermes", "skills", "h", "SKILL.md"), 1)
	for link, target := range links {
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	scan := skills.Discover(context.Background(), "", envOf(nil), []string{repo})
	equal(t, "instructions", listed(tmp, scan.Instructions), []string{
		"cursor repo/.cursor/skills/own/SKILL.md",
		"hermes repo/.hermes/skills/h/SKILL.md",
	})
	equal(t, "executables", listed(tmp, scan.Executables), []string{"hermes repo/.hermes/skills/h/scripts/t.sh"})
	equal(t, "failed", failures(tmp, scan.Failed), []string{
		"copilot repo/.github/skills " + graph.ReasonSourceUnsupported,
		"cursor repo/.cursor/skills/linked " + graph.ReasonSourceUnsupported,
		"cursor repo/.cursor/skills/own/scripts " + graph.ReasonSourceUnsupported,
	})
}

// Invariant 6: a repository whose deadline has passed (here, a run already
// ended) goes to coverage and nothing of it is listed.
func TestDiscover_Deadline(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	write(t, filepath.Join(repo, ".cursor", "skills", "s", "SKILL.md"), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scan := skills.Discover(ctx, "", envOf(nil), []string{repo})
	if len(scan.Instructions) != 0 {
		t.Errorf("instructions = %v, want none", scan.Instructions)
	}
	equal(t, "failed", failures(tmp, scan.Failed), []string{"cursor repo/.cursor/skills " + graph.ReasonReadError})
}

// No home: no user folder; nothing on disk: nothing listed.
func TestDiscover_Nothing(t *testing.T) {
	scan := skills.Discover(context.Background(), "", envOf(nil), []string{filepath.Join(t.TempDir(), "absent")})
	if len(scan.Instructions)+len(scan.Executables)+len(scan.Failed) != 0 {
		t.Errorf("scan = %+v, want empty", scan)
	}
}
