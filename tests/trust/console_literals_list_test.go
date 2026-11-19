package trust_test

// consoleLiterals are the texts that reach the console as a literal in the
// code today rather than from the catalog, by file. A new one fails
// TestNoConsoleTextOutsideCatalog; one that is gone must leave the list
// (TestConsoleLiteralsListIsCurrent), so the list only shrinks.
var consoleLiterals = map[string][]string{
	// baseline: an advanced command whose console report has always been English, outside the catalog.
	"cmd/rosmarops/cmd_baseline.go": {
		"  %-8s  WARNING: could not list extensions: %v\n",
		"vscode",
		"  %-8s  %d extension(s) baselined\n",
		"  %-8s  WARNING: %d repo(s) had scan errors (see log)\n",
		"repos",
		"  %-8s  %d repo(s) baselined\n",
		"  %-16s  WARNING: could not list shell profiles: %v\n",
		"shellprofile",
		"  %-16s  %d profile scope(s) hashed\n",
		"  %-16s  WARNING: could not list scheduled tasks: %v\n",
		"scheduledtasks",
		"  %-16s  %d task(s) baselined\n",
		"  %-16s  WARNING: could not list Defender exclusions: %v\n",
		"avexclusions",
		"  %-16s  %d exclusion(s) baselined\n",
		"  %-16s  %d global package(s) baselined\n",
		"globals",
		"\nBaseline updated. Run 'rosmarops run' to scan.",
		"  %-8s  baseline established  %s…\n",
		"  %-8s  unchanged             %s…\n",
		"  %-8s  CHANGED               %s… → %s…\n",
	},
	// init: the install command's English lines, outside the catalog.
	"cmd/rosmarops/cmd_init.go": {
		"warning: %v; repo_roots left empty\n",
		"Repositories found under %s: %d\n",
		"RosmarOps initialized.\n\n",
		"  Config: %s\n",
		"  State:  %s\n\n",
		"Next step:\n",
		"  Run 'rosmarops run' to scan this computer.\n",
		"  Results are written to %s.\n",
		"error: config already exists at %s\n",
		"error: state already exists at %s\n",
		"hint: use 'rosmarops baseline' to rehash after intentional changes",
	},
	// status: its English table of paths, versions and fingerprints, outside the catalog.
	"cmd/rosmarops/cmd_status.go": {
		"RosmarOps %s\n\n",
		"  Config  %s\n",
		"  State   %s\n\n",
		"  Config error: %v\n\n",
		"  State error: %v\n\n",
		"  Config schema version : %d\n",
		"  State schema version  : %d\n",
		"  Last run              : never\n",
		"  Last run              : %s\n",
		"  Last run              : %s (%s)\n",
		"  Consecutive green     : %d\n",
		"  Binary baseline       : not set (run 'rosmarops baseline')\n",
		"  Binary baseline       : %s…\n",
		"  Config baseline       : not set (run 'rosmarops baseline')\n",
		"  Config baseline       : %s…\n",
	},
	// the technical prefix of an error.
	"cmd/rosmarops/firstrun.go": {
		"error:",
	},
	// the technical prefix of an error, the fixed markers of an integrity stop around its catalog texts, and the stub of a command not built.
	"cmd/rosmarops/main.go": {
		"error:",
		"INTEGRITY FAILURE — see report:",
		"\n🔴 ABORT: %s\n%s\n",
		"not implemented yet (target: %s)\n",
	},
}
