package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	windowsadapter "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/globals"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/scan/shellprofile"
	"github.com/qtorb/rosmarops/internal/state"
)

func newBaselineCmd() *cli.Command {
	return &cli.Command{
		Name: "baseline", Short: "cli.cmd.baseline.short", Long: "cli.cmd.baseline.long", Args: cli.AnyArgs,
		Run: runBaseline,
	}
}

func runBaseline(cli.Invocation) error {
	ctx := context.Background()

	// Load state — must exist (user should run rosmarops init first).
	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return fmt.Errorf("load state (run 'rosmarops init' first): %w", err)
	}

	// Hash the binary.
	binaryHash, err := hash.Self()
	if err != nil {
		return fmt.Errorf("hash binary: %w", err)
	}

	// Hash the config file.
	cfgPath, err := config.ResolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfgRaw, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("hash config: %w", err)
	}
	configHash := hash.Bytes(cfgRaw)

	// Load config content to get repo_roots for per-project MCP scan.
	// If the config cannot be parsed, baseline with empty repo_roots (global configs only).
	cfg, cfgLoadErr := config.LoadConfig(cfgPath)
	if cfgLoadErr != nil {
		slog.Warn("baseline: could not load config; MCP per-project scan uses empty repo_roots", "err", cfgLoadErr)
	}

	// Report what changed.
	prevBinary := st.Integrity.VigilBinarySHA256
	prevConfig := st.Integrity.ConfigSHA256

	printHashDiff("binary", prevBinary, binaryHash)
	printHashDiff("config", prevConfig, configHash)

	// Write updated integrity hashes to state, and the copy of the accepted
	// configuration with its hash (adjustment A: the copy lists what
	// changes next).
	st.Integrity.VigilBinarySHA256 = binaryHash
	st.Integrity.ConfigSHA256 = configHash
	st.EngineVersion = version // the version the binary reference is of (J2)
	copyDir := ""
	if cfgLoadErr == nil {
		copyDir = cfg.ReportDirectory
	}
	if copyDir == "" {
		if copyDir, err = report.DefaultReportDir(); err != nil {
			return fmt.Errorf("resolve report dir: %w", err)
		}
	}
	if err := writeAcceptedCopy(copyDir, cfgRaw); err != nil {
		return err
	}

	plat := windowsadapter.New()

	// Snapshot VS Code extensions (M4).
	currentExts, extErr := plat.ListVSCodeExtensions(ctx)
	if extErr != nil {
		slog.Warn("baseline: could not list VS Code extensions", "err", extErr)
		fmt.Printf("  %-8s  WARNING: could not list extensions: %v\n", "vscode", extErr)
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		extBaseline := make([]state.VSCodeExtension, 0, len(currentExts))
		for _, ext := range currentExts {
			extBaseline = append(extBaseline, state.VSCodeExtension{
				ID:        strings.ToLower(ext.ID),
				Version:   ext.Version,
				FirstSeen: now,
			})
		}
		st.Baselines.VSCodeExtensions = extBaseline
		slog.Info("baseline: VS Code extensions", "count", len(extBaseline))
		fmt.Printf("  %-8s  %d extension(s) baselined\n", "vscode", len(extBaseline))
	}

	// MCP servers have no reference since decision 2026-10-26 (agentes y
	// MCP nuevos, A4): a new one is the v2 memory's.
	var repoRoots []string
	if cfgLoadErr == nil {
		repoRoots = cfg.RepoRoots
	}

	// Snapshot repo baselines (M6).
	repoScanCfg := repos.ScanConfig{
		Patterns: secrets.BuiltinPatterns(),
	}
	_, newRepoBaselines, repoErrs := repos.ScanAllRepos(ctx, repoRoots, st.Baselines.Repos, repoScanCfg)
	if len(repoErrs) > 0 {
		for _, e := range repoErrs {
			slog.Warn("baseline: repo scan error", "scope", e.Scope, "err", e.Message)
		}
		fmt.Printf("  %-8s  WARNING: %d repo(s) had scan errors (see log)\n", "repos", len(repoErrs))
	} else {
		st.Baselines.Repos = newRepoBaselines
		slog.Info("baseline: repos", "count", len(newRepoBaselines))
		fmt.Printf("  %-8s  %d repo(s) baselined\n", "repos", len(newRepoBaselines))
	}

	// Snapshot shell profiles (M7).
	currentProfiles, profErr := plat.ListShellProfiles(ctx)
	if profErr != nil {
		slog.Warn("baseline: could not list shell profiles", "err", profErr)
		fmt.Printf("  %-16s  WARNING: could not list shell profiles: %v\n", "shellprofile", profErr)
	} else {
		newSHA := shellprofile.CombinedSHA(currentProfiles)
		st.Baselines.ShellProfileSHA256 = newSHA
		slog.Info("baseline: shell profiles", "combined_sha256", newSHA[:16]+"…")
		fmt.Printf("  %-16s  %d profile scope(s) hashed\n", "shellprofile", len(currentProfiles))
	}

	// Snapshot scheduled tasks (M7).
	currentTasks, tasksErr := plat.ListScheduledTasks(ctx)
	if tasksErr != nil {
		slog.Warn("baseline: could not list scheduled tasks", "err", tasksErr)
		fmt.Printf("  %-16s  WARNING: could not list scheduled tasks: %v\n", "scheduledtasks", tasksErr)
	} else {
		taskBL := make([]state.ScheduledTaskBase, 0, len(currentTasks))
		for _, t := range currentTasks {
			taskBL = append(taskBL, state.ScheduledTaskBase{
				Name:   t.Name,
				SHA256: t.SHA256,
			})
		}
		st.Baselines.ScheduledTasks = taskBL
		slog.Info("baseline: scheduled tasks", "count", len(taskBL))
		fmt.Printf("  %-16s  %d task(s) baselined\n", "scheduledtasks", len(taskBL))
	}

	// Snapshot Defender exclusions (M7).
	currentExcls, avErr := plat.ListDefenderExclusions(ctx)
	if avErr != nil {
		slog.Warn("baseline: could not list Defender exclusions", "err", avErr)
		fmt.Printf("  %-16s  WARNING: could not list Defender exclusions: %v\n", "avexclusions", avErr)
	} else {
		avBL := make([]state.AVExclusion, 0, len(currentExcls))
		for _, e := range currentExcls {
			if e.Value == "" {
				continue
			}
			avBL = append(avBL, state.AVExclusion{Kind: e.Kind, Value: e.Value})
		}
		st.Baselines.AVExclusions = avBL
		slog.Info("baseline: Defender exclusions", "count", len(avBL))
		fmt.Printf("  %-16s  %d exclusion(s) baselined\n", "avexclusions", len(avBL))
	}

	// Snapshot global npm/pnpm packages + npmrc (M7).
	homeDir, homeDirErr := os.UserHomeDir()
	if homeDirErr != nil {
		slog.Warn("baseline: could not resolve home dir; npmrc check skipped", "err", homeDirErr)
		homeDir = ""
	}
	globalsResult := globals.ScanGlobals(ctx, homeDir, nil, nil)
	st.Baselines.GlobalPackages = globalsResult.NewPackageBaseline
	if globalsResult.NewNpmrcSHA != "" {
		st.Baselines.NpmrcSHA256 = globalsResult.NewNpmrcSHA
	}
	slog.Info("baseline: global packages", "count", len(globalsResult.NewPackageBaseline))
	fmt.Printf("  %-16s  %d global package(s) baselined\n", "globals", len(globalsResult.NewPackageBaseline))

	if err := state.SaveState(stPath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	slog.Info("baseline updated", "binary_sha256", binaryHash[:16]+"…", "config_sha256", configHash[:16]+"…")
	fmt.Println("\nBaseline updated. Run 'rosmarops run' to scan.")
	return nil
}

const zeroSHA = "0000000000000000000000000000000000000000000000000000000000000000"

func printHashDiff(label, prev, curr string) {
	switch {
	case prev == zeroSHA || prev == "":
		fmt.Printf("  %-8s  baseline established  %s…\n", label, curr[:16])
	case prev == curr:
		fmt.Printf("  %-8s  unchanged             %s…\n", label, curr[:16])
	default:
		fmt.Printf("  %-8s  CHANGED               %s… → %s…\n", label, prev[:16], curr[:16])
	}
}
