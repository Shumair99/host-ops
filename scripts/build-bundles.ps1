param(
    [string]$Version = 'dev',
    [string]$Targets = 'windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64',
    [string]$OutputDirectory = 'output/bundles'
)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $go = Get-Command go -ErrorAction SilentlyContinue
    if ($go) { $goPath = $go.Source } else { $goPath = Join-Path $PWD '.tools/go/bin/go.exe' }
    if (-not (Test-Path -LiteralPath $goPath)) { throw 'Building bundles requires Go. Installed users do not need Go.' }
    $env:Path = (Split-Path -Parent $goPath) + ';' + $env:Path
    & $goPath run ./cmd/hostops-bundle --version $Version --targets $Targets --output $OutputDirectory
    if ($LASTEXITCODE -ne 0) { throw 'Bundle build failed.' }
} finally { Pop-Location }
