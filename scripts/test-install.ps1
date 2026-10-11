param([string]$Bundle = (Join-Path $PSScriptRoot '../output/bundles/hostops-windows-amd64.zip'))
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$bundlePath = (Resolve-Path -LiteralPath $Bundle).Path
$testRoot = Join-Path $projectRoot ('local/install-smoke-' + [Guid]::NewGuid().ToString('N'))
$savedPath = $env:Path
$savedConfig = $env:HOSTOPS_CONFIG
$savedAppData = $env:APPDATA
$report = [Collections.Generic.List[string]]::new()
function Assert-Check([bool]$condition, [string]$message) {
    if (-not $condition) { throw $message }
    $report.Add($message)
}
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null
try {
    $extracted = Join-Path $testRoot 'extracted'
    Expand-Archive -LiteralPath $bundlePath -DestinationPath $extracted
    $source = Join-Path $extracted 'hostops-windows-amd64'
    $installation = Join-Path $testRoot 'installed'
    & (Join-Path $source 'Install-HostOps.ps1') -InstallDirectory $installation -SkipPath
    $launch = Join-Path $installation 'bin/hostops.cmd'
    $env:HOSTOPS_CONFIG = ''
    $env:APPDATA = Join-Path $testRoot 'profile'
    $unrelated = Join-Path $testRoot 'another-folder'
    New-Item -ItemType Directory -Force -Path $unrelated | Out-Null
    Push-Location $unrelated
    try {
        $version = (& $launch --version | Out-String)
        Assert-Check ($LASTEXITCODE -eq 0 -and $version -match 'HostOps preview|HostOps dev|HostOps [0-9]') 'Installed launcher works from another directory and reports build metadata.'
        $frame = (& $launch START --demo --once --page export-ai --color never | Out-String)
        Assert-Check ($LASTEXITCODE -eq 0 -and $frame -match 'Export for AI' -and $frame -match 'DEMO') 'Installed offline dashboard opens the export view.'
        # Windows PowerShell 5.1 wraps native stderr in error records. This
        # deliberately cancelled command must be captured rather than thrown.
        $ErrorActionPreference = 'Continue'
        try { $onboarding = ("`n" | & $launch start --once --db :memory: 2>&1 | Out-String) } finally { $ErrorActionPreference = 'Stop' }
        Assert-Check ($LASTEXITCODE -eq 1 -and $onboarding -match 'First-time setup' -and $onboarding -match 'VPS SSH target') 'Fresh installed command enters onboarding without project settings.'
        Assert-Check (-not (Test-Path -LiteralPath (Join-Path $env:APPDATA 'HostOps/hostops.json'))) 'Cancelled first-time setup leaves no unverified connection.'
        $export = Join-Path $testRoot 'export.md'
        $result = (& $launch export-ai --demo --output $export --json | Out-String)
        Assert-Check ($LASTEXITCODE -eq 0 -and (Test-Path -LiteralPath $export)) 'Installed app writes a complete AI Markdown export.'
        $markdown = Get-Content -LiteralPath $export -Raw
        Assert-Check ($markdown -match 'Evidence' -and $markdown -match 'security' -and $markdown -match 'recovery') 'Markdown includes consolidated assessment evidence.'
    } finally { Pop-Location }
    $manifest = Get-Content -LiteralPath (Join-Path $source 'bundle.json') -Raw | ConvertFrom-Json
    Assert-Check (-not ($manifest.files.path | Where-Object { $_ -match '(^local/|^\.git/|\.db$|vps-review|frontend-audit)' })) 'Bundle excludes local configuration, databases and private audit reports.'
    $settings = Join-Path $env:APPDATA 'HostOps/hostops.json'
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $settings) | Out-Null
    [IO.File]::WriteAllText($settings, '{"host":{"id":"test-host","name":"Test connection","ssh":"ops@example.test","history_mode":"session"},"sites":[]}')
    $beforeSettings = Get-Content -LiteralPath $settings -Raw
    & (Join-Path $source 'Install-HostOps.ps1') -InstallDirectory $installation -SkipPath
    Assert-Check ((Get-Content -LiteralPath $settings -Raw) -ceq $beforeSettings) 'Reinstall preserves per-user settings.'
    $oldLauncher = Get-Content -LiteralPath $launch -Raw
    $asset = Join-Path $source 'GETTING-STARTED.md'
    $oldAsset = [IO.File]::ReadAllBytes($asset)
    try {
        [IO.File]::AppendAllText($asset, 'tampered')
        $rejected = $false
        try { & (Join-Path $source 'Install-HostOps.ps1') -InstallDirectory $installation -SkipPath } catch { $rejected = $_.Exception.Message -match 'checksum failed' }
        Assert-Check $rejected 'Damaged bundle is rejected before activating an update.'
        Assert-Check ((Get-Content -LiteralPath $launch -Raw) -ceq $oldLauncher) 'Rejected update preserves the current launcher.'
    } finally { [IO.File]::WriteAllBytes($asset, $oldAsset) }
    Push-Location $unrelated
    try {
        $frame = (& $launch start --once --no-collect --db :memory: --color never | Out-String)
        Assert-Check ($LASTEXITCODE -eq 0 -and $frame -match 'Opening saved VPS connection: Test connection' -and $frame -notmatch 'First-time setup') 'Installed command reopens saved settings from another directory.'
    } finally { Pop-Location }
    & (Join-Path $installation 'Uninstall-HostOps.ps1') -InstallDirectory $installation -SkipPath
    Assert-Check (-not (Test-Path -LiteralPath $installation) -and (Test-Path -LiteralPath $settings) -and (Test-Path -LiteralPath $export)) 'Uninstall removes only the managed installation and retains settings and export.'
    $report.Add('No production VPS or real user PATH/settings were changed. SSH/discovery integration is covered separately with deterministic service fixtures.')
    $report | Set-Content -LiteralPath (Join-Path $testRoot 'RESULTS.txt')
    $resolvedExtraction = (Resolve-Path -LiteralPath $extracted).Path
    if ($resolvedExtraction.StartsWith($testRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $resolvedExtraction -Recurse -Force }
    Write-Host "`nInstallation smoke test passed. Evidence: $testRoot" -ForegroundColor Green
} finally {
    $env:Path = $savedPath
    $env:HOSTOPS_CONFIG = $savedConfig
    $env:APPDATA = $savedAppData
}
