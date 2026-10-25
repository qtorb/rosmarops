// Package cli is RosmarOps's command dispatcher on the standard flag package
// (decision 2026-10-25, sustituir gojsonschema, G4): commands, subcommands,
// string flags and exact argument counts, with help texts from the message
// catalog in the --lang language (English by default). It replaces cobra and
// pflag, which linked net into the binary.
//
// The surface is cobra's as RosmarOps used it: "-h", "--help" and "help"
// print help and exit 0, as does a command with subcommands run alone;
// "--version" and "-v" print the version; flags may come before or after
// arguments; an unknown command or flag, or a wrong argument count, is an
// error.
package cli

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// HelpKeys are the catalog codes the help can emit, sorted. The tests check
// them against the command tree and the catalogs.
var HelpKeys = []string{
	"cli.cmd.allow.add.short", "cli.cmd.allow.list.short", "cli.cmd.allow.remove.short", "cli.cmd.allow.short",
	"cli.cmd.baseline.long", "cli.cmd.baseline.short", "cli.cmd.init.long", "cli.cmd.init.short",
	"cli.cmd.run.long", "cli.cmd.run.short", "cli.cmd.schedule.short", "cli.cmd.status.long", "cli.cmd.status.short",
	"cli.flag.lang", "cli.flag.repos_under", "cli.help.more", "cli.usage",
}

// Flag is a string flag; Usage is its catalog code.
type Flag struct {
	Name    string
	Default string
	Usage   string
}

// AnyArgs accepts any number of arguments.
const AnyArgs = -1

// Command is a command or a subcommand. Short and Long are catalog codes
// (Long may be empty). A command with Subs and no Run prints its help.
type Command struct {
	Name  string
	Usage string // the argument part of the usage line, data: "<category> <id>"
	Short string
	Long  string
	Flags []Flag
	Args  int // AnyArgs, or the exact count
	Subs  []*Command
	// Before runs before Run, with the command's path from the root (the
	// integrity check).
	Before func(path []string) error
	Run    func(inv Invocation) error
}

// Invocation is a command's parsed flags and arguments.
type Invocation struct {
	Path  []string
	Flags map[string]string
	Args  []string
}

// Program is the root of the tree.
type Program struct {
	Name    string
	Version string
	Root    *Command
	Stdout  io.Writer
	Stderr  io.Writer
}

// Execute runs args (without the program name). A nil error is exit 0; any
// error is exit 1, printed by the caller.
func (p Program) Execute(args []string) error {
	if len(args) > 0 && args[0] == "help" {
		target, path, _ := p.find(args[1:])
		return p.help(target, path, args)
	}
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(p.Stdout, "%s version %s\n", p.Name, p.Version)
		return nil
	}
	cmd, path, rest := p.find(args)
	if wantsHelp(rest) {
		return p.help(cmd, path, args)
	}
	if cmd == p.Root {
		switch {
		case len(rest) == 0, len(rest) == 2 && rest[0] == "--lang", len(rest) == 1 && strings.HasPrefix(rest[0], "--lang="):
			return p.help(cmd, path, args)
		case strings.HasPrefix(rest[0], "-"):
			return fmt.Errorf("unknown flag: %s", rest[0])
		}
		return fmt.Errorf("unknown command %q for %q", rest[0], p.Name)
	}
	if cmd.Run == nil {
		return p.help(cmd, path, args)
	}
	inv, err := parse(cmd, path, rest)
	if err != nil {
		return err
	}
	if cmd.Before != nil {
		if err := cmd.Before(path); err != nil {
			return err
		}
	}
	return cmd.Run(inv)
}

// find walks the leading command names of args.
func (p Program) find(args []string) (*Command, []string, []string) {
	cmd, path := p.Root, []string{}
	for len(args) > 0 {
		next := sub(cmd, args[0])
		if next == nil {
			break
		}
		cmd, path, args = next, append(path, next.Name), args[1:]
	}
	return cmd, path, args
}

func sub(c *Command, name string) *Command {
	for _, s := range c.Subs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// wantsHelp reports -h or --help before a "--".
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// helpLang is the --lang of args, for the help: en when absent; a language
// without a catalog is an error, as for run.
func helpLang(args []string) (catalog.Catalog, error) {
	lang := catalog.DefaultLang
	for i, a := range args {
		if a == "--" {
			break
		}
		switch {
		case a == "--lang" && i+1 < len(args):
			lang = args[i+1]
		case strings.HasPrefix(a, "--lang="):
			lang = strings.TrimPrefix(a, "--lang=")
		}
	}
	cat, err := catalog.Load(lang)
	if err != nil {
		return catalog.Catalog{}, fmt.Errorf("lang_unsupported: %q (en, es)", lang)
	}
	return cat, nil
}

// parse parses cmd's flags, which may come before or after its arguments,
// with the standard flag package. Only the "--name" spelling is accepted, as
// with cobra.
func parse(cmd *Command, path, args []string) (Invocation, error) {
	fs := flag.NewFlagSet(strings.Join(path, " "), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	values := map[string]*string{}
	for _, f := range cmd.Flags {
		values[f.Name] = fs.String(f.Name, f.Default, f.Usage)
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--") && len(a) > 2:
			name, _, hasValue := strings.Cut(a[2:], "=")
			if _, known := values[name]; !known {
				return Invocation{}, fmt.Errorf("unknown flag: --%s", name)
			}
			flags = append(flags, a)
			if !hasValue && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return Invocation{}, fmt.Errorf("unknown shorthand flag: %q in %s", a[1:2], a)
		default:
			positional = append(positional, a)
		}
	}
	if err := fs.Parse(flags); err != nil {
		return Invocation{}, err
	}
	if cmd.Args != AnyArgs && len(positional) != cmd.Args {
		return Invocation{}, fmt.Errorf("accepts %d arg(s), received %d", cmd.Args, len(positional))
	}
	inv := Invocation{Path: path, Flags: map[string]string{}, Args: positional}
	for name, v := range values {
		inv.Flags[name] = *v
	}
	return inv, nil
}

// help prints cmd's help to standard output: the root's usage and command
// list, or a command's text, flags and subcommands. Only the texts come from
// the catalog; names, flags and the usage line are data.
func (p Program) help(cmd *Command, path, args []string) error {
	cat, err := helpLang(args)
	if err != nil {
		return err
	}
	var b strings.Builder
	if cmd == p.Root {
		b.WriteString(cat.Text("cli.usage", nil) + "\n")
	} else {
		text := cmd.Short
		if cmd.Long != "" {
			text = cmd.Long
		}
		b.WriteString(cat.Text(text, nil) + "\n\n")
		line := append([]string{p.Name}, path...)
		if cmd.Usage != "" {
			line = append(line, cmd.Usage)
		}
		b.WriteString("  " + strings.Join(line, " ") + "\n")
	}
	if len(cmd.Subs) > 0 {
		if cmd != p.Root {
			b.WriteString("\n")
		}
		width := 0
		for _, s := range cmd.Subs {
			width = max(width, len(s.Name))
		}
		for _, s := range cmd.Subs {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, s.Name, cat.Text(s.Short, nil))
		}
	}
	if len(cmd.Flags) > 0 {
		b.WriteString("\n")
		flags := append([]Flag{}, cmd.Flags...)
		sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
		for _, f := range flags {
			fmt.Fprintf(&b, "  --%s  %s\n", f.Name, cat.Text(f.Usage, nil))
		}
	}
	if cmd == p.Root {
		b.WriteString("\n" + cat.Text("cli.help.more", nil) + "\n")
	}
	_, err = io.WriteString(p.Stdout, b.String())
	return err
}
