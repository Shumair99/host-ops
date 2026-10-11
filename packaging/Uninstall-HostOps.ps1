param([string]$InstallDirectory = $PSScriptRoot, [switch]$SkipPath)
$ErrorActionPreference = 'Stop'
$installRoot = (Resolve-Path -LiteralPath $InstallDirectory).Path
if ($installRoot -eq [IO.Path]::GetPathRoot($installRoot)) { throw 'Refusing to remove a drive root.' }
if ((Get-Item -LiteralPath $installRoot).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Installation directory cannot be a link.' }
$marker = Get-Content -LiteralPath (Join-Path $installRoot 'hostops-install.json') -Raw | ConvertFrom-Json
if ($marker.application -ne 'HostOps' -or $marker.directory -ne $installRoot) { throw 'This is not a managed HostOps installation.' }
if (-not $SkipPath) {
    $bin = Join-Path $installRoot 'bin'
    $parts = @([Environment]::GetEnvironmentVariable('Path','User') -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ine $bin.TrimEnd('\') })
    [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
}
# The absolute target was verified against its installation marker above.
Remove-Item -LiteralPath $installRoot -Recurse -Force
Write-Host 'HostOps removed. Your connection settings, history and exported Markdown remain in your user profile.'
