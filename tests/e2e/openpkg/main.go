// Command openpkg opens the sealed space-view package that a run wrote, using
// the kit key stored on this computer, and prints a one-line summary. It proves
// the BV-157 realistic end-to-end run sealed a package the kit can open, without
// any network: it only reads the local kit store (internal/kit) and the package
// file, and calls results.Open.
//
// It is a test helper under tests/e2e and is never built into rosmarops.exe
// (the release build is `go build ... ./cmd/rosmarops`). It takes the package
// path as its only argument; the kit store is the default one on this computer.
//
//	go run ./tests/e2e/openpkg <reports>/rosmarops-results-YYYY-MM-DD-xxxxxxxx.locked.json
//
// Exit 0 and "ok ..." on success; exit 1 and "openpkg: ..." on any failure.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/results"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: openpkg <package.locked.json>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "openpkg:", err)
		os.Exit(1)
	}
}

func run(pkgPath string) error {
	storePath, err := kit.StorePath()
	if err != nil {
		return fmt.Errorf("kit store path: %w", err)
	}
	stored, err := kit.Load(storePath)
	if err != nil {
		return fmt.Errorf("load kit %s: %w", storePath, err)
	}
	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("read package: %w", err)
	}
	pkg, err := results.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse package: %w", err)
	}
	content, err := results.Open(stored.Secret.Key, pkg)
	if err != nil {
		return fmt.Errorf("open package with kit key: %w", err)
	}
	if len(content) == 0 {
		return fmt.Errorf("package opened but its content is empty")
	}
	fmt.Printf("ok %s opened: %d bytes of content, host_id=%s, rosmarops=%s\n",
		filepath.Base(pkgPath), len(content), pkg.Header.HostID, pkg.Header.RosmaropsVersion)
	return nil
}
