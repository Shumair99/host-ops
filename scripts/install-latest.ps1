param(
    [string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA 'Programs\HostOps'),
    [switch]$SkipPath
)

# Keep installer variables out of the caller's session when run through iex.
& {
    param([string]$Destination, [bool]$SkipUserPath)
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version Latest
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $repository = 'Shumair99/host-ops'
    $architecture = $env:PROCESSOR_ARCHITEW6432
    if (-not $architecture) { $architecture = $env:PROCESSOR_ARCHITECTURE }
    switch ($architecture.ToUpperInvariant()) {
        'AMD64' { $target = 'windows-amd64' }
        'ARM64' { $target = 'windows-arm64' }
        default { throw 'HostOps supports Windows amd64 and arm64.' }
    }
    $name = "hostops-$target.zip"
    $headers = @{ Accept = 'application/vnd.github+json'; 'User-Agent' = 'HostOps-installer' }
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repository/releases/latest" -Headers $headers
    } catch {
        throw "Could not find the latest HostOps release. Check your connection and https://github.com/$repository/releases. $($_.Exception.Message)"
    }
    if ($release.tag_name -notmatch '^v?[0-9]+\.[0-9]+\.[0-9]+$' -or $release.draft -or $release.prerelease) { throw 'The latest release must be a stable version.' }
    $archiveAsset = @($release.assets | Where-Object { $_.name -ceq $name })
    $checksumAsset = @($release.assets | Where-Object { $_.name -ceq 'SHA256SUMS' })
    if ($archiveAsset.Count -ne 1 -or $checksumAsset.Count -ne 1) { throw "The release is missing $name or SHA256SUMS." }
    $base = "https://github.com/$repository/releases/download/$($release.tag_name)/"
    if ($archiveAsset[0].browser_download_url -cne "$base$name" -or $checksumAsset[0].browser_download_url -cne "${base}SHA256SUMS") { throw 'Unexpected release download location.' }
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    $work = [IO.Path]::GetFullPath((Join-Path $tempRoot ('hostops-download-' + [Guid]::NewGuid().ToString('N'))))
    if (-not $work.StartsWith($tempRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Invalid temporary directory.' }
    New-Item -ItemType Directory -Path $work | Out-Null
    try {
        Write-Host "Downloading HostOps $($release.tag_name) for $target..."
        $archive = Join-Path $work $name
        $checksums = Join-Path $work 'SHA256SUMS'
        Invoke-WebRequest -UseBasicParsing -Uri "$base$name" -OutFile $archive
        Invoke-WebRequest -UseBasicParsing -Uri "${base}SHA256SUMS" -OutFile $checksums
        $matchesForAsset = @(Get-Content -LiteralPath $checksums | Where-Object { $_ -cmatch ('^[a-f0-9]{64}  ' + [Regex]::Escape($name) + '$') })
        if ($matchesForAsset.Count -ne 1) { throw 'Release checksum is missing or duplicated.' }
        $expected = $matchesForAsset[0].Substring(0, 64)
        if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expected) { throw 'Download checksum failed. Installation was not changed.' }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $zip = [IO.Compression.ZipFile]::OpenRead($archive)
        try {
            foreach ($entry in $zip.Entries) {
                if (-not $entry.FullName.StartsWith("hostops-$target/", [StringComparison]::Ordinal) -or $entry.FullName -match '[:\\\x00-\x1f]' -or $entry.FullName -match '(^|/)\.\.?(/|$)') { throw 'Invalid archive entry.' }
            }
        } finally { $zip.Dispose() }
        $extracted = Join-Path $work 'extracted'
        Expand-Archive -LiteralPath $archive -DestinationPath $extracted
        $installer = Join-Path $extracted "hostops-$target/Install-HostOps.ps1"
        # A process-only policy lets the bundled installer run without changing
        # the account's execution policy. Enable PATH in this session afterward.
        $arguments = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-InstallDirectory', $Destination)
        if ($SkipUserPath) { $arguments += '-SkipPath' }
        & (Get-Process -Id $PID).Path @arguments
        if ($LASTEXITCODE -ne 0) { throw 'The bundle installer failed.' }
        $bin = Join-Path ([IO.Path]::GetFullPath($Destination)) 'bin'
        if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $bin.TrimEnd('\') })) { $env:Path = "$bin;$env:Path" }
        Write-Host 'Ready. Run: hostops start' -ForegroundColor Green
    } finally {
        $resolvedWork = [IO.Path]::GetFullPath($work)
        if ($resolvedWork.StartsWith($tempRoot.TrimEnd('\') + '\hostops-download-', [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedWork)) {
            Remove-Item -LiteralPath $resolvedWork -Recurse -Force
        }
    }
} $InstallDirectory ([bool]$SkipPath)
