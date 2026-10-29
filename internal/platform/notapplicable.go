package platform

import "errors"

// ErrNotApplicable is what a check with no equivalent on this system
// returns (decision 2026-10-28, macOS, M1a): on macOS, the scheduled tasks
// (and so the weekly review), Defender's exclusions, the PowerShell
// profiles, the Python cross, the notification and the display language. It
// is not a failure and not a coverage gap: it does not make the report
// yellow. The macOS report lists what it leaves unchecked, with the signed
// html.na.* texts (decision 2026-10-29, macOS vista previa, point 3); the
// notification only the log says.
var ErrNotApplicable = errors.New("not applicable on this system")
