package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/state"
)

// neverRun is the zero time stored in state before the first rosmarops run.
const neverRun = "0001-01-01T00:00:00Z"

// zeroHash is defined in main.go (package-level shared constant).

func newStatusCmd() *cli.Command {
	return &cli.Command{
		Name: "status", Short: "cli.cmd.status.short", Long: "cli.cmd.status.long", Args: cli.AnyArgs,
		Before: integrityCheck,
		Run:    runStatus,
	}
}

func runStatus(cli.Invocation) error {
	cfgPath, err := config.ResolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}

	fmt.Printf("RosmarOps %s\n\n", version)
	fmt.Printf("  Config  %s\n", pathStatus(cfgPath))
	fmt.Printf("  State   %s\n\n", pathStatus(stPath))

	// Load config for schema version.
	cfg, cfgErr := config.LoadConfig(cfgPath)
	if cfgErr != nil && !errors.Is(cfgErr, fs.ErrNotExist) && !isNotExist(cfgErr) {
		fmt.Printf("  Config error: %v\n\n", cfgErr)
	}

	// Load state for runtime info.
	st, stErr := state.LoadState(stPath)
	if stErr != nil && !isNotExist(stErr) {
		fmt.Printf("  State error: %v\n\n", stErr)
	}

	// Schema versions.
	if cfgErr == nil {
		fmt.Printf("  Config schema version : %d\n", cfg.SchemaVersion)
	}
	if stErr == nil {
		fmt.Printf("  State schema version  : %d\n", st.SchemaVersion)
	}
	fmt.Println()

	// Last run.
	if stErr != nil || st.LastRun == neverRun || st.LastRun == "" {
		fmt.Printf("  Last run              : never\n")
	} else {
		t, err := time.Parse(time.RFC3339, st.LastRun)
		if err != nil {
			fmt.Printf("  Last run              : %s\n", st.LastRun)
		} else {
			fmt.Printf("  Last run              : %s (%s)\n",
				t.Local().Format("2006-01-02 15:04 MST"),
				st.LastRunStatus)
		}
	}

	// Consecutive green.
	if stErr == nil {
		fmt.Printf("  Consecutive green     : %d\n", st.ConsecutiveGreen)
	}
	fmt.Println()

	// Baseline integrity.
	if stErr == nil {
		binaryHash := st.Integrity.VigilBinarySHA256
		configHash := st.Integrity.ConfigSHA256
		if binaryHash == zeroHash || binaryHash == "" {
			fmt.Printf("  Binary baseline       : not set (run 'rosmarops baseline')\n")
		} else {
			fmt.Printf("  Binary baseline       : %s…\n", binaryHash[:16])
		}
		if configHash == zeroHash || configHash == "" {
			fmt.Printf("  Config baseline       : not set (run 'rosmarops baseline')\n")
		} else {
			fmt.Printf("  Config baseline       : %s…\n", configHash[:16])
		}
	}

	return nil
}

// pathStatus returns "<path>  [exists]" or "<path>  [missing]".
func pathStatus(path string) string {
	_, err := os.Stat(path)
	if err == nil {
		return path + "  [exists]"
	}
	return path + "  [missing]"
}

// isNotExist checks if the error chain contains a not-exist error,
// handling wrapped errors from ReadFile whose message includes the path.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// The schema validator or ReadFile may wrap ErrNotExist with path info.
	msg := err.Error()
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "cannot find the file") ||
		strings.Contains(msg, "The system cannot find")
}
