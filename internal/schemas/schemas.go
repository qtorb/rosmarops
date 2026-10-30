// Package schemas embeds the canonical JSON Schema files (the v1 config and
// report schemas, the v1 and v2 state schemas, the v2 graph and report
// schemas under v2/, and the v1 kit schema) into the binary.
// These are the source of truth for validation at runtime; the copies under
// /schemas/ at the repo root are the human-readable reference.
//
// When the schema files under /schemas/ are updated (which requires a new
// schema version per project policy), the copies here must be updated too.
package schemas

import _ "embed"

// ConfigSchema is the embedded config.v1.json schema.
//
//go:embed config.v1.json
var ConfigSchema []byte

// StateSchema is the embedded state.v1.json schema. It is kept to validate
// v1 state files before migrating them to v2.
//
//go:embed state.v1.json
var StateSchema []byte

// StateV2Schema is the embedded state.v2.json schema.
//
//go:embed state.v2.json
var StateV2Schema []byte

// ReportSchema is the embedded report.v1.json schema.
//
//go:embed report.v1.json
var ReportSchema []byte

// GraphV2Schema is the embedded v2/graph.v2.schema.json schema.
//
//go:embed v2/graph.v2.schema.json
var GraphV2Schema []byte

// ReportV2Schema is the embedded v2/report.v2.schema.json schema.
//
//go:embed v2/report.v2.schema.json
var ReportV2Schema []byte

// IndexHistorySchema is the embedded v2/index-history.v1.schema.json schema.
//
//go:embed v2/index-history.v1.schema.json
var IndexHistorySchema []byte

// FindingsMemorySchema is the embedded v2/findings-memory.v1.schema.json
// schema. It is kept to read v1 memories.
//
//go:embed v2/findings-memory.v1.schema.json
var FindingsMemorySchema []byte

// FindingsMemoryV2Schema is the embedded v2/findings-memory.v2.schema.json
// schema (decision 2026-10-26, agentes y MCP nuevos, A2).
//
//go:embed v2/findings-memory.v2.schema.json
var FindingsMemoryV2Schema []byte

// FindingsMemoryV3Schema is the embedded v2/findings-memory.v3.schema.json
// schema (BV-91, D3): adds last_checked.mcp_dest_refs.
//
//go:embed v2/findings-memory.v3.schema.json
var FindingsMemoryV3Schema []byte

// IntelSchema is the embedded intel/intel.v1.schema.json schema: the
// threat-data snapshot contract (BV-100, F0a).
//
//go:embed intel/intel.v1.schema.json
var IntelSchema []byte

// KitSchema is the embedded kit.v1.schema.json schema: the rosmarops-kit
// file (BV-118, CTO-10 point 5).
//
//go:embed kit.v1.schema.json
var KitSchema []byte

// ResultsSchema is the embedded results.v1.schema.json schema: the closed
// package rosmarops-results v1 (BV-119, AP-17 §2).
//
//go:embed results.v1.schema.json
var ResultsSchema []byte

// SpaceViewSchema is the embedded v2/space-view.v1.schema.json schema: the
// content of a rosmarops-results v1 package (BV-119/BV-122, copied unchanged
// from vigil).
//
//go:embed v2/space-view.v1.schema.json
var SpaceViewSchema []byte
