$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$goCommand = Get-Command go -ErrorAction SilentlyContinue
$goExe = if ($goCommand) { $goCommand.Source } else { Join-Path $projectRoot '.tools\go\bin\go.exe' }
if (-not (Test-Path -LiteralPath $goExe)) { throw 'Install Go 1.26+ before running checks.' }
$env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\gomodcache'
Push-Location $projectRoot
try {
    & $goExe test ./...
    if ($LASTEXITCODE) { throw 'Go tests failed.' }
    & $goExe vet ./...
    if ($LASTEXITCODE) { throw 'Go vet failed.' }
    New-Item -ItemType Directory -Force -Path bin | Out-Null
    foreach ($program in @('hostops', 'hostops-agent', 'hostops-watcher')) {
        & $goExe build -trimpath -o "bin/$program.exe" "./cmd/$program"
        if ($LASTEXITCODE) { throw "Build failed: $program" }
    }
    Push-Location web
    try {
        if (-not (Test-Path -LiteralPath node_modules)) {
            npm ci
            if ($LASTEXITCODE) { throw 'npm ci failed.' }
        }
        npm run build
        if ($LASTEXITCODE) { throw 'Dashboard build failed.' }
    } finally { Pop-Location }
} finally { Pop-Location }
