param([switch]$Check)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskTools = Join-Path $taskRoot '.local\toolchains'
$env:CARGO_HOME = Join-Path $taskTools 'cargo'
$env:RUSTUP_HOME = Join-Path $taskTools 'rustup'
$env:PATH = (Join-Path $env:CARGO_HOME 'bin') + ';' + $env:PATH
node (Join-Path $PSScriptRoot 'prepare-desktop-binaries.mjs')
if ($LASTEXITCODE -ne 0) { throw 'Desktop binary resource preparation failed.' }
Push-Location (Join-Path $taskRoot 'apps\client')
try {
    if ($Check) { cargo check --locked --manifest-path src-tauri/Cargo.toml }
    else { npm run tauri -- dev }
    if ($LASTEXITCODE -ne 0) { throw "Native development command exited with code $LASTEXITCODE" }
} finally { Pop-Location }
