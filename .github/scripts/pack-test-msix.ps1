# pack-test-msix.ps1: one x64 MSIX test package of a given rosmarops.exe,
# signed with a self-signed certificate made here, and that certificate's
# public .cer, for the journey tests (journeys.yml) to install with
# Add-AppxPackage. The same manifest, icons and publisher as msix.yml's test
# packages; never a Store package (that is msix.yml's). Windows PowerShell 5.1
# (System.Drawing) on a runner with the Windows SDK.
#
#   powershell -File .github/scripts/pack-test-msix.ps1 -Exe rosmarops.exe -Version 0.15.1 -Out dir
param(
    [Parameter(Mandatory)] [string]$Exe,
    [Parameter(Mandatory)] [string]$Version,  # X.Y.Z; the package version is X.Y.Z.0
    [Parameter(Mandatory)] [string]$Out
)
$ErrorActionPreference = 'Stop'
$root = 'C:\Program Files (x86)\Windows Kits\10\bin'
$makeappx = Get-ChildItem -Path $root -Recurse -Filter makeappx.exe | Where-Object { $_.FullName -match '\\x64\\makeappx.exe$' } |
    Sort-Object FullName -Descending | Select-Object -First 1
$signtool = Get-ChildItem -Path $root -Recurse -Filter signtool.exe | Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } |
    Sort-Object FullName -Descending | Select-Object -First 1
if (-not $makeappx -or -not $signtool) { throw "MakeAppx or SignTool not found under $root" }
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path

$publisher = 'CN=E6864926-2E32-484E-9FDB-FE93F4BF4836'
$cert = New-SelfSignedCertificate -Type Custom -Subject $publisher -KeyUsage DigitalSignature -FriendlyName 'RosmarOps MSIX journey test' `
    -CertStoreLocation 'Cert:\CurrentUser\My' -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3', '2.5.29.19={text}')
$secret = ConvertTo-SecureString -String ([guid]::NewGuid().ToString()) -Force -AsPlainText
$pfx = Join-Path $env:RUNNER_TEMP "journey-$Version.pfx"
Export-PfxCertificate -Cert "Cert:\CurrentUser\My\$($cert.Thumbprint)" -FilePath $pfx -Password $secret | Out-Null

$stage = Join-Path $env:RUNNER_TEMP "journey-pkg-$Version"
New-Item -ItemType Directory -Force -Path (Join-Path $stage 'Assets') | Out-Null
Copy-Item $Exe (Join-Path $stage 'rosmarops.exe')
Add-Type -AssemblyName System.Drawing
$ico = [System.Drawing.Icon]::new((Join-Path $repo 'assets\windows\rosmarops.ico'), 256, 256)
$src = $ico.ToBitmap()
foreach ($a in @(@{n = 'Square44x44Logo.png'; s = 44 }, @{n = 'Square150x150Logo.png'; s = 150 }, @{n = 'StoreLogo.png'; s = 50 })) {
    $bmp = New-Object System.Drawing.Bitmap($a.s, $a.s)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.DrawImage($src, 0, 0, $a.s, $a.s)
    $bmp.Save((Join-Path $stage "Assets\$($a.n)"), [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
}
$src.Dispose(); $ico.Dispose()
(Get-Content (Join-Path $repo 'packaging\msix\AppxManifest.xml') -Raw).Replace('@VERSION@', "$Version.0").Replace('@ARCH@', 'x64') |
    Set-Content (Join-Path $stage 'AppxManifest.xml') -NoNewline

$msix = Join-Path $Out "rosmarops-$Version-x64-test.msix"
& $makeappx.FullName pack /o /d $stage /p $msix | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'makeappx failed' }
& $signtool.FullName sign /fd SHA256 /a /f $pfx /p ([System.Net.NetworkCredential]::new('', $secret).Password) $msix | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'signtool failed' }
Export-Certificate -Cert "Cert:\CurrentUser\My\$($cert.Thumbprint)" -FilePath (Join-Path $Out "rosmarops-$Version-x64-test.cer") | Out-Null
Remove-Item $pfx
Write-Output "packed $msix"
