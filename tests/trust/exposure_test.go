package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/exposure"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-208 pt4/pt5 (CTO-26 §1, §4, §5, §6): the six exposure axes and the class,
// computed from the reference computer. Autonomy, reach and opacity come from R2
// with one agent; contagion and delegation from R2-todos; complexity from the
// agents registry.

func overviewByProduct(v report.V2, product string) rules.OverviewAgent {
	for _, o := range v.AgentsOverview {
		if o.Product == product {
			return o
		}
	}
	return rules.OverviewAgent{}
}

// r2ExposureAgents builds the nine agents' exposure potential, in the
// factory-scores order. R2-todos is assembled once; each agent's single-agent R2
// report is built for axes 1, 2 and 5.
func r2ExposureAgents(t *testing.T) []exposure.Agent {
	t.Helper()
	allV, allG := r2AllReport(t)
	out := make([]exposure.Agent, 0, len(r2Cases()))
	for _, c := range r2Cases() {
		single := c.build(t)
		_, band, sid := r2AgentIndexID(t, single, c.product)
		_, singleAssume := r2Detail(single, sid)
		out = append(out, exposure.Compute(exposure.Inputs{
			Product:           c.product,
			Evaluated:         true,
			Single:            overviewByProduct(single, c.product),
			SingleAssumptions: singleAssume,
			All:               overviewByProduct(allV, c.product),
			AllGraph:          allG,
			ClientID:          overviewByProduct(allV, c.product).AgentID,
			FactoryBand:       band,
		}))
	}
	return out
}

// TestExposureClassCoherentWithFactoryBand (CTO-26 §4): every agent's class must
// be coherent with its factory band — contained → A/B, open_gaps → B/C, exposed
// → C/D. A contradiction means the rule or the engine is wrong, never the table:
// it fails CI on all three systems.
func TestExposureClassCoherentWithFactoryBand(t *testing.T) {
	for _, a := range r2ExposureAgents(t) {
		if !a.CoherentWithBand() {
			t.Errorf("%s: class %s is not coherent with factory band %q (allowed %v)",
				a.Product, a.Class, a.FactoryBand, exposure.AllowedClasses(a.FactoryBand))
		}
	}
}
