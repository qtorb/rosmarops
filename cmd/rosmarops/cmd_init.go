package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

func newInitCmd() *cli.Command {
	// design choice 2026-05-21: non-interactive init for testability and
	// security-tool transparency (user edits JSON directly). Prompting for
	// repo_roots would require terminal detection, makes the command
	// non-scriptable, and obscures the config format from the user. Plain
	// JSON with a known path is auditable; interactive prompts are not.
	return &cli.Command{
		Name: "init", Short: "cli.cmd.init.short", Long: "cli.cmd.init.long", Args: cli.AnyArgs,
		Flags: []cli.Flag{{Name: "repos-under", Usage: "cli.flag.repos_under"}},
		Run:   runInit,
	}
}

func runInit(inv cli.Invocation) error {
	reposUnder := inv.Flags["repos-under"]
	cfgPath, err := config.ResolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	// repo_roots from --repos-under when given. A folder that cannot be read
	// leaves repo_roots empty: init still succeeds.
	var repoRoots []string
	if reposUnder != "" {
		repos, err := config.ReposUnder(reposUnder)
		if err != nil {
			slog.Warn("init: --repos-under folder not read; repo_roots left empty", "err", err)
			fmt.Fprintf(os.Stderr, "warning: %v; repo_roots left empty\n", err)
		} else {
			repoRoots = repos
			fmt.Printf("Repositories found under %s: %d\n", reposUnder, len(repos))
		}
	}
	stPath, reportDir, err := initialize(cfgPath, repoRoots)
	if err != nil {
		return err
	}

	fmt.Printf("RosmarOps initialized.\n\n")
	fmt.Printf("  Config: %s\n", cfgPath)
	fmt.Printf("  State:  %s\n\n", stPath)
	fmt.Printf("Next step:\n")
	fmt.Printf("  Run 'rosmarops run' to scan this computer.\n")
	fmt.Printf("  Results are written to %s.\n", reportDir)

	return nil
}

// initialize writes the configuration at cfgPath, with repoRoots, and the
// state with the integrity references of this binary and this
// configuration, and the accepted copy (decision 2026-10-26, integridad y
// revisión semanal, J1). It never overwrites: an existing configuration or
// state is an error. It returns the state's path and the reports directory.
func initialize(cfgPath string, repoRoots []string) (stPath, reportDir string, err error) {
	stPath, err = state.StatePath()
	if err != nil {
		return "", "", fmt.Errorf("resolve state path: %w", err)
	}

	// Fail early if either file already exists — no silent overwrite.
	cfgExists := fileExists(cfgPath)
	stExists := fileExists(stPath)

	if cfgExists || stExists {
		if cfgExists {
			slog.Error("config already exists", "path", cfgPath)
			fmt.Fprintf(os.Stderr, "error: config already exists at %s\n", cfgPath)
		}
		if stExists {
			slog.Error("state already exists", "path", stPath)
			fmt.Fprintf(os.Stderr, "error: state already exists at %s\n", stPath)
		}
		fmt.Fprintln(os.Stderr, "hint: use 'rosmarops baseline' to rehash after intentional changes")
		return "", "", errors.New("init aborted: files already exist")
	}

	// Ensure ~/.rosmarops/ directory exists.
	// This is the only MkdirAll call permitted by the write-scope invariant.
	stDir := filepath.Dir(stPath)
	if err := os.MkdirAll(stDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create state directory %s: %w", stDir, err)
	}

	cfg := config.DefaultConfig()
	if repoRoots != nil {
		cfg.RepoRoots = repoRoots
	}
	if err := config.WriteConfig(cfgPath, cfg); err != nil {
		return "", "", fmt.Errorf("write config: %w", err)
	}
	slog.Info("config created", "path", cfgPath)

	reportDir = cfg.ReportDirectory
	if reportDir == "" {
		reportDir, err = report.DefaultReportDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve report dir: %w", err)
		}
	}

	st, err := state.DefaultState(version)
	if err != nil {
		return "", "", fmt.Errorf("create state: %w", err)
	}
	self, err := hash.Self()
	if err != nil {
		return "", "", fmt.Errorf("hash binary: %w", err)
	}
	cfgRaw, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", "", fmt.Errorf("read config: %w", err)
	}
	st.Integrity.VigilBinarySHA256, st.Integrity.ConfigSHA256 = self, hash.Bytes(cfgRaw)
	if err := writeAcceptedCopy(reportDir, cfgRaw); err != nil {
		return "", "", err
	}
	if err := state.SaveState(stPath, st); err != nil {
		return "", "", fmt.Errorf("write state: %w", err)
	}
	slog.Info("state created", "path", stPath)
	return stPath, reportDir, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
