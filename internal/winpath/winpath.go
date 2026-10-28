// Package winpath resolves trusted Windows system paths through the Windows
// API (GetSystemDirectory / GetWindowsDirectory), not through environment
// variables such as %SystemRoot% or %PATH% that a caller or a tampered
// profile could redirect (BV-93, B4). Off Windows every function returns "",
// and callers fall back to their previous behaviour — PowerShell and Explorer
// are not there anyway.
package winpath

import (
	"path/filepath"
	"strings"
)

// ChildEnv returns base (an os.Environ()-shaped slice) with PSModulePath
// replaced by the single system module directory, so a PowerShell child's
// module auto-loading cannot be pointed at a module the environment names
// (BV-93, B4). Off Windows (no system directory) base is returned unchanged.
func ChildEnv(base []string) []string {
	mods := SystemModules()
	if mods == "" {
		return base
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(strings.ToUpper(kv), "PSMODULEPATH=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "PSModulePath="+mods)
}

// PowerShellExe is Windows PowerShell's absolute path under the system
// directory (…\System32\WindowsPowerShell\v1.0\powershell.exe), or "" off
// Windows or when the API gave nothing.
func PowerShellExe() string {
	d := SystemDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "WindowsPowerShell", "v1.0", "powershell.exe")
}

// ExplorerExe is Explorer's absolute path under the Windows directory
// (…\explorer.exe), or "" off Windows.
func ExplorerExe() string {
	d := WindowsDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "explorer.exe")
}

// SystemModules is the system PowerShell module directory
// (…\System32\WindowsPowerShell\v1.0\Modules), or "" off Windows. It is the
// only entry a PowerShell child's PSModulePath is given, so module
// auto-loading cannot be pointed at a module a repository or the environment
// chose.
func SystemModules() string {
	d := SystemDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "WindowsPowerShell", "v1.0", "Modules")
}

// Module is the absolute path of a named system module's directory, for
// Import-Module by absolute path (e.g. ScheduledTasks, Defender), or "" off
// Windows.
func Module(name string) string {
	m := SystemModules()
	if m == "" {
		return ""
	}
	return filepath.Join(m, name)
}
