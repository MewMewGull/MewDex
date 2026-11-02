param([string]$Config = '.local/device.json')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
Push-Location $taskRoot
try {
    go run ./cmd/mewdex serve -config $Config
    if ($LASTEXITCODE -ne 0) { throw "Device service exited with code $LASTEXITCODE" }
} finally { Pop-Location }
