package knowledge

import (
	"path"
	"strings"
)

// The launcher rule (decision 2026-10-24, informe legible, §4, E8): an
// execute_shell allow rule is equivalent to an unrestricted shell when the
// commands it admits include arbitrary code. Claude Code matches everything
// before the first "*" as written (permissions, "Wildcard patterns"), so the
// rule is read on its fixed text, up to the first "*":
//   - second_shell (cmd /c, powershell -Command, ...): the option is in the
//     fixed text; any "*" after it counts;
//   - inline_code (node -e, python -c, ...): the "*" comes right after the
//     option, or inside a code argument not yet closed (an open quote, or a
//     token the "*" continues); not after a closed code argument;
//   - npx: "npx *", or "npx <package> *" for a code-running package;
//   - git: the "*" can cover options before the subcommand (no subcommand
//     in the fixed text);
//   - env_launcher (docker exec, ...): any "*" after the subcommand.

// token is one shell word of a rule's fixed text. complete is false for the
// last word when the fixed text ends inside it (an open quote, or no
// whitespace after it): the "*" continues it.
type token struct {
	text     string
	complete bool
}

// tokenize splits the fixed text of a rule into shell words: whitespace
// outside quotes separates words; '...' is literal; "..." and unquoted text
// take a backslash as an escape of the next character.
func tokenize(s string) []token {
	var out []token
	var cur strings.Builder
	inWord, quote, escaped := false, byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\\':
			escaped, inWord = true, true
		case quote == '"':
			if c == '"' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				out = append(out, token{cur.String(), true})
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord || quote != 0 || escaped {
		out = append(out, token{cur.String(), false})
	}
	return out
}

// programName is the command name of a word: its base name, lowercased,
// without ".exe".
func programName(w string) string {
	w = strings.ReplaceAll(w, `\`, "/")
	return strings.TrimSuffix(strings.ToLower(path.Base(w)), ".exe")
}

// windowsOptions are the programs whose options are case-insensitive.
var windowsOptions = map[string]bool{"cmd": true, "powershell": true, "pwsh": true}

// LauncherOf returns the code launcher a shell rule's specifier falls in,
// and false when it falls in none (no "*", or the "*" is where no code goes).
// It is the one detection of launchers: the rules layer's egress and the
// graph's write inference use it (decision 2026-10-26, lanzadores en
// escritura, L1).
func LauncherOf(spec string, launchers []CodeLauncher) (CodeLauncher, bool) {
	star := strings.Index(spec, "*")
	if star < 0 {
		return CodeLauncher{}, false
	}
	toks := tokenize(spec[:star])
	if len(toks) == 0 || !toks[0].complete {
		return CodeLauncher{}, false
	}
	prog := programName(toks[0].text)
	args := toks[1:]
	for _, l := range launchers {
		if l.Program != prog {
			continue
		}
		var ok bool
		switch l.Family {
		case "second_shell":
			ok = hasOption(args, l.Options, windowsOptions[prog]) >= 0
		case "inline_code":
			ok = inlineCode(args, l.Options)
		case "npx":
			ok = npxPackage(args, l.Package)
		case "git":
			ok = gitOptionsOpen(args)
		case "env_launcher":
			ok = len(args) > 0 && args[0].complete && args[0].text == l.Subcommand
		}
		if ok {
			return l, true
		}
	}
	return CodeLauncher{}, false
}

// hasOption returns the index of the first complete word that is one of
// options, or -1.
func hasOption(args []token, options []string, foldCase bool) int {
	for i, a := range args {
		if !a.complete {
			continue
		}
		for _, o := range options {
			if a.text == o || foldCase && strings.EqualFold(a.text, o) {
				return i
			}
		}
	}
	return -1
}

// inlineCode reports a "*" right after a code option, or inside the code
// argument that follows it while that argument is not closed. A long option
// may carry its code after "=" (--eval=code).
func inlineCode(args []token, options []string) bool {
	for i, a := range args {
		for _, o := range options {
			if strings.HasPrefix(o, "--") && strings.HasPrefix(a.text, o+"=") {
				return i == len(args)-1 && !a.complete
			}
		}
		if !a.complete {
			return false
		}
		for _, o := range options {
			if a.text != o {
				continue
			}
			rest := args[i+1:]
			// Right after the option, or inside its unclosed code argument.
			return len(rest) == 0 || len(rest) == 1 && !rest[0].complete
		}
	}
	return false
}

// npxPackage reports "npx *" (pkg empty: no package in the fixed text) or
// "npx <pkg> *". Options before the package are skipped; a package name
// loses its version ("tsx@4" is tsx).
func npxPackage(args []token, pkg string) bool {
	var name string
	for _, a := range args {
		if !a.complete {
			// The "*" continues a word: the package is not fixed, unless the
			// word is an option.
			if strings.HasPrefix(a.text, "-") {
				break
			}
			return false
		}
		if strings.HasPrefix(a.text, "-") {
			continue
		}
		name = a.text
		break
	}
	if name == "" {
		return pkg == ""
	}
	if at := strings.LastIndex(name, "@"); at > 0 {
		name = name[:at]
	}
	return pkg != "" && strings.ToLower(name) == pkg
}

// gitValueOptions are git global options written with their value as the
// next word, so that word is not taken for the subcommand.
var gitValueOptions = map[string]bool{"-c": true, "-C": true, "--git-dir": true, "--work-tree": true, "--namespace": true}

// gitOptionsOpen reports a git rule whose fixed text has no subcommand: the
// "*" can stand in for options before it, -c included.
func gitOptionsOpen(args []token) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !a.complete {
			return strings.HasPrefix(a.text, "-") || i > 0 && gitValueOptions[args[i-1].text]
		}
		if !strings.HasPrefix(a.text, "-") {
			return false // the subcommand is fixed
		}
		if gitValueOptions[a.text] {
			i++ // its value
		}
	}
	return true
}
