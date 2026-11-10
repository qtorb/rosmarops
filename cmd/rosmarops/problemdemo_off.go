//go:build !demoproblem

package main

import "time"

// The release binary (built without the `demoproblem` tag) never reads
// ROSMAROPS_DEMO_PROBLEM: these are no-ops (CTO-32 C5). The test candidate
// builds problemdemo.go instead.

func seedDemoProblemB(_ string, _ time.Time) {}

func seedDemoProblemA(_ string, _ time.Time) {}
