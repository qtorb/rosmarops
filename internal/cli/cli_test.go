package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/cli"
)

// program is a small tree with the shapes RosmarOps uses: a command with a
// flag and any arguments, a stub guarded by Before, and a group with an
// exact-count subcommand.
func program(out *bytes.Buffer, got *cli.Invocation, before *[]string) cli.Program {
	run := func(inv cli.Invocation) error { *got = inv; return nil }
	guard := func(path []string) error { *before = path; return nil }
	return cli.Program{
		Name: "rosmarops", Version: "9.9.9", Stdout: out, Stderr: out,
		Root: &cli.Command{Name: "rosmarops", Subs: []*cli.Command{
			{
				Name: "run", Short: "cli.cmd.run.short", Long: "cli.cmd.run.long", Args: cli.AnyArgs, Before: guard, Run: run,
				Flags: []cli.Flag{{Name: "lang", Default: "en", Usage: "cli.flag.lang"}},
			},
			{Name: "allow", Short: "cli.cmd.allow.short", Subs: []*cli.Command{
				{Name: "add", Usage: "<category> <id>", Short: "cli.cmd.allow.add.short", Args: 2, Run: run},
			}},
		}},
	}
}

func TestExecute(t *testing.T) {
	for _, c := range []struct {
		args       []string
		err        string // substring; "" is no error
		out        string // substring of the output
		lang, path string
		params     string
	}{
		{args: nil, out: "Usage: rosmarops"},
		{args: []string{"--lang", "es"}, out: "Uso: rosmarops"},
		{args: []string{"--lang=fr"}, err: "lang_unsupported"},
		{args: []string{"--version"}, out: "rosmarops version 9.9.9"},
		{args: []string{"-v"}, out: "rosmarops version 9.9.9"},
		{args: []string{"help", "run"}, out: "--lang"},
		{args: []string{"run", "-h", "--lang", "es"}, out: "idioma del informe"},
		{args: []string{"run", "--help"}, out: "rosmarops run"},
		{args: []string{"nope"}, err: `unknown command "nope" for "rosmarops"`},
		{args: []string{"--bogus"}, err: "unknown flag: --bogus"},
		{args: []string{"run", "--bogus"}, err: "unknown flag: --bogus"},
		{args: []string{"run", "-lang", "es"}, err: "unknown shorthand flag"},
		{args: []string{"run", "--lang"}, err: "flag needs an argument"},
		{args: []string{"run"}, lang: "en", path: "run"},
		{args: []string{"run", "a", "--lang", "es", "b"}, lang: "es", path: "run", params: "a b"},
		{args: []string{"run", "--lang=es", "--", "-x"}, lang: "es", path: "run", params: "-x"},
		{args: []string{"allow"}, out: "  add  "},
		{args: []string{"allow", "add", "-h"}, out: "rosmarops allow add <category> <id>"},
		{args: []string{"allow", "nope"}, out: "rosmarops allow"},
		{args: []string{"allow", "add", "x"}, err: "accepts 2 arg(s), received 1"},
		{args: []string{"allow", "add", "x", "y"}, path: "allow add", params: "x y"},
	} {
		var out bytes.Buffer
		var got cli.Invocation
		var before []string
		err := program(&out, &got, &before).Execute(c.args)
		switch {
		case c.err != "":
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%v: error %v, want %q", c.args, err, c.err)
			}
			continue
		case err != nil:
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if !strings.Contains(out.String(), c.out) {
			t.Errorf("%v: output %q lacks %q", c.args, out.String(), c.out)
		}
		if strings.Contains(out.String(), "[cli.") {
			t.Errorf("%v: a code without text in %q", c.args, out.String())
		}
		if c.path != "" {
			if strings.Join(got.Path, " ") != c.path || got.Flags["lang"] != c.lang && c.lang != "" || strings.Join(got.Args, " ") != c.params {
				t.Errorf("%v: invocation %+v", c.args, got)
			}
			if c.path == "run" && strings.Join(before, " ") != "run" {
				t.Errorf("%v: Before saw %v", c.args, before)
			}
		}
	}
}
