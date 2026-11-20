// Command exporttemplates writes the open templates repository
// (qtorb/rosmarops-templates) for one RosmarOps version: every template
// «Choose a template» offers, under templates/<agent>/<use>/, with the text
// the report copies and a README of the catalog's English texts that describe
// it, and the Claude Code skill under skills/rosmarops-templates/. A release
// tool, never part of the program; standard library only. Nothing it writes
// comes from the computer it runs on. From the repository root:
//
//	go run ./tools/exporttemplates -version 0.16.0 -out ../rosmarops-templates
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/qtorb/rosmarops/internal/report"
)

// skill is the Claude Code skill of the templates repository, literal
// (BV-266 §2, signed with GTM-190 on 20-11; steps 2 and 5 as the advisor set them on 20-11).
//
//go:embed SKILL.md
var skill string

var versionRx = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

func main() {
	version := flag.String("version", "", "the RosmarOps version, X.Y.Z")
	out := flag.String("out", "", "the templates repository's folder")
	flag.Parse()
	if err := run(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "exporttemplates:", err)
		os.Exit(1)
	}
}

func run(version, out string) error {
	if !versionRx.MatchString(version) || out == "" {
		return fmt.Errorf("usage: exporttemplates -version X.Y.Z -out DIR")
	}
	files, err := Files(version)
	if err != nil {
		return err
	}
	// The export owns templates/ and the skill: what an older version had
	// there and this one does not is removed.
	if err := os.RemoveAll(filepath.Join(out, "templates")); err != nil {
		return err
	}
	for _, f := range files {
		p := filepath.Join(out, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("exporttemplates: %d files for %s in %s\n", len(files), version, out)
	return nil
}

// Files is everything the export writes: the templates and the skill.
func Files(version string) ([]report.TemplateFile, error) {
	files, err := report.ExportTemplates(version)
	if err != nil {
		return nil, err
	}
	return append(files, report.TemplateFile{Path: "skills/rosmarops-templates/SKILL.md", Content: skill}), nil
}
