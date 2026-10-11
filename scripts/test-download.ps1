param([string]$Bundle = (Join-Path $PSScriptRoot '../output/bundles/hostops-windows-amd64.zip'))
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$bundlePath = (Resolve-Path -LiteralPath $Bundle).Path
$testRoot = Join-Path $projectRoot ('local/download-smoke-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot | Out-Null
$assetName = Split-Path -Leaf $bundlePath
$digest = (Get-FileHash -LiteralPath $bundlePath -Algorithm SHA256).Hash.ToLowerInvariant()
$checksumPath = Join-Path $testRoot 'SHA256SUMS'
[IO.File]::WriteAllText($checksumPath, "$digest  $assetName`n")
$testRelease = @{
    tag_name = 'v1.2.3'; draft = $false; prerelease = $false
    assets = @(
        @{name=$assetName; browser_download_url="https://github.com/Shumair99/host-ops/releases/download/v1.2.3/$assetName"},
        @{name='SHA256SUMS'; browser_download_url='https://github.com/Shumair99/host-ops/releases/download/v1.2.3/SHA256SUMS'}
    )
}
# Mock only the download boundary. Extraction, hashing and installation are real.
function Invoke-RestMethod { param($Uri, $Headers) if ($Uri -cne 'https://api.github.com/repos/Shumair99/host-ops/releases/latest') { throw 'Unexpected request' }; return $testRelease }
function Invoke-WebRequest {
    param($Uri, $OutFile, [switch]$UseBasicParsing)
    if ($Uri -ceq "https://github.com/Shumair99/host-ops/releases/download/v1.2.3/$assetName") { Copy-Item -LiteralPath $bundlePath -Destination $OutFile }
    elseif ($Uri -ceq 'https://github.com/Shumair99/host-ops/releases/download/v1.2.3/SHA256SUMS') { Copy-Item -LiteralPath $checksumPath -Destination $OutFile }
    else { throw 'Unexpected asset request' }
}
$savedPath = $env:Path
try {
    $destination = Join-Path $testRoot 'installed'
    & (Join-Path $PSScriptRoot 'install-latest.ps1') -InstallDirectory $destination -SkipPath
    $command = Get-Command hostops -ErrorAction Stop
    if (-not $command.Source.StartsWith($destination + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Installer did not enable the command in the same session.' }
    $version = (& $command.Source --version | Out-String)
    if ($LASTEXITCODE -ne 0 -or $version -notmatch 'HostOps') { throw 'Downloaded launcher failed.' }
    $marker = Get-Content -LiteralPath (Join-Path $destination 'hostops-install.json') -Raw
    [IO.File]::WriteAllText($checksumPath, (('0' * 64) + "  $assetName`n"))
    $rejected = $false
    try { & (Join-Path $PSScriptRoot 'install-latest.ps1') -InstallDirectory $destination -SkipPath } catch { $rejected = $_.Exception.Message -match 'Download checksum failed' }
    if (-not $rejected -or (Get-Content -LiteralPath (Join-Path $destination 'hostops-install.json') -Raw) -cne $marker) { throw 'Damaged download was not safely rejected.' }
    $testRelease.assets = @()
    $rejected = $false
    try { & (Join-Path $PSScriptRoot 'install-latest.ps1') -InstallDirectory $destination -SkipPath } catch { $rejected = $_.Exception.Message -match 'missing' }
    if (-not $rejected) { throw 'Incomplete release was not rejected.' }
    Write-Host 'Latest-release installer passed: matching bundle, current-session PATH, checksum rejection and missing-asset rejection. No network requests or user PATH changes.' -ForegroundColor Green
} finally { $env:Path = $savedPath }
