package main

import "io"

// BV-211b (CTO-34): the WinForms scripts are far too large to pass on
// powershell.exe's command line (the dismiss script alone is over Windows'
// ~32 KB limit, so CreateProcess fails with «The filename or extension is too
// long»). Every window instead runs the same tiny bootstrap on the command
// line; the bootstrap reads the real script from standard input up to a
// sentinel line and runs it, and the window's own data follows on the same
// standard input. The command line is a fixed, small size that never grows
// with the number of findings, and nothing is written to disk. This launcher
// is platform-neutral so it can be tested on every system; only the exec calls
// that use it live in the _windows.go files.

// windowEndOfScript is the sentinel line that ends the script on standard
// input. No window script may contain it (a trust test checks this).
const windowEndOfScript = "#rosmarops-end-of-script"

// windowBootstrap is the literal command run by every window (CTO-34): it reads
// lines from standard input into a script until the sentinel, then runs that
// script. $null from ReadLine (a closed input) exits 3.
const windowBootstrap = `$sb=New-Object System.Text.StringBuilder;while($true){$l=[Console]::In.ReadLine();if($null -eq $l){exit 3};if($l -ceq '#rosmarops-end-of-script'){break};[void]$sb.AppendLine($l)};. ([scriptblock]::Create($sb.ToString()))`

// windowCmdArgs are powershell.exe's arguments for every window: the bootstrap,
// nothing data-dependent, so the command line is fixed and small.
func windowCmdArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", windowBootstrap}
}

// windowArgv is the full command line (with the interpreter) a window runs, for
// tests to check it is exactly the fixed bootstrap invocation.
func windowArgv(ps string) []string {
	return append([]string{ps}, windowCmdArgs()...)
}

// writeWindowScript sends a window's script followed by the sentinel, so the
// bootstrap runs it; the window's own data (the base64 config, and the connect
// window's live protocol) follows on the same standard input.
func writeWindowScript(w io.Writer, script string) error {
	_, err := io.WriteString(w, script+"\n"+windowEndOfScript+"\n")
	return err
}
