package windows

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Registry hives of Claude Code policy settings.
const (
	HiveHKLM = "HKLM"
	HiveHKCU = "HKCU"
)

// policyKey is the registry key of Claude Code policy settings; the policy
// is the JSON in its Settings value (REG_SZ or REG_EXPAND_SZ)
// (https://code.claude.com/docs/en/managed-settings#where-each-mechanism-stores-the-policy).
const policyKey = `SOFTWARE\Policies\ClaudeCode`

// RegistryReader reads the Settings value of HKLM or HKCU
// \SOFTWARE\Policies\ClaudeCode, and a single string value under a policy key
// (BV-140: Codex's RequirementsPath). exists is false when the key or the value
// is absent. Implementations only read.
type RegistryReader interface {
	PolicySettings(ctx context.Context, hive string) (value []byte, exists bool, err error)
	// StringValue reads one REG_SZ/REG_EXPAND_SZ value under hive\keyPath.
	// keyPath and valueName are fixed, code-supplied names (no user input).
	StringValue(ctx context.Context, hive, keyPath, valueName string) (value string, exists bool, err error)
	// Uint32Value reads one REG_DWORD value under hive\keyPath (BV-140: VS Code
	// policies such as ChatToolsAutoApprove are DWORDs). keyPath and valueName
	// are fixed, code-supplied names. exists is false when the key or value is
	// absent. Implementations only read.
	Uint32Value(ctx context.Context, hive, keyPath, valueName string) (value uint32, exists bool, err error)
}

// safeRegistryToken reports a registry key path or value name that is safe to
// embed in the read-only PowerShell snippet: letters, digits and the few
// punctuation characters a policy key uses. Callers pass constants; this is a
// guard against ever drifting to anything with a quote or newline.
func safeRegistryToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '\\' || r == '_' || r == '-' || r == '.' || r == ' ':
		default:
			return false
		}
	}
	return true
}

// registryPath is the coverage path of a hive's Settings value.
func registryPath(hive string) string {
	return "registry:" + hive + `\` + policyKey + `\Settings`
}

// psRegistry reads the registry through PowerShell (runPS), read-only:
// Test-Path and Get-ItemProperty, nothing that creates or changes a key.
type psRegistry struct{}

// Output markers of the PowerShell snippet.
const (
	psNoKey   = "ROSMAROPS:NOKEY"
	psNoValue = "ROSMAROPS:NOVALUE"
	psValue   = "ROSMAROPS:VALUE\n"
)

func (psRegistry) PolicySettings(ctx context.Context, hive string) ([]byte, bool, error) {
	if hive != HiveHKLM && hive != HiveHKCU {
		return nil, false, fmt.Errorf("unknown hive %q", hive)
	}
	script := `[Console]::OutputEncoding = [Text.Encoding]::UTF8
$p = '` + hive + `:\` + policyKey + `'
if (-not (Test-Path -LiteralPath $p)) { [Console]::Out.Write('` + psNoKey + `'); exit 0 }
$v = (Get-ItemProperty -LiteralPath $p -Name 'Settings' -ErrorAction SilentlyContinue).Settings
if ($null -eq $v) { [Console]::Out.Write('` + psNoValue + `'); exit 0 }
[Console]::Out.Write("` + "ROSMAROPS:VALUE`n" + `" + [string]$v)`
	out, err := runPS(ctx, script)
	if err != nil {
		return nil, false, err
	}
	switch {
	case out == nil, bytes.Equal(out, []byte(psNoKey)), bytes.Equal(out, []byte(psNoValue)):
		// nil: PowerShell absent, so the registry cannot be read here.
		return nil, false, nil
	case bytes.HasPrefix(out, []byte(psValue)):
		return out[len(psValue):], true, nil
	default:
		return nil, false, fmt.Errorf("unexpected PowerShell output for %s", registryPath(hive))
	}
}

func (psRegistry) StringValue(ctx context.Context, hive, keyPath, valueName string) (string, bool, error) {
	if hive != HiveHKLM && hive != HiveHKCU {
		return "", false, fmt.Errorf("unknown hive %q", hive)
	}
	if !safeRegistryToken(keyPath) || !safeRegistryToken(valueName) {
		return "", false, fmt.Errorf("unsafe registry token")
	}
	script := `[Console]::OutputEncoding = [Text.Encoding]::UTF8
$p = '` + hive + `:\` + keyPath + `'
if (-not (Test-Path -LiteralPath $p)) { [Console]::Out.Write('` + psNoKey + `'); exit 0 }
$v = (Get-ItemProperty -LiteralPath $p -Name '` + valueName + `' -ErrorAction SilentlyContinue).'` + valueName + `'
if ($null -eq $v) { [Console]::Out.Write('` + psNoValue + `'); exit 0 }
[Console]::Out.Write("` + "ROSMAROPS:VALUE`n" + `" + [string]$v)`
	out, err := runPS(ctx, script)
	if err != nil {
		return "", false, err
	}
	switch {
	case out == nil, bytes.Equal(out, []byte(psNoKey)), bytes.Equal(out, []byte(psNoValue)):
		return "", false, nil
	case bytes.HasPrefix(out, []byte(psValue)):
		return string(out[len(psValue):]), true, nil
	default:
		return "", false, fmt.Errorf("unexpected PowerShell output for %s\\%s", keyPath, valueName)
	}
}

func (psRegistry) Uint32Value(ctx context.Context, hive, keyPath, valueName string) (uint32, bool, error) {
	if hive != HiveHKLM && hive != HiveHKCU {
		return 0, false, fmt.Errorf("unknown hive %q", hive)
	}
	if !safeRegistryToken(keyPath) || !safeRegistryToken(valueName) {
		return 0, false, fmt.Errorf("unsafe registry token")
	}
	// A REG_DWORD prints as its decimal digits via [string]$v; the same
	// read-only Get-ItemProperty snippet as StringValue.
	script := `[Console]::OutputEncoding = [Text.Encoding]::UTF8
$p = '` + hive + `:\` + keyPath + `'
if (-not (Test-Path -LiteralPath $p)) { [Console]::Out.Write('` + psNoKey + `'); exit 0 }
$v = (Get-ItemProperty -LiteralPath $p -Name '` + valueName + `' -ErrorAction SilentlyContinue).'` + valueName + `'
if ($null -eq $v) { [Console]::Out.Write('` + psNoValue + `'); exit 0 }
[Console]::Out.Write("` + "ROSMAROPS:VALUE`n" + `" + [string]$v)`
	out, err := runPS(ctx, script)
	if err != nil {
		return 0, false, err
	}
	switch {
	case out == nil, bytes.Equal(out, []byte(psNoKey)), bytes.Equal(out, []byte(psNoValue)):
		return 0, false, nil
	case bytes.HasPrefix(out, []byte(psValue)):
		n, perr := strconv.ParseUint(strings.TrimSpace(string(out[len(psValue):])), 10, 32)
		if perr != nil {
			return 0, false, fmt.Errorf("registry value %s\\%s is not a DWORD: %w", keyPath, valueName, perr)
		}
		return uint32(n), true, nil
	default:
		return 0, false, fmt.Errorf("unexpected PowerShell output for %s\\%s", keyPath, valueName)
	}
}
