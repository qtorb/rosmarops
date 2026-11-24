# How to change a sandbox template

A template is a text the report gives the person to paste into an agent: a settings block or a launch command. RosmarOps measures most templates before the person applies them, and publishes their scores with each release in `profile-scores.json`. This guide changes a template and recomputes what depends on it.

Read [the factory scores and sandbox templates decision](../decisions/0012-factory-scores-and-sandbox-templates.md), [the posture score decision](../decisions/0010-posture-score.md) and [the exposure class decision](../decisions/0011-exposure-class.md) first.

## Before you start

The templates are signed copy, and their scores are published figures. Agree the change and the new figures with the owner before you merge.

## The templates

The ids are `P0` to `P3`, as the report names them. Their names are catalog texts (`cc.name.P0` and so on: «Read only», «Local development», «Sandboxed development», «Unattended»).

| Agent | Template | Where the text is | Measured? |
|---|---|---|---|
| Claude Code | P0, P1 (block for `~/.claude/settings.json`) | `p0JSON`, `p1JSON` in `internal/profiles/profiles.go` | yes, on every system |
| Claude Code | P2 (user block only: it has had no project block since BV-254) | `p2JSON` in `internal/profiles/profiles.go` | yes, on macOS and Linux only (`MeasuredOn`) |
| Claude Code | P3 (launch command) | `p3Command` in `internal/profiles/profiles.go` | no |
| Grok Build | P0, P1, P3 | `grokP0`, `grokP1`, `grokP3` in `internal/profiles/guided.go` | no (guided) |
| Grok Build | P2 (P1 with the `strict` sandbox) | `grokP2` in `internal/profiles/guided.go` | yes, on macOS and Linux |
| Cursor | P0, P1 (`~/.cursor/sandbox.json`) | `cursorBlock` in `internal/profiles/guided.go` | yes, as declared answers: `DeclaredProfiles` in `internal/pipeline/profile.go` |
| Grok Bot | «Only the cloud», «With your computer» | `DeclaredProfiles` in `internal/pipeline/profile.go` | yes, as declared answers |
| Claude Code, Grok Build | «Try it just this once» session commands | `internal/profiles/session.go` | no |

How a measured template is measured:

- Claude Code: `profiles.Apply` adds the block to the person's Claude Code user settings, the way Claude Code merges a pasted block. `pipeline.Profile` runs the whole graph again with it.
- Grok Build P2: `profiles.GrokApply` adds the block to the user `config.toml`. Its rules are written twice: in the TOML text and in `grokP1Rules`. They must match.
- Cursor and Grok Bot: `pipeline.DeclaredProfile` replaces the person's answers with the template's answers ([declared answers](../decisions/0017-declared-answers.md)).

## Steps

### 1. Change the text

Edit the constant in `internal/profiles/profiles.go` or `internal/profiles/guided.go`. Keep it valid JSON or TOML: the person pastes it as it is.

### 2. Make sure the engine measures what the text says

For Claude Code, `Permissions` decodes the block with the `block` and `sandboxBlock` structs in `internal/profiles/profiles.go`, and `Apply` puts it on the settings scan. A key those structs do not decode is copied to the person but **not measured**. If you add such a key and it matters for the score, decode it and apply it.

For Grok Build P2, update `grokP1Rules` or `GrokApply` to match the TOML. `TestGrokP2BlockIsWhatIsMeasured` (`adapters/windows/grok_profile_test.go`) reads the TOML with the real reader and compares it with what `GrokApply` measures.

For Cursor or Grok Bot, change the answers in `DeclaredProfiles`, and the text in `cursorBlock` if it changes.

### 3. Update the hand-written copy in the integration tests

`tests/integration/` keeps its own copy of the Claude Code P0 and P1 permissions (and the Grok Build P0 and P1 rules), and a test checks that the blocks the report copies are the ones it measured. Search for it with `grep -rn 'Bash(npm run lint)' tests/integration/` and make the same change.

### 4. Recompute the figures

Run the template tests verbosely. Each one logs the figures it measured:

```
go test -count=1 -v -run 'TestProfileScores' ./tests/trust/
```

`TestProfileScores` logs one line per template and system: the product, the template, the system, the score, the band, the class, and the score of the reference computer with every agent. Its pins are in `profilePins`, in the same package: `product/template/system` to score and class. A template without a pin is written as measured.

Other tests pin template figures too. Run them all and read each failure:

```
go test -count=1 -v -run 'Profile|P2|Coherence' ./tests/trust/ ./tests/integration/
```

Give the new figures to the owner. When they sign them, update the pins in those tests to the signed values. Do not change a pin to whatever the code now gives without that sign-off.

### 5. Check coherence

`TestCoherenceSister` (`tests/trust/`) measures every measured template on every system, plus Cursor's and Grok Bot's answer grids and Claude Code's modes, and checks that each class is one the band allows (`exposure.AllowedClasses`). There is no tolerance and no exception list. `TestExposureClassCoherentWithFactoryBand` checks the same for the factory scores.

`TestCoherenceSister` is long and skips under `-short`; run it without `-short`. If it fails, the template's score and class disagree. Do not loosen the test; report the case to the owner.

### 6. Check `profile-scores.json`

`profile-scores.json` is not in the repository. The release workflow (`.github/workflows/release.yml`, job `factory-scores`) writes it by running `TestProfileScores` with `ROSMAROPS_PROFILE_SCORES_OUT` set, and uploads it to the release. The test writes the file only when every pin matches, and validates it against `schemas/release/profile-scores.v1.schema.json`.

To see the file a release would publish:

```
ROSMAROPS_PROFILE_SCORES_OUT="$PWD/profile-scores.json" go test -count=1 -run '^TestProfileScores$' ./tests/trust/
```

The path must be absolute. Do not commit the file. A new field in it is a schema change: ask the owner.

### 7. Update the golden pages and the texts

The one-agent golden pages in `internal/report/testdata/` contain the Claude Code blocks. `TestRenderHTML_OneAgentUnchanged` fails until you update them; see «Golden pages» in [change-a-text.md](change-a-text.md). If a sentence about the template changes (the `cc.*` keys), follow the same guide.

### 8. Say it in the release note

A changed template changes a published score. Say so in the next release note ([release.md](release.md)).

## Tests that must pass

- `go test -count=1 ./internal/profiles/`
- `go test -count=1 -run 'TestGrokP2BlockIsWhatIsMeasured' ./adapters/windows/`
- `go test -count=1 -run 'Profile|P2|Coherence' ./tests/trust/ ./tests/integration/`
- `go test -count=1 -run 'TestRenderHTML_OneAgentUnchanged' ./internal/report/`

And what CI runs:

```
go test -count=1 ./...
go vet ./...
staticcheck ./...
gofumpt -l .
```
