# make-home.ps1 (BV-157): build a realistic test computer under C:\Users\e2e,
# deterministically (fixed dates, authors and content, no network), for the
# post-publish end-to-end check. It only writes under the home directory it is
# given and the manifest file; it never runs rosmarops.exe and never touches the
# network. run.ps1 then runs the published rosmarops.exe on this computer.
#
# What it mounts under C:\Users\e2e:
#   - the same agents and traps as testdata/sample-home (Claude Code, Cursor,
#     Hermes, Claude Desktop, .mcp.json, shared skills, a project hook, fake
#     secrets, an .env committed to git, the malicious claudeopuseni in a .venv),
#     with the placeholders replaced by fixed fake secrets;
#   - in .cursor\mcp.json a pinned MCP server `npx -y @ifasn/md-doc-mcp@1.0.4`
#     (the engine matches npm by a pinned npx command, so the version is given);
#   - 48 git repositories directly under projects\ (config.ReposUnder counts the
#     immediate subdirectories that hold a .git, so the run finds exactly 48):
#       * shop-api and landing-page, the two sample projects (they carry the
#         agent configs and the .env-in-git), counted as 2 of the 42 "medium";
#       * 40 generated medium repos (100 commits, 200 files each);
#       * 6 generated large repos (3000 commits, 2000 files each), one with a
#         path longer than 180 characters, and a fake key in the history of 3.
#     The history is built with `git fast-import` (one process per repo), which
#     is the only way to make thousands of fixed-date commits in reasonable time.
#   - the fixed fake secrets are written to the manifest so run.ps1 can prove
#     none of them appears in full in any output. The manifest lives outside the
#     home, so it is never scanned.
#
# No kit is created here: `rosmarops kit create` needs the state that `init`
# writes, and `init` refuses to run twice, so the kit is created in run.ps1
# right after its own `init` (reported for BV-157).
#
#   pwsh scripts/e2e/make-home.ps1 -HomeDir C:\Users\e2e -Manifest <path>.json
param(
    [string]$HomeDir = 'C:\Users\e2e',
    [string]$Fixture = (Join-Path $PSScriptRoot '..\..\testdata\sample-home'),
    [string]$Manifest = (Join-Path ([System.IO.Path]::GetTempPath()) 'rosmarops-e2e-manifest.json'),
    [int]$MediumRepos = 40,
    [int]$LargeRepos = 6,
    [int]$MediumCommits = 100,
    [int]$MediumFiles = 200,
    [int]$LargeCommits = 3000,
    [int]$LargeFiles = 2000
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Fixture = (Resolve-Path $Fixture).Path
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $root
try {
    & go run ./tests/journeys/makehome -home $HomeDir -fixture $Fixture -manifest $Manifest `
        -medium $MediumRepos -large $LargeRepos -medium-commits $MediumCommits -medium-files $MediumFiles `
        -large-commits $LargeCommits -large-files $LargeFiles
    if ($LASTEXITCODE -ne 0) { throw "makehome failed ($LASTEXITCODE)" }
} finally {
    Pop-Location
}
