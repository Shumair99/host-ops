param([string]$Manifest = 'local/hostops.json', [switch]$Demo)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not $Demo -and -not (Test-Path -LiteralPath $Manifest)) { throw 'Run bin/hostops.exe connect user@vps first.' }
    if (-not (Test-Path -LiteralPath 'bin/hostops.exe')) { throw 'Run scripts/check.ps1 first.' }
    if ($Demo) { & .\bin\hostops.exe dashboard --demo } else { & .\bin\hostops.exe dashboard --config $Manifest --db local/hostops.db --interval 30s }
    if ($LASTEXITCODE) { throw 'Dashboard stopped with an error.' }
} finally { Pop-Location }
