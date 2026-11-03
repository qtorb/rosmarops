# make-manifests.ps1 (BV-162): write the winget manifests for one released
# version of RosmarOps under packaging/winget/<X.Y.Z>/. Four files, all with
# the same ManifestVersion:
#   RosmarOps.RosmarOps.yaml                 (version)
#   RosmarOps.RosmarOps.installer.yaml       (installer, portable x64)
#   RosmarOps.RosmarOps.locale.en-US.yaml    (defaultLocale)
#   RosmarOps.RosmarOps.locale.es-ES.yaml    (locale)
#
# The InstallerSha256 is READ from the release's SHA256SUMS.txt, never
# computed here: pass -Sha256Sums with a local copy, or let the script
# download it from the release for the tag. RosmarOps itself sends nothing;
# this is a packaging helper that runs on a maintainer's machine or in CI.
#
#   pwsh scripts/winget/make-manifests.ps1 -Tag v0.7.4
#   pwsh scripts/winget/make-manifests.ps1 -Tag v0.7.4 -Sha256Sums dist/SHA256SUMS.txt
#
# ManifestVersion 1.12.0 is the most recent schema microsoft/winget-pkgs
# accepts today (BV-162, 03-11; e.g. Microsoft.PowerToys 0.99.1). To move to a
# newer one, bump $ManifestVersion after checking it is live in that repo.
param(
    [Parameter(Mandatory = $true)][string]$Tag,
    [string]$Sha256Sums,
    [string]$OutRoot = (Join-Path $PSScriptRoot '..\..\packaging\winget'),
    [string]$ManifestVersion = '1.12.0'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Tag -notmatch '^v([0-9]+)\.([0-9]+)\.([0-9]+)$') {
    throw "tag must look like vX.Y.Z, got '$Tag'"
}
$version = $Tag.Substring(1)

# The InstallerSha256 comes from SHA256SUMS.txt, the same file the release
# publishes and e2e-realistic checks; never a hash computed here by hand.
if ($Sha256Sums) {
    $sumsText = [System.IO.File]::ReadAllText((Resolve-Path $Sha256Sums).Path)
} else {
    $sumsUrl = "https://github.com/qtorb/rosmarops/releases/download/$Tag/SHA256SUMS.txt"
    Write-Output "Reading SHA256SUMS.txt from $sumsUrl"
    $sumsText = (Invoke-WebRequest -Uri $sumsUrl -UseBasicParsing).Content
}
$m = [regex]::Match($sumsText, '(?m)^([0-9a-f]{64})\s+\*?rosmarops\.exe$')
if (-not $m.Success) { throw "no rosmarops.exe line in SHA256SUMS.txt" }
$sha = $m.Groups[1].Value.ToUpperInvariant()

$installerUrl = "https://github.com/qtorb/rosmarops/releases/download/$Tag/rosmarops.exe"
$releaseNotes = "https://github.com/qtorb/rosmarops/releases/tag/$Tag"

$outDir = Join-Path $OutRoot $version
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# UTF-8 without BOM, LF line endings: what winget-pkgs expects.
$utf8 = [System.Text.UTF8Encoding]::new($false)
function Write-Manifest([string]$name, [string]$body) {
    $path = Join-Path $outDir $name
    [System.IO.File]::WriteAllText($path, ($body -replace "`r`n", "`n"), $utf8)
    Write-Output "wrote $path"
}

$versionYaml = @"
# yaml-language-server: `$schema=https://aka.ms/winget-manifest.version.$ManifestVersion.schema.json

PackageIdentifier: RosmarOps.RosmarOps
PackageVersion: $version
DefaultLocale: en-US
ManifestType: version
ManifestVersion: $ManifestVersion
"@

$installerYaml = @"
# yaml-language-server: `$schema=https://aka.ms/winget-manifest.installer.$ManifestVersion.schema.json

PackageIdentifier: RosmarOps.RosmarOps
PackageVersion: $version
InstallerType: portable
Commands:
- rosmarops
Installers:
- Architecture: x64
  InstallerUrl: $installerUrl
  InstallerSha256: $sha
ManifestType: installer
ManifestVersion: $ManifestVersion
"@

$enUsYaml = @"
# yaml-language-server: `$schema=https://aka.ms/winget-manifest.defaultLocale.$ManifestVersion.schema.json

PackageIdentifier: RosmarOps.RosmarOps
PackageVersion: $version
PackageLocale: en-US
Publisher: RosmarOps
PublisherUrl: https://rosmarops.com
PackageName: RosmarOps
PackageUrl: https://github.com/qtorb/rosmarops
License: AGPL-3.0
LicenseUrl: https://github.com/qtorb/rosmarops/blob/master/LICENSE
ShortDescription: Local, read-only review of what the AI agents on your computer can do.
Description: RosmarOps reads the configuration of the AI agents on this computer, such as Claude Code, Cursor and Hermes Agent, works out what each one can do without asking you and which keys it can reach, and writes a report you open in your browser. It runs on your computer and sends nothing unless you connect it to your space.
Moniker: rosmarops
Tags:
- security
- ai-agents
- mcp
- audit
- claude-code
- cursor
ReleaseNotesUrl: $releaseNotes
ManifestType: defaultLocale
ManifestVersion: $ManifestVersion
"@

$esEsYaml = @"
# yaml-language-server: `$schema=https://aka.ms/winget-manifest.locale.$ManifestVersion.schema.json

PackageIdentifier: RosmarOps.RosmarOps
PackageVersion: $version
PackageLocale: es-ES
Publisher: RosmarOps
PublisherUrl: https://rosmarops.com
PackageName: RosmarOps
ShortDescription: Revisión local, de solo lectura, de lo que pueden hacer los agentes de IA de tu ordenador.
Description: RosmarOps lee la configuración de los agentes de IA de este ordenador, como Claude Code, Cursor y Hermes Agent, calcula qué puede hacer cada uno sin preguntarte y a qué claves llega, y escribe un informe que abres en tu navegador. Funciona en tu ordenador y no envía nada salvo que lo conectes a tu espacio.
ManifestType: locale
ManifestVersion: $ManifestVersion
"@

Write-Manifest 'RosmarOps.RosmarOps.yaml' $versionYaml
Write-Manifest 'RosmarOps.RosmarOps.installer.yaml' $installerYaml
Write-Manifest 'RosmarOps.RosmarOps.locale.en-US.yaml' $enUsYaml
Write-Manifest 'RosmarOps.RosmarOps.locale.es-ES.yaml' $esEsYaml
Write-Output "winget manifests for $Tag written to $outDir (ManifestVersion $ManifestVersion)"
