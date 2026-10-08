param([string]$Manifest = 'local/hostops.json')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
$previousToken = $env:HOSTOPS_TOKEN
try {
    if (-not (Test-Path -LiteralPath $Manifest)) { throw 'Create local/hostops.json from an examples/sites manifest first.' }
    if (-not (Test-Path -LiteralPath 'bin/hostops-agent.exe')) { throw 'Run scripts/check.ps1 first.' }
    New-Item -ItemType Directory -Force -Path local | Out-Null
    $tokenFile = Join-Path $projectRoot 'local/dev-token.txt'
    if (-not (Test-Path -LiteralPath $tokenFile)) {
        $tokenBytes = New-Object byte[] 32
        $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
        try { $random.GetBytes($tokenBytes) } finally { $random.Dispose() }
        [IO.File]::WriteAllText($tokenFile, [Convert]::ToBase64String($tokenBytes))
    }
    $env:HOSTOPS_TOKEN = [IO.File]::ReadAllText($tokenFile).Trim()
    Write-Host 'Open http://127.0.0.1:9090 and use the token saved in local/dev-token.txt. Ctrl+C stops the local agent.'
    & .\bin\hostops-agent.exe --config $Manifest --db local/hostops.db --interval 60s
    if ($LASTEXITCODE) { throw 'Local agent stopped with an error.' }
} finally {
    $env:HOSTOPS_TOKEN = $previousToken
    Pop-Location
}
