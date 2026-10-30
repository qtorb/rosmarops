package knowledge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

// MCPCatalogVersion is the version of the embedded MCP catalog (mcp_catalog.json,
// AP-10 Anexo 1, BV-117/BV-121).
const MCPCatalogVersion = "2026-10-30"

//go:embed mcp_catalog.json
var mcpCatalogJSON []byte

// MCP catalog categories (AP-10 Anexo 1, «Gravedad y peso»).
const (
	CategoryMoney          = "money"
	CategoryInfrastructure = "infrastructure"
	CategoryDatabase       = "database"
	CategoryMessaging      = "messaging"
	CategoryEmailDrafts    = "email_drafts"
	CategoryCodeRepo       = "code_repo"
	CategoryCloudDocs      = "cloud_docs"
	CategoryCalendar       = "calendar"
	CategoryIssueTracker   = "issue_tracker"
	CategoryCRM            = "crm"
)

// AllMCPCategories lists every catalog category, in the order of the annex.
var AllMCPCategories = []string{
	CategoryMoney, CategoryInfrastructure, CategoryDatabase, CategoryMessaging,
	CategoryEmailDrafts, CategoryCodeRepo, CategoryCloudDocs, CategoryCalendar,
	CategoryIssueTracker, CategoryCRM,
}

var mcpCategorySet = func() map[string]bool {
	m := make(map[string]bool, len(AllMCPCategories))
	for _, c := range AllMCPCategories {
		m[c] = true
	}
	return m
}()

// MCPCatalogEntry is one known MCP server that acts outside the machine.
// Recognition data (remote_urls, packages, images, url_suffix) and read-only /
// sandbox exclusions are applied by the graph; the category and vendor_note
// drive the rule and its texts.
type MCPCatalogEntry struct {
	ID          string   `json:"id"`
	Service     string   `json:"service"`
	Category    string   `json:"category"`
	Writes      bool     `json:"writes"`
	RemoteURLs  []string `json:"remote_urls,omitempty"`
	Packages    []string `json:"packages,omitempty"`
	Images      []string `json:"images,omitempty"`
	URLSuffix   string   `json:"url_suffix,omitempty"`
	SandboxURLs []string `json:"sandbox_urls,omitempty"`
	VendorNote  bool     `json:"vendor_note,omitempty"`
	DocsURL     string   `json:"docs_url,omitempty"`
	Verified    string   `json:"verified"`
}

// MCPCatalog is the embedded catalog of known MCP servers (BV-117/BV-121).
type MCPCatalog struct {
	Version string            `json:"version"`
	Note    string            `json:"note"`
	Entries []MCPCatalogEntry `json:"entries"`
}

// LoadMCPCatalog decodes the embedded catalog, strictly (an unknown field is an
// error), and checks its version and every entry.
func LoadMCPCatalog() (MCPCatalog, error) {
	dec := json.NewDecoder(bytes.NewReader(mcpCatalogJSON))
	dec.DisallowUnknownFields()
	var c MCPCatalog
	if err := dec.Decode(&c); err != nil {
		return MCPCatalog{}, fmt.Errorf("decode mcp_catalog.json: %w", err)
	}
	if c.Version != MCPCatalogVersion {
		return MCPCatalog{}, fmt.Errorf("mcp_catalog.json version %q, want %q", c.Version, MCPCatalogVersion)
	}
	seen := map[string]bool{}
	for i, e := range c.Entries {
		switch {
		case e.ID == "" || e.Service == "" || e.Verified == "":
			return MCPCatalog{}, fmt.Errorf("mcp catalog entry %d has an empty field", i)
		case !mcpCategorySet[e.Category]:
			return MCPCatalog{}, fmt.Errorf("mcp catalog entry %s: unknown category %q", e.ID, e.Category)
		case len(e.RemoteURLs) == 0 && len(e.Packages) == 0 && len(e.Images) == 0 && e.URLSuffix == "":
			return MCPCatalog{}, fmt.Errorf("mcp catalog entry %s: no way to recognise it", e.ID)
		case seen[e.ID]:
			return MCPCatalog{}, fmt.Errorf("duplicate mcp catalog id %q", e.ID)
		}
		seen[e.ID] = true
	}
	return c, nil
}
