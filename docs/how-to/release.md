# How to release a version

A release is a version tag `vX.Y.Z` on `master`. The tag starts `.github/workflows/release.yml`, which checks, builds, attests and publishes the GitHub release. The Microsoft Store, winget and Homebrew are separate steps after it. The owner decides and starts every release, and the Partner Center upload is the owner's alone.

A tag with a hyphen (`v0.5.0-alpha.1`, `v0.5.0-rc.1`) is published as a prerelease and never as «latest» (`.github/scripts/release-flags.sh`).

## 1. Write the release note

Create `docs/release-notes/vX.Y.Z.md`. The file name is the tag, exactly. The workflow fails without it, and the file becomes the text of the GitHub release. Follow the latest notes in [`docs/release-notes/`](../release-notes/):

1. `# RosmarOps vX.Y.Z`.
2. One short paragraph: what the version does for the person, whether their score may change, and who should update.
3. A list of changes, in plain words, as the person sees them.
4. `*En español, más abajo.*`
5. `## Which file to download`, with every asset and who it is for (copy it from the last note and add any new asset).
6. `---`, then the same note in Spanish. The Spanish part is written by the owner.

Merge the note to `master` with the code it describes. Wait until `ci` (`.github/workflows/ci.yml`) is green on that commit.

## 2. Start the release

Either way works:

- **Push a tag:**

  ```
  git tag -a vX.Y.Z -m "RosmarOps vX.Y.Z"
  git push origin vX.Y.Z
  ```

- **Run workflow:** in GitHub Actions, open `release`, choose «Run workflow» on `master`, and enter the tag (`vX.Y.Z`). The `tag` job checks that it runs on `master`, that the tag has the form `vX.Y.Z` or `vX.Y.Z-prerelease`, that the tag does not exist yet, that `docs/release-notes/vX.Y.Z.md` exists, and that `ci.yml` is green on the commit. It does not create the tag yet: `publish` creates the annotated tag on that same `master` commit, after `verify`, `build` and `journeys` have passed. A run that stops before `publish` (a failed check, a failed build, the journeys gate) leaves no tag, so you can fix the cause and start «Run workflow» again with the same version.

## 3. What `release.yml` does

| Job | Runs on | What it does |
|---|---|---|
| `tag` | manual runs only | The checks above. It does not create the tag. |
| `verify` | Linux | The same checks as CI: `go vet` and `staticcheck` for Linux, Windows and macOS, `gofumpt`, the release flags, `go test -count=1 ./...`. |
| `build` | Linux | Builds `rosmarops.exe` (with the icon and version resource from `go run ./tools/winres -version X.Y.Z`), runs `govulncheck` on it, builds `rosmarops-macos-apple-silicon`, `rosmarops-macos-intel`, `rosmarops-linux-amd64` and `rosmarops-linux-arm64`, runs the network trust tests, writes `SHA256SUMS.txt`, and keeps them as the artifact `release-candidate`. |
| `journeys` | Linux, Windows, macOS | The journey tests (`journeys.yml`) on exactly those binaries: see [journeys](journeys.md). They block `publish` only when the repository variable `JOURNEYS_GATE` is `on`. |
| `publish` | Linux | On a manual run, first creates and pushes the annotated tag on the run's `master` commit (and stops if it already exists). Then checks every file against `build`'s `SHA256SUMS.txt`, creates the build provenance attestation and the GitHub release. |
| `sample-report` | Windows | Checks the released `rosmarops.exe` by its SHA-256, makes the sample report with `scripts/sample-report/generate.ps1`, and adds `sample-report.en.html` and `sample-report.es.html` to the release. |
| `factory-scores` | Linux | Runs `TestR2FactoryScores`, `TestExposurePotential` and `TestProfileScores` in `tests/trust` with their output variables set, and adds `factory-scores.json`, `exposure-potential.json` and `profile-scores.json` to the release. |
| `e2e-realistic` | Windows | Runs the released `rosmarops.exe` on a test computer with 48 projects (`scripts/e2e/make-home.ps1`, `scripts/e2e/run.ps1`). A failure turns the whole run red. |

Until `publish`, every job checks out the run's own commit (`github.sha`): on a manual run, the `master` commit the run started on; on a tag push, the tagged commit. The version comes from the tag's name, never from the tag itself, so nothing before `publish` needs the tag to exist.

Every executable is built with `CGO_ENABLED=0`, `-trimpath` and `-ldflags="-X main.version=X.Y.Z"`. `SHA256SUMS.txt` covers only the executables, not the JSON files or the sample report.

If a job after `publish` fails, the GitHub release already exists. Fix the cause and re-run the failed job within a day (the jobs after `publish` take `rosmarops.exe` from an Actions artifact kept for one day). Do not delete and re-create the tag. If `journeys` fails with the gate on, nothing is published and, on a manual run, no tag exists: fix the cause, then re-run the failed jobs of the run, or start «Run workflow» again with the same version from the new `master`. On a tag push the tag is yours: delete it by hand before you push it again.

## 4. Microsoft Store (MSIX)

`.github/workflows/msix.yml` builds the Store packages. It never touches the GitHub release or winget, and it uploads nothing to Partner Center.

1. In GitHub Actions, open `msix`, choose «Run workflow» on `master`, and enter the version **without** the `v` (`X.Y.Z`). The tag `vX.Y.Z` must exist.
2. The `build` job checks out the tag, reads the Go version from the tag's own `release.yml`, and builds the x64 exe. It must be byte for byte the released `rosmarops.exe` (checked against `SHA256SUMS.txt`); a mismatch fails the run. It also builds the arm64 exe.
3. The `package` job fills `packaging/msix/AppxManifest.xml` with the package version `X.Y.Z.0` and the architecture, validates it, and packs it with the Windows SDK's MakeAppx.
4. The run's artifact `rosmarops-msix-X.Y.Z` holds, per architecture (x64 and arm64):
   - `rosmarops-X.Y.Z-<arch>.msix`: unsigned, for the Store;
   - `rosmarops-X.Y.Z-<arch>-test.msix` and `rosmarops-X.Y.Z-<arch>-test.cer`: signed with a test certificate made in the job, to install by hand before submitting.
5. **The owner** downloads the artifact and uploads the unsigned packages to Partner Center. The Store signs them.

A pull request that changes `packaging/msix/`, `msix.yml` or `tools/winres/` also runs this workflow, as a review build.

## 5. winget

1. Write the manifests for the version, with PowerShell (`pwsh`):

   ```
   pwsh scripts/winget/make-manifests.ps1 -Tag vX.Y.Z
   ```

   It writes four files under `packaging/winget/X.Y.Z/` and reads `InstallerSha256` from the release's `SHA256SUMS.txt` (or from a local copy given with `-Sha256Sums`). Merge them to `master` in a pull request.
2. Optional: run `.github/workflows/winget-check.yml` («Run workflow», version `X.Y.Z`). It validates the manifests and installs from them on a Windows runner. Each run downloads `rosmarops.exe` and adds one to the public download count.
3. Run `.github/workflows/winget-submit.yml` («Run workflow», version `X.Y.Z`). It needs the repository secret `WINGET_TOKEN`, which the owner sets. It downloads a pinned `wingetcreate.exe` and checks its SHA-256, runs `winget validate` (a warning is a failure), checks that the manifest's `InstallerSha256` equals `rosmarops.exe` in `SHA256SUMS.txt`, and opens the pull request to `microsoft/winget-pkgs`. The link is in the run's summary.

## 6. Homebrew

The formula lives in the separate public repository `qtorb/homebrew-rosmarops`. Homebrew builds RosmarOps from the source tarball of the tag, so the formula points to that tarball, not to a release asset.

1. Get the SHA-256 of the tag's source tarball:

   ```
   curl -sL https://github.com/qtorb/rosmarops/archive/refs/tags/vX.Y.Z.tar.gz | sha256sum
   ```

2. In the formula, set `url` to that tarball URL and `sha256` to the value from step 1. Keep the rest as the formula already has it.
3. Commit it to `qtorb/homebrew-rosmarops`.

The program Homebrew builds does not match `SHA256SUMS.txt`, because it is built on the person's computer. That is expected and the README says so.

## 7. Check afterwards

- The `release` run is green, including `sample-report`, `factory-scores` and `e2e-realistic`.
- The release page has `rosmarops.exe`, `rosmarops-macos-apple-silicon`, `rosmarops-macos-intel`, `rosmarops-linux-amd64`, `rosmarops-linux-arm64`, `SHA256SUMS.txt`, `sample-report.en.html`, `sample-report.es.html`, `factory-scores.json`, `exposure-potential.json` and `profile-scores.json`.
- A stable tag is «latest»; a tag with a hyphen is a prerelease.
- The checksum and the attestation verify:

  ```
  sha256sum -c SHA256SUMS.txt --ignore-missing
  gh attestation verify rosmarops.exe --repo qtorb/rosmarops
  ```

- `rosmarops --version` on the downloaded program prints the version `X.Y.Z`.
- The `msix` run shows that x64 reproduces the release exactly.
- The winget pull request in `microsoft/winget-pkgs` is open and its checks pass.
- `brew install qtorb/rosmarops/rosmarops` (or `brew upgrade rosmarops`) installs `X.Y.Z` on macOS or Linux.
- If the Go version in `release.yml` changed, the reproducible build commands in [the trust model](../trust-model.md) («At release») name the new version.

See also [ARCHITECTURE.md](../../ARCHITECTURE.md) and [the network decision](../decisions/0001-network-only-in-spacenet.md): the `build` job runs the network trust tests again before anything is published.
