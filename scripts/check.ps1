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
        & $goExe build -buildvcs=false -trimpath -o "bin/$program.exe" "./cmd/$program"
        if ($LASTEXITCODE) { throw "Build failed: $program" }
    }
    $previousOS = $env:GOOS
    $previousArch = $env:GOARCH
    $previousCGOEnabled = $env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'
        $env:CGO_ENABLED = '0'
        foreach ($architecture in @('amd64', 'arm64')) {
            $env:GOARCH = $architecture
            & $goExe build -buildvcs=false -trimpath -o "bin/collectors/linux-$architecture/hostops" './cmd/hostops'
            if ($LASTEXITCODE) { throw "Collector build failed: $architecture" }
            & $goExe build -buildvcs=false -trimpath -o "bin/agents/vps/linux-$architecture/hostops-vps-agent" './cmd/hostops-vps-agent'
            if ($LASTEXITCODE) { throw "VPS agent build failed: $architecture" }
        }
    } finally { $env:GOOS = $previousOS; $env:GOARCH = $previousArch; $env:CGO_ENABLED = $previousCGOEnabled }
} finally { Pop-Location }
