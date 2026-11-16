package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hostenv"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-249 §5: the environment field fits report.v2, and the sheet and the
// Markdown carry it; with no signal nothing appears.
func TestEnvironmentInReport(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	en, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range []*hostenv.Env{
		nil,
		{Kind: hostenv.KindContainer, Signal: hostenv.SignalDockerenv, Engine: hostenv.EngineDocker, HostMounts: []string{"/workspace"}, DockerSocket: true},
		{Kind: hostenv.KindWSL, Signal: hostenv.SignalProcVersion, Drives: []string{"C:"}},
		{Kind: hostenv.KindVirtualMachine, Signal: hostenv.SignalHVVMMPresent, VMType: hostenv.Unidentified},
		{Kind: hostenv.KindWindowsSandbox, Signal: hostenv.SignalWDAGAccount},
	} {
		v := base
		v.Environment = env
		raw, err := report.RenderV2JSON(v)
		if err != nil {
			t.Fatalf("%+v: %v", env, err)
		}
		if strings.Contains(string(raw), `"environment"`) != (env != nil) {
			t.Errorf("%+v: environment in JSON %v", env, env != nil)
		}
		page := report.RenderHTML(v, en, "/home/a", nil)
		md := report.RenderV2Markdown(v, en, nil)
		tag := env.TestEnvironment()
		for _, out := range []string{page, md} {
			if strings.Contains(out, "Review of a test environment") != tag {
				t.Errorf("%+v: tag %v", env, tag)
			}
			if env != nil && !strings.Contains(out, "This review ran") {
				t.Errorf("%+v: no environment line", env)
			}
		}
	}
}
