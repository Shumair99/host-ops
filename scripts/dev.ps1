param([string]$Manifest = 'local/hostops.json')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not (Test-Path -LiteralPath $Manifest)) { throw 'Create local/hostops.json from an examples/sites manifest first.' }
    if (-not (Test-Path -LiteralPath 'bin/hostops-agent.exe')) { throw 'Run scripts/check.ps1 first.' }
    & .\bin\hostops-agent.exe --config $Manifest --db local/hostops.db --interval 60s
    if ($LASTEXITCODE) { throw 'Collector stopped with an error.' }
} finally { Pop-Location }
