# 0011. The exposure class A–D

Date: 2026-11-09
Amended: 2026-11-11
Amended: 2026-11-14

## Context

The posture score says how exposed an agent is on this computer, given its findings. It does not say how much risk a product carries by design: how much it does without asking, how far it reaches, whether it can change other agents. A second measure was needed, one that compares products and does not move with every fix.

## Decision

Each evaluated agent gets an exposure class from A (least) to D (most), computed by `internal/exposure`.

**Six axes,** each 0–3, with the input codes that justify the level:
- `autonomy`: how many of four sensitive actions it does without asking (read files on the whole machine, write files, run commands, send out). A sandbox still counts.
- `reach`: how far what it does without asking goes (same project or sandbox, whole machine, any destination).
- `contagion`: whether it loads instructions other agents write, reads another agent's configuration, or can change other agents' instructions or configuration. Only one of the first two signals gives level 1.
- `delegation`: whether a model decides for it in each row, and what can wake it without the person.
- `opacity`: how many assumptions it carries that cannot be seen at rest (capped at 2).
- `complexity`: from the agents registry in `internal/agents`.

**The class** (`classify`, rule `weighted_v1`). Weights are 2 for autonomy, reach and contagion, and 1 for delegation, opacity and complexity. `F = Σ weight × level / Σ weight × 3` over the axes with data. Cuts: `F < 0.25` A, `< 0.50` B, `< 0.75` C, otherwise D. The class is `provisional` while any axis has no data, with a closed `no_data` reason. An agent RosmarOps detects but cannot evaluate gets opacity 3 and no class.

**Coherence with the band.** The class must agree with the factory band of the same review (`AllowedClasses`): `contained` allows A or B, `open_gaps` B or C, `exposed` C or D.

**Where it appears.** The release publishes each product's class at its defaults on the reference computer as `exposure-potential.json`. Since 2026-11-14 each person's report also shows the letter for their own agents, with the 0–100 score next to it as the progress number.

## Consequences

- The JSON carries only English axis names, levels, input codes and the letter; the words come from the catalog.
- A class that falls outside its band's allowed letters fails CI: `tests/trust/exposure_test.go` (`TestExposureClassCoherentWithFactoryBand`) and `tests/trust/bv249_coherence_test.go`.
- `tests/trust/exposure_json_test.go` (`TestExposurePotential`) writes the release file.
- Changing a weight, a cut or an axis rule changes the published classes, so it is a decision for the owner.
