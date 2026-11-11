package main

import "strings"

// storeAliasExe maps the running binary's real path to the path RosmarOps should
// name in the weekly-review instructions and in the scheduled task it looks for
// (BV-209). When RosmarOps is installed from the Microsoft Store, the real exe
// lives under «…\WindowsApps\<package>\rosmarops.exe», a versioned folder that
// changes with every update — a scheduled task pointing there breaks on the next
// update. The stable path is the execution alias
// «%LOCALAPPDATA%\Microsoft\WindowsApps\rosmarops.exe» (uap3:AppExecutionAlias),
// which is on PATH and never changes. Outside the Store (real is not under
// WindowsApps) or when LOCALAPPDATA is unknown, the real path is returned
// unchanged, so nothing changes for the standalone .exe or on macOS/Linux.
//
// This is used only for what the person is told to run and for matching their
// weekly task. The startup integrity check still hashes the real running binary
// (hash.Self → os.Executable), so invariant 8 is unaffected.
func storeAliasExe(real, localAppData string) string {
	if real == "" || localAppData == "" {
		return real
	}
	// Match «\WindowsApps\» case-insensitively; the Store install is the only
	// place a RosmarOps exe runs from there.
	if !strings.Contains(strings.ToLower(real), `\windowsapps\`) {
		return real
	}
	return localAppData + `\Microsoft\WindowsApps\rosmarops.exe`
}
