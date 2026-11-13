# check-release-exe.ps1 (BV-230): the rosmarops.exe a post-release job got
# from this run's release job (an Actions artifact) must be the published one.
# Its SHA-256 must equal the rosmarops.exe line of the release's SHA256SUMS.txt
# and the digest the releases API gives for the published rosmarops.exe asset.
# Any difference, or a missing line or digest, fails the job.
#
# The only release asset it downloads is SHA256SUMS.txt: downloading the
# binary would add to the public download counter.
#
#   ./.github/scripts/check-release-exe.ps1 -Tag v0.11.6 -Exe dist/rosmarops.exe
param(
    [Parameter(Mandatory = $true)][string]$Tag,
    [Parameter(Mandatory = $true)][string]$Exe,
    [string]$Repo = $env:GITHUB_REPOSITORY
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not (Test-Path -LiteralPath $Exe -PathType Leaf)) { throw "no $Exe from the release job" }
$got = (Get-FileHash -Algorithm SHA256 -LiteralPath $Exe).Hash.ToLowerInvariant()

$sums = Join-Path ([System.IO.Path]::GetTempPath()) ("sha256sums-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $sums | Out-Null
gh release download $Tag --repo $Repo --pattern SHA256SUMS.txt --dir $sums
if ($LASTEXITCODE -ne 0) { throw "could not download SHA256SUMS.txt of $Tag" }
$line = Select-String -LiteralPath (Join-Path $sums 'SHA256SUMS.txt') -Pattern '^([0-9a-f]{64})\s+\*?rosmarops\.exe$'
if (-not $line) { throw "SHA256SUMS.txt of $Tag has no rosmarops.exe line" }
$want = $line.Matches[0].Groups[1].Value

$digest = gh api "repos/$Repo/releases/tags/$Tag" --jq '.assets[] | select(.name == "rosmarops.exe") | .digest'
if ($LASTEXITCODE -ne 0) { throw "could not read the release $Tag" }
$digest = "$digest".Trim()
if ($digest -notmatch '^sha256:[0-9a-f]{64}$') { throw "the published rosmarops.exe of $Tag has no SHA-256 digest ('$digest')" }
$published = $digest.Substring(7)

if ($got -ne $want) { throw "rosmarops.exe SHA-256 $got does not match SHA256SUMS.txt ($want)" }
if ($got -ne $published) { throw "rosmarops.exe SHA-256 $got does not match the published asset's digest ($published)" }
Write-Host "rosmarops.exe $got = SHA256SUMS.txt = published asset digest"
