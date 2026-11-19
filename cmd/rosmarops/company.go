package main

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/company"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/report"
)

// The configuration a review uses: the person's config.json, or, when they
// have none, the company configuration of a managed computer.
const (
	configSourceUser    = "user"
	configSourceCompany = "company"
)

// How this review accepted its binary, as the summary and report.v2's company
// object say it.
const (
	binaryUnchanged = "unchanged"
	binaryFirstRun  = "first_run"
)

// The scan-error scopes of a company configuration that is ignored: the
// review is at least yellow, and «Your company» says why.
const (
	scopeCompanyUntrusted = "company_config_untrusted"
	scopeCompanyInvalid   = "company_config_invalid"
)

// companyDir is the company folder; tests replace it.
var companyDir = company.Dir

// companyRun is what a review knows about the company configuration.
type companyRun struct {
	loaded company.Loaded
	// source is configSourceUser or configSourceCompany.
	source string
	// summaryDir is the default reports data folder, where the summary is
	// written whenever a trusted configuration turns it on.
	summaryDir string
	// overridden: the person's reports folder was ignored for the usual one.
	overridden bool
	// changed: the company configuration in use changed since the last review.
	changed bool
	// binary is how this review accepted its binary (binaryUnchanged,
	// binaryFirstRun, or a state.binary_accepted_via value).
	binary string
	// adminOld and adminNew are an update the administrator's list accepted.
	adminOld, adminNew string
	// noSummary: a command that is not a review (status, findings…) never
	// writes the company summary, not even when it stops.
	noSummary bool
}

// inUse reports that this review uses the company configuration: it is
// trusted and the person has no config.json of their own. An untrusted or
// invalid file never gets here (resolveRunConfig leaves the source as user).
func (c *companyRun) inUse() bool { return c != nil && c.source == configSourceCompany }

// summaryOn reports a trusted company configuration that turns the summary on.
func (c *companyRun) summaryOn() bool { return c != nil && !c.noSummary && c.loaded.SummaryEnabled() }

// resolveRunConfig is the configuration of a review: the person's
// config.json at cfgPath when it exists; else, with a trusted company
// configuration, the defaults with its first-run project folders, never
// written to disk, and cfgPath becomes company.json's path, which the
// integrity check then fingerprints. Without either, loading cfgPath fails
// as it always did.
func resolveRunConfig(cfgPath, home string, comp *companyRun) (config.Config, string, error) {
	if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) && comp.loaded.State == company.Trusted {
		roots, rootsErr := company.ResolveRoots(home, comp.loaded.RepoRoots())
		if rootsErr == nil {
			cfg := config.DefaultConfig()
			cfg.RepoRoots = roots
			comp.source = configSourceCompany
			return cfg, comp.loaded.Path, nil
		}
		// A project folder outside the user's folder: the file is not accepted.
		slog.Warn("company configuration: repo root refused", "err", rootsErr)
		comp.loaded = company.Loaded{State: company.Invalid, Path: comp.loaded.Path}
	}
	cfg, err := config.LoadConfig(cfgPath)
	return cfg, cfgPath, err
}

// loadCompany reads the company configuration and removes a previous summary
// when this review writes none, so the company never reads an old result.
func loadCompany(defaultReportDir string) *companyRun {
	c := &companyRun{loaded: company.LoadConfig(companyDir()), source: configSourceUser, summaryDir: report.DataDir(defaultReportDir), binary: binaryUnchanged}
	if !c.summaryOn() {
		if err := report.RemoveSummary(c.summaryDir); err != nil {
			slog.Warn("company summary: could not remove the previous one", "err", err)
		}
	}
	return c
}

// adminListed reports whether the administrator's fingerprint list, trusted,
// holds self with this binary's version.
func adminListed(self string) bool {
	list, ok := company.LoadList(companyDir())
	return ok && company.Lists(list, self, version)
}

// scanErrors are the scan errors of an ignored company configuration.
func (c *companyRun) scanErrors() []finding.ScanError {
	switch c.loaded.State {
	case company.Untrusted:
		return []finding.ScanError{{Scope: scopeCompanyUntrusted, Message: "company configuration not owned and writable only by administrators: ignored"}}
	case company.Invalid:
		return []finding.ScanError{{Scope: scopeCompanyInvalid, Message: "company configuration not valid against company.v1: ignored"}}
	}
	return nil
}

// reportObject is report.v2's company object; nil without a company
// configuration.
func (c *companyRun) reportObject() *report.Company {
	if c == nil || c.loaded.State == company.Absent {
		return nil
	}
	return &report.Company{
		State: c.loaded.State, SHA256: c.loaded.SHA256, Changed: c.changed,
		SummaryEnabled: c.summaryOn(), ReportDirOverridden: c.overridden,
		ConfigSource: c.source, BinaryAcceptedVia: c.binary,
	}
}

// applyToReport puts the company object and the summary the company reads
// into v, and writes the summary when it is on.
func (c *companyRun) applyToReport(v *report.V2, g *graph.Graph, notChecked []string) {
	v.Company, v.CompanyPath = c.reportObject(), c.loaded.Path
	if !c.summaryOn() {
		return
	}
	s := report.SummaryOf(*v, g, version, notChecked, c.source, c.binary, time.Now())
	if _, err := report.WriteSummary(c.summaryDir, s); err != nil {
		slog.Error("company summary: not written", "err", err)
		return
	}
	v.CompanyRead = &s
}

// writeAbortSummary writes the summary of a review that stopped, when the
// company reads one: run is integrity_abort or failed.
func (c *companyRun) writeAbortSummary(run, code string) {
	if !c.summaryOn() {
		return
	}
	if _, err := report.WriteSummary(c.summaryDir, report.AbortSummary(run, code, version, c.source, time.Now())); err != nil {
		slog.Error("company summary: not written", "err", err)
	}
}

// reportDirFor is the reports folder of a review: with a trusted company
// configuration always the usual one, so the company finds the summary at a
// fixed path; the report then says the person's folder was ignored.
func (c *companyRun) reportDirFor(configured, defaultDir string) string {
	if configured == "" {
		return defaultDir
	}
	if c.loaded.State == company.Trusted && filepath.Clean(configured) != filepath.Clean(defaultDir) {
		c.overridden = true
		return defaultDir
	}
	return configured
}
