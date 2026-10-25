package windows

import (
	"bytes"
	"context"
	"fmt"
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
// \SOFTWARE\Policies\ClaudeCode. exists is false when the key or the value is
// absent. Implementations only read.
type RegistryReader interface {
	PolicySettings(ctx context.Context, hive string) (value []byte, exists bool, err error)
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
