# Compile one native desktop development binary. Never starts a window or bundles installers.
param([switch]$SideBySide)

$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskTools = Join-Path $taskRoot '.local\toolchains'
$env:CARGO_HOME = Join-Path $taskTools 'cargo'
$env:RUSTUP_HOME = Join-Path $taskTools 'rustup'
$env:PATH = (Join-Path $env:CARGO_HOME 'bin') + ';' + $env:PATH
$taskTargetName = if ($SideBySide) { 'desktop-development-next-target' } else { 'desktop-development-target' }
$env:CARGO_TARGET_DIR = Join-Path $taskRoot (Join-Path '.state' $taskTargetName)
$taskStatusPath = Join-Path $taskRoot '.state\desktop-development-build.json'
if (Test-Path -LiteralPath $taskStatusPath) {
    $taskPrevious = Get-Content -LiteralPath $taskStatusPath -Raw | ConvertFrom-Json
    if ($taskPrevious.state -eq 'building') { throw 'A previous desktop build is still marked active. Inspect it before starting another.' }
}
$taskGuard = [IO.File]::Open((Join-Path $taskRoot '.state\desktop-development-build.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
$taskReport = [ordered]@{ state='building'; pid=$PID; startedAt=[DateTime]::UtcNow.ToString('o'); platform='windows'; architecture='x64'; configuration='Debug'; finalCandidate=$false; uiLaunched=$false; installerProduced=$false }
$taskReport.targetDirectory = $env:CARGO_TARGET_DIR
$taskReport.sideBySide = [bool]$SideBySide
$taskPushed = $false
try {
    $taskReport.phase = 'resources'
    $taskReport | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
    node (Join-Path $PSScriptRoot 'prepare-desktop-binaries.mjs')
    if ($LASTEXITCODE -ne 0) { throw 'Desktop binary resource preparation failed.' }
    Push-Location (Join-Path $taskRoot 'apps\client')
    $taskPushed = $true
    $taskCli = npm run --silent tauri -- --version
    if ($taskCli -ne 'tauri-cli 2.12.4') { throw 'Pinned Tauri CLI 2.12.4 is required.' }
    $taskReport.phase = 'native-build'
    $taskReport | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
    $taskLog = Join-Path $taskRoot '.state\desktop-development-build.log'
    Write-Output 'Building one development target: Windows / x64 / Debug executable. No window or installer will be started.'
    # Windows PowerShell treats native stderr (including a harmless Tauri
    # warning) as an ErrorRecord under Stop. Judge the process by its exit code.
    $taskNativePreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        npm run tauri -- build --debug --no-bundle --no-sign --ci -- --locked *> $taskLog
        $taskExit = $LASTEXITCODE
    } finally { $ErrorActionPreference = $taskNativePreference }
    $taskReport.state = if ($taskExit -eq 0) { 'succeeded' } else { 'failed' }
    $taskReport.exitCode = $taskExit
    $taskReport.finishedAt = [DateTime]::UtcNow.ToString('o')
    if ($taskExit -eq 0) {
        $taskBinary = Join-Path $env:CARGO_TARGET_DIR 'debug\mewdex-client.exe'
        if (-not (Test-Path -LiteralPath $taskBinary -PathType Leaf)) { throw 'The expected native executable is missing.' }
        $taskReport.binary = $taskBinary
        $taskReport.sha256 = (Get-FileHash -LiteralPath $taskBinary -Algorithm SHA256).Hash.ToLowerInvariant()
        $taskHelper = Join-Path (Split-Path $taskBinary -Parent) 'mewdex-input-bridge.exe'
        if (-not (Test-Path -LiteralPath $taskHelper -PathType Leaf)) { throw 'The APP channel connector is missing from the development output.' }
        $taskReport.helperSha256 = (Get-FileHash -LiteralPath $taskHelper -Algorithm SHA256).Hash.ToLowerInvariant()
        $taskReport.component = 'app'
        $taskReport.phase = 'complete'
        $taskEvidenceDir = Join-Path $taskRoot '.state\evidence'
        [IO.Directory]::CreateDirectory($taskEvidenceDir) | Out-Null
        $taskReport | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskEvidenceDir 'windows-desktop-build.json') -Encoding utf8
    }
    $taskReport | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
    if ($taskExit -ne 0) { throw "Desktop development build exited with code $taskExit. Inspect $taskLog" }
} catch {
    $taskReport.state = 'failed'
    $taskReport.finishedAt = [DateTime]::UtcNow.ToString('o')
    $taskReport | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
    throw
} finally {
    if ($taskPushed) { Pop-Location }
    $taskGuard.Dispose()
}
