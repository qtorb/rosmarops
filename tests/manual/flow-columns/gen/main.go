// Command gen renders the example report to four HTML files for the BV-141
// visual check: en/es × with/without «Your space». It is a manual helper
// (tests/manual), never part of CI.
//
//	go run ./tests/manual/flow-columns/gen -out /tmp/flowcols
//
// It loads the vendored example graph, builds the v2 report with a minimal
// input, and renders each page. «Your space» is toggled on the built report
// exactly as cmd_run does after a send.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/report"
)

func main() {
	out := flag.String("out", "flowcols-out", "output directory for the HTML files")
	graphPath := flag.String("graph", "schemas/v2/graph.v2.example.json", "example graph JSON")
	flag.Parse()

	if err := run(*out, *graphPath); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(out, graphPath string) error {
	g, err := graph.LoadGraph(graphPath)
	if err != nil {
		return fmt.Errorf("load graph: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	v1 := report.Build("example", nil, nil, 0, 0, 0, 0, nil)
	v1.GeneratedAt = "2026-10-22T09:00:00Z"
	base, _ := report.BuildV2(report.V2Input{
		Graph: &g, GeneratedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		V1: v1, Home: "/home/sample",
	})
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			return err
		}
		for _, space := range []bool{false, true} {
			v := base
			v.SpaceConnected = space
			if space {
				v.SpaceSentDate = "2026-10-22"
			}
			name := fmt.Sprintf("report.%s.%s.html", lang, map[bool]string{false: "nospace", true: "space"}[space])
			page := report.RenderHTML(v, cat, "/home/sample", time.UTC)
			if err := os.WriteFile(filepath.Join(out, name), []byte(page), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%d bytes, space=%v)\n", name, len(page), space)
		}
	}
	return nil
}
