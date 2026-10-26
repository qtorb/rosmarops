// Package proc prepares the child processes RosmarOps runs (PowerShell, git,
// VS Code's CLI) so that none opens a window: a scheduled review runs with
// no console (decision 2026-10-26, integridad y revisión semanal, J3), and a
// console program started from it would otherwise get a console of its own.
package proc
