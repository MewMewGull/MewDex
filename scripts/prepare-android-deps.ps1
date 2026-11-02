param([string]$ExistingSdk = $env:ANDROID_HOME)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskSdk = Join-Path $taskRoot '.local\toolchains\android-sdk'
$taskDependencies = Get-Content -LiteralPath (Join-Path $taskRoot 'tools\android-deps.lock.json') -Raw | ConvertFrom-Json
$taskManager = Join-Path $ExistingSdk 'cmdline-tools\latest\bin\sdkmanager.bat'
if (-not (Test-Path -LiteralPath $taskManager -PathType Leaf)) { throw 'Existing Android SDK manager is missing; no SDK was installed.' }
$taskLicense = Join-Path $ExistingSdk 'licenses\android-sdk-license'
if (-not (Test-Path -LiteralPath $taskLicense -PathType Leaf)) { throw 'An existing accepted SDK license is required. No agreement was accepted automatically.' }
New-Item -ItemType Directory -Path (Join-Path $taskSdk 'licenses') -Force | Out-Null
$taskLicenseCopy = Join-Path $taskSdk 'licenses\android-sdk-license'
if (Test-Path -LiteralPath $taskLicenseCopy) {
    if ((Get-FileHash -LiteralPath $taskLicenseCopy).Hash -ne (Get-FileHash -LiteralPath $taskLicense).Hash) { throw 'A different project-local license file exists; it was preserved.' }
} else { Copy-Item -LiteralPath $taskLicense -Destination $taskLicenseCopy }
$taskGuard = Join-Path $taskSdk '.mewdex-prepare-lock'
$taskLock = [System.IO.File]::Open($taskGuard, 'OpenOrCreate', 'ReadWrite', 'None')
try {
    $taskPackages = @($taskDependencies.sdk.platform) + @($taskDependencies.sdk.libraryPlatforms) + @($taskDependencies.sdk.agpDefaultBuildTools, $taskDependencies.sdk.buildTools)
    foreach ($taskPackage in $taskPackages) { if ($taskPackage -notmatch '^(platforms|build-tools);[a-z0-9.\-]+$') { throw 'Unexpected package in the Android dependency lock.' } }
    # An explicit SDK root confines package writes to MewDex. Stdin is closed;
    # a newly required license cannot be accepted by this script.
    $taskStart = [System.Diagnostics.ProcessStartInfo]::new()
    $taskStart.FileName = $env:ComSpec
    $taskPackageArguments = ($taskPackages | ForEach-Object { '"' + $_ + '"' }) -join ' '
    $taskStart.Arguments = '/d /s /c ""' + $taskManager + '" --sdk_root="' + $taskSdk + '" ' + $taskPackageArguments + '"'
    $taskStart.UseShellExecute = $false
    $taskStart.CreateNoWindow = $true
    $taskStart.RedirectStandardInput = $true
    $taskProcess = [System.Diagnostics.Process]::Start($taskStart)
    $taskProcess.StandardInput.Close()
    $taskProcess.WaitForExit()
    if ($taskProcess.ExitCode -ne 0) { throw "Project-local Android package setup exited with code $($taskProcess.ExitCode)" }
    foreach ($taskPackage in $taskPackages) {
        $taskPackagePath = Join-Path $taskSdk $taskPackage.Replace(';','\')
        $taskFile = Join-Path $taskPackagePath $(if ($taskPackage.StartsWith('platforms;')) { 'android.jar' } else { 'aapt2.exe' })
        if (-not (Test-Path -LiteralPath $taskFile -PathType Leaf)) { throw "Expected Android package file was not installed: $taskFile" }
    }
    [ordered]@{ sdk = $taskSdk; packages = $taskPackages; sourceManager = $taskManager; existingSdkModified = $false; licensesAcceptedAutomatically = $false; finalCandidate = $false } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskRoot '.state\evidence\android-dependencies.json') -Encoding utf8
} finally { $taskLock.Dispose() }
