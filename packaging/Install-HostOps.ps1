param(
    [string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA 'Programs\HostOps'),
    [switch]$SkipPath
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$sourceRoot = [IO.Path]::GetFullPath($PSScriptRoot)
$installRoot = [IO.Path]::GetFullPath($InstallDirectory)
function Assert-RegularLocation([string]$path) {
    $current = [IO.Path]::GetFullPath($path)
    while ($current) {
        if (Test-Path -LiteralPath $current) {
            if ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Installation path cannot contain links: $current" }
        }
        $next = Split-Path -Parent $current
        if ($next -eq $current) { break }
        $current = $next
    }
}
Assert-RegularLocation $installRoot
if ($installRoot -eq [IO.Path]::GetPathRoot($installRoot)) { throw 'Choose a dedicated HostOps installation directory.' }
if ($installRoot.StartsWith($sourceRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -or $installRoot -eq $sourceRoot) { throw 'Install outside the extracted bundle.' }
if (Test-Path -LiteralPath $installRoot) {
    $item = Get-Item -LiteralPath $installRoot
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Installation directory cannot be a link.' }
    $entries = @(Get-ChildItem -LiteralPath $installRoot -Force)
    if ($entries.Count -gt 0) {
        $markerPath = Join-Path $installRoot 'hostops-install.json'
        if (-not (Test-Path -LiteralPath $markerPath)) { throw 'The destination contains other files. Choose an empty directory.' }
        $marker = Get-Content -LiteralPath $markerPath -Raw | ConvertFrom-Json
        if ($marker.application -ne 'HostOps' -or $marker.directory -ne $installRoot) { throw 'Destination is not a managed HostOps installation.' }
    }
}
$manifestPath = Join-Path $sourceRoot 'bundle.json'
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
if ($manifest.application -ne 'HostOps' -or $manifest.schema -ne 1 -or $manifest.target -notmatch '^windows-(amd64|arm64)$') { throw 'Use a HostOps Windows bundle.' }
$nativeArchitecture = $env:PROCESSOR_ARCHITEW6432
if (-not $nativeArchitecture) { $nativeArchitecture = $env:PROCESSOR_ARCHITECTURE }
if (($nativeArchitecture -ieq 'ARM64' -and $manifest.target -ne 'windows-arm64') -or ($nativeArchitecture -ine 'ARM64' -and $manifest.target -ne 'windows-amd64')) { throw 'Use the bundle matching your Windows architecture.' }
$required = @('hostops.exe','hostops-agent.exe','hostops-watcher.exe','collectors/linux-amd64/hostops','collectors/linux-arm64/hostops','agents/vps/linux-amd64/hostops-vps-agent','agents/vps/linux-arm64/hostops-vps-agent')
$listed = @{}
foreach ($entry in $manifest.files) {
    $relative = [string]$entry.path
    if (-not $relative -or [IO.Path]::IsPathRooted($relative) -or $relative -match '[:\x00-\x1f]' -or $relative -match '(^|[/\\])\.\.?([/\\]|$)' -or $entry.sha256 -notmatch '^[a-f0-9]{64}$') { throw 'Invalid bundle manifest path or checksum.' }
    $file = [IO.Path]::GetFullPath((Join-Path $sourceRoot $relative))
    if (-not $file.StartsWith($sourceRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Bundle path escapes its directory.' }
    if ($listed.ContainsKey($relative)) { throw 'Duplicate bundle manifest path.' }
    $listed[$relative] = $file
    $item = Get-Item -LiteralPath $file
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Bundle assets must be regular files.' }
    $parent = $item.Directory
    while ($parent.FullName -ne $sourceRoot) {
        if ($parent.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Bundle directories cannot be links.' }
        $parent = $parent.Parent
    }
    if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.sha256) { throw "Bundle checksum failed: $relative. Extract a fresh complete bundle." }
}
foreach ($relative in $required) { if (-not $listed.ContainsKey($relative)) { throw "Incomplete bundle: $relative" } }
$digest = (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant().Substring(0,20)
$versions = Join-Path $installRoot 'versions'
$release = Join-Path $versions $digest
$stage = Join-Path $installRoot ('.stage-' + [Guid]::NewGuid().ToString('N'))
Assert-RegularLocation $versions
Assert-RegularLocation (Join-Path $installRoot 'bin')
Assert-RegularLocation $release
New-Item -ItemType Directory -Force -Path $versions | Out-Null
try {
    New-Item -ItemType Directory -Path $stage | Out-Null
    foreach ($entry in $manifest.files) {
        $destination = Join-Path $stage ([string]$entry.path)
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
        Copy-Item -LiteralPath $listed[[string]$entry.path] -Destination $destination
    }
    Copy-Item -LiteralPath $manifestPath -Destination (Join-Path $stage 'bundle.json')
    # An immutable version directory keeps an already-running app usable during updates.
    if (Test-Path -LiteralPath $release) {
        foreach ($entry in $manifest.files) {
            $existing = Join-Path $release ([string]$entry.path)
            Assert-RegularLocation $existing
            if (-not (Test-Path -LiteralPath $existing -PathType Leaf) -or (Get-FileHash -LiteralPath $existing -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.sha256) { throw 'Installed version is damaged. Choose another installation directory.' }
        }
    } else {
        $resolvedStage = [IO.Path]::GetFullPath($stage)
        if (-not $resolvedStage.StartsWith($installRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Invalid installation staging path.' }
        Move-Item -LiteralPath $stage -Destination $release
    }
    $bin = Join-Path $installRoot 'bin'
    New-Item -ItemType Directory -Force -Path $bin | Out-Null
    foreach ($program in @('hostops','hostops-agent','hostops-watcher')) {
        Assert-RegularLocation (Join-Path $bin "$program.cmd")
        $launcher = "@echo off`r`n`"%~dp0..\versions\$digest\$program.exe`" %*`r`n"
        [IO.File]::WriteAllText((Join-Path $bin "$program.cmd"), $launcher, [Text.Encoding]::ASCII)
    }
    $record = @{ application = 'HostOps'; directory = $installRoot; version = $manifest.version; active = $digest }
    Assert-RegularLocation (Join-Path $installRoot 'hostops-install.json')
    Assert-RegularLocation (Join-Path $installRoot 'Uninstall-HostOps.ps1')
    [IO.File]::WriteAllText((Join-Path $installRoot 'hostops-install.json'), ($record | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
    Copy-Item -LiteralPath (Join-Path $sourceRoot 'Uninstall-HostOps.ps1') -Destination (Join-Path $installRoot 'Uninstall-HostOps.ps1') -Force
    if (-not $SkipPath) {
        $userPath = [Environment]::GetEnvironmentVariable('Path','User')
        $parts = @($userPath -split ';' | Where-Object { $_ })
        if (-not ($parts | Where-Object { $_.TrimEnd('\') -ieq $bin.TrimEnd('\') })) {
            [Environment]::SetEnvironmentVariable('Path', (($parts + $bin) -join ';'), 'User')
        }
    }
    if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $bin.TrimEnd('\') })) { $env:Path = "$bin;$env:Path" }
    Write-Host "`nHostOps $($manifest.version) installed for your account." -ForegroundColor Green
    Write-Host "Location: $installRoot"
    Write-Host 'Open a new terminal and run: hostops start'
    Write-Host 'Onboarding verifies SSH access and discovers your VPS. Your connection settings live in your user profile.'
    if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) { Write-Host 'OpenSSH client is required to connect a VPS; install it through your operating system before starting.' -ForegroundColor Yellow }
} finally {
    if (Test-Path -LiteralPath $stage) {
        $resolvedStage = (Resolve-Path -LiteralPath $stage).Path
        if ($resolvedStage.StartsWith($installRoot + [IO.Path]::DirectorySeparatorChar + '.stage-', [StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $resolvedStage -Recurse -Force }
    }
}
