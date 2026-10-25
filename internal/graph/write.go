package graph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the graph file written to the reports directory. Unlike the v1
// report, its name is fixed: the graph is the current state and each run
// replaces it.
const FileName = "graph.v2.json"

// WriteFile encodes g (schema and integrity validated) and writes it to
// dir/graph.v2.json, creating dir if needed. dir must be the configured
// reports directory, one of RosmarOps's two allowed write targets.
//
// On any failure the previous graph file is removed, so a stale graph from an
// earlier run never stands in for the current one.
func WriteFile(dir string, g Graph) (string, error) {
	path := filepath.Join(dir, FileName)

	out, err := Encode(g)
	if err != nil {
		return "", removeStale(path, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", removeStale(path, fmt.Errorf("create graph dir %s: %w", dir, err))
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", removeStale(path, fmt.Errorf("write graph %s: %w", path, err))
	}
	return path, nil
}

// RemoveStale deletes a previous graph file in dir, if any. Callers use it
// when no graph can be built this run.
func RemoveStale(dir string) error {
	path := filepath.Join(dir, FileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale graph %s: %w", path, err)
	}
	return nil
}

// removeStale removes the graph at path after cause, and returns cause (joined
// with the removal error if that also failed).
func removeStale(path string, cause error) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(cause, fmt.Errorf("remove stale graph %s: %w", path, err))
	}
	return cause
}
