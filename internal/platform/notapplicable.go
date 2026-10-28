package platform

import "errors"

// ErrNotApplicable is what a check with no equivalent on this system
// returns (decision 2026-10-28, macOS, M1a): on macOS, the scheduled tasks,
// Defender's exclusions, the PowerShell profiles, the notification, opening
// the report, the folder picker and the display language. It is not a
// failure and not a coverage gap: it does not make the report yellow, and
// only the log says it.
//
// TODO(M1b): this holds only while no macOS binary is published (Albert,
// 2026-10-28). Before publishing one, the report must say what is not
// checked on macOS, with signed catalog texts.
var ErrNotApplicable = errors.New("not applicable on this system")
