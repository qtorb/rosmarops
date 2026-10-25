package windows

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qtorb/rosmarops/internal/platform"
)

// psMpPreferenceResponse mirrors the relevant fields returned by
// Get-MpPreference serialized to JSON.
type psMpPreferenceResponse struct {
	ExclusionPath      []string `json:"ExclusionPath"`
	ExclusionProcess   []string `json:"ExclusionProcess"`
	ExclusionExtension []string `json:"ExclusionExtension"`
}

// ListDefenderExclusions implements platform.Platform.
//
// Reads Windows Defender exclusions via Get-MpPreference. Each exclusion kind
// (path, process, extension) is returned as a separate DefenderExclusion entry.
//
// The @() wrapper in the PowerShell script forces PowerShell to serialize null
// fields as empty arrays rather than JSON null, which simplifies Go parsing.
//
// Returns nil, nil if PowerShell is unavailable or Get-MpPreference fails
// (e.g., Defender not installed, insufficient privileges).
func (w *WindowsPlatform) ListDefenderExclusions(ctx context.Context) ([]platform.DefenderExclusion, error) {
	const psCmd = `try {` +
		`$p=Get-MpPreference;` +
		`@{` +
		`ExclusionPath=@($p.ExclusionPath);` +
		`ExclusionProcess=@($p.ExclusionProcess);` +
		`ExclusionExtension=@($p.ExclusionExtension)` +
		`} | ConvertTo-Json -Compress` +
		`} catch { Write-Output '{}' }`

	raw, err := runPS(ctx, psCmd)
	if err != nil {
		return nil, fmt.Errorf("defender: query exclusions: %w", err)
	}
	if raw == nil {
		return nil, nil
	}

	var resp psMpPreferenceResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("defender: parse JSON: %w", err)
	}

	var exclusions []platform.DefenderExclusion
	for _, v := range resp.ExclusionPath {
		if v != "" {
			exclusions = append(exclusions, platform.DefenderExclusion{Kind: "path", Value: v})
		}
	}
	for _, v := range resp.ExclusionProcess {
		if v != "" {
			exclusions = append(exclusions, platform.DefenderExclusion{Kind: "process", Value: v})
		}
	}
	for _, v := range resp.ExclusionExtension {
		if v != "" {
			exclusions = append(exclusions, platform.DefenderExclusion{Kind: "extension", Value: v})
		}
	}
	return exclusions, nil
}
