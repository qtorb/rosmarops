package report

import (
	"fmt"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// The open templates repository (qtorb/rosmarops-templates) holds the same
// templates «Choose a template» offers, exported by tools/exporttemplates on
// each release: one folder per agent and template, with the text the report
// copies, unchanged, and a README made only of the catalog's English texts
// that describe the template in the report. Nothing in them comes from the
// computer that runs the export: the blocks are constants of
// internal/profiles and the texts are the catalog's.

// TemplateFile is one file of the export: its path inside the repository and
// its content.
type TemplateFile struct {
	Path    string
	Content string
}

// exportTemplate is one template to export.
type exportTemplate struct {
	agent, agentName string // the folder and the agent's product name
	id               string // profiles.P0…P3
	file             string // the file name of what the report copies
	block            string // what the report copies after its sentence
	sentence         string // the catalog key of the sentence before the block; "" for a command
	steps            []string
	rows             [3][][2]string
	nc               string
	// ncBySystem, when set, replaces nc: «Doesn't cover» differs by system
	// (Claude Code's P1), each text after its system's label.
	ncBySystem [][2]string
}

// templateUse is a template's folder name, from its id.
var templateUse = map[string]string{
	profiles.P0: "read-only",
	profiles.P1: "local-development",
	profiles.P2: "sandboxed-development",
	profiles.P3: "unattended",
}

// exportTemplates are every template the report can offer, on any system:
// Claude Code's four, and the guided agents' that copy a text. Grok Bot's
// templates copy nothing, so they have no file to export.
func exportTemplates() []exportTemplate {
	var out []exportTemplate
	for _, id := range []string{profiles.P0, profiles.P1, profiles.P2, profiles.P3} {
		r := profileRows[id]
		t := exportTemplate{
			agent: "claude-code", agentName: "Claude Code", id: id,
			file: "settings.json", block: profiles.Block(id), sentence: "cc.T1",
			rows: [3][][2]string{r[0], r[1], r[2]}, nc: "cc." + id + ".nc",
		}
		switch id {
		case profiles.P1:
			// The report shows cc.P1.nc on Windows and cc.P1b.nc on macOS
			// and Linux; the export gives both, each after its system.
			t.ncBySystem = [][2]string{{"Windows: ", "cc.P1.nc"}, {"macOS and Linux: ", "cc.P1b.nc"}}
			t.steps = []string{"cc.S1", "cc.S2", "cc.S3"}
		case profiles.P2:
			t.steps = []string{"cc.P2.step.1"}
		case profiles.P3:
			t.file, t.sentence, t.steps = "command.txt", "", []string{"cc.S6"}
		default:
			t.steps = []string{"cc.S1", "cc.S2", "cc.S3"}
		}
		out = append(out, t)
	}
	files := map[string]string{profiles.GuidedCursor: "sandbox.json", profiles.GuidedGrok: "config.toml"}
	names := map[string]string{profiles.GuidedCursor: "Cursor", profiles.GuidedGrok: "Grok Build"}
	for _, g := range guidedAgents {
		if files[g.key] == "" {
			continue
		}
		for _, p := range g.profiles {
			block := profiles.GuidedBlock(g.key, p.id)
			if block == "" {
				continue
			}
			t := exportTemplate{
				agent: g.key, agentName: names[g.key], id: p.id, file: files[g.key], block: block,
				sentence: p.sentence, steps: p.steps, rows: p.rows, nc: p.nc,
			}
			if p.cmd {
				t.file, t.sentence = "command.txt", ""
			}
			out = append(out, t)
		}
	}
	return out
}

// ExportTemplates is the open templates repository's content for version:
// templates/<agent>/<use>/ with the file the report copies and its README.
func ExportTemplates(version string) ([]TemplateFile, error) {
	cat, err := catalog.Load("en")
	if err != nil {
		return nil, fmt.Errorf("export templates: %w", err)
	}
	var out []TemplateFile
	for _, t := range exportTemplates() {
		use := templateUse[t.id]
		if use == "" {
			return nil, fmt.Errorf("export templates: no folder for %s %s", t.agent, t.id)
		}
		dir := "templates/" + t.agent + "/" + use + "/"
		out = append(out,
			TemplateFile{Path: dir + t.file, Content: t.block + "\n"},
			TemplateFile{Path: dir + "README.md", Content: templateReadme(cat, t, version)})
	}
	return out, nil
}

// templateReadme is a template's README: its name, the sentence the report
// copies with it (or the steps), its four rows and the generated line, all
// the catalog's English texts.
func templateReadme(cat catalog.Catalog, t exportTemplate, version string) string {
	var b strings.Builder
	name := cat.Text("cc.name."+t.id, nil)
	fmt.Fprintf(&b, "# %s · %s\n\n", t.agentName, name)
	for i, s := range t.steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, cat.Text(s, nil))
	}
	if len(t.steps) > 0 {
		b.WriteString("\n")
	}
	if t.sentence != "" {
		fmt.Fprintf(&b, "%s\n\n", cat.Text(t.sentence, nil))
	}
	for i, head := range []string{"cc.row.does", "cc.row.asks", "cc.row.no"} {
		fmt.Fprintf(&b, "## %s\n\n", cat.Text(head, nil))
		if len(t.rows[i]) == 0 {
			b.WriteString("—\n\n")
			continue
		}
		for _, l := range t.rows[i] {
			fmt.Fprintf(&b, "- %s\n", cat.Text(l[1], nil))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## %s\n\n", cat.Text("cc.row.nc", nil))
	if len(t.ncBySystem) == 0 {
		fmt.Fprintf(&b, "%s\n\n", cat.Text(t.nc, nil))
	}
	for _, l := range t.ncBySystem {
		fmt.Fprintf(&b, "%s%s\n\n", l[0], cat.Text(l[1], nil))
	}
	fmt.Fprintf(&b, "Generated by RosmarOps %s. Do not edit by hand.\n", version)
	return b.String()
}
