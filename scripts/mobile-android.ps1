param(
    [ValidateSet('Check','Init','BuildArm64')][string]$Action = 'Check',
    [string]$SdkRoot = $env:ANDROID_HOME,
    [string]$JavaRoot = $env:JAVA_HOME,
    [string]$NdkVersion = '27.2.12479018',
    [string]$NdkRoot = ''
)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskTools = Join-Path $taskRoot '.local\toolchains'
if (-not $SdkRoot -or -not $JavaRoot) { throw 'Set Android SDK and Java paths for this process before checking mobile.' }
$taskNdk = if ($NdkRoot) { $NdkRoot } else { Join-Path $SdkRoot "ndk\$NdkVersion" }
$taskClang = Join-Path $taskNdk 'toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android24-clang.cmd'
$taskAr = Join-Path $taskNdk 'toolchains\llvm\prebuilt\windows-x86_64\bin\llvm-ar.exe'
foreach ($taskFile in @((Join-Path $JavaRoot 'bin\java.exe'), $taskClang, $taskAr)) {
    if (-not (Test-Path -LiteralPath $taskFile -PathType Leaf)) { throw "Missing Android build dependency: $taskFile. No installation was attempted." }
}
$env:CARGO_HOME = Join-Path $taskTools 'cargo'
$env:RUSTUP_HOME = Join-Path $taskTools 'rustup'
$taskRustup = Join-Path $env:CARGO_HOME 'bin\rustup.exe'
if (-not (Test-Path -LiteralPath $taskRustup)) { throw 'Project-local Rust is missing. No installation was attempted.' }
$env:PATH = (Join-Path $env:CARGO_HOME 'bin') + ';' + $env:PATH
$env:ANDROID_HOME = $SdkRoot
$env:ANDROID_SDK_ROOT = $SdkRoot
$env:JAVA_HOME = $JavaRoot
$env:NDK_HOME = $taskNdk
$env:GRADLE_USER_HOME = Join-Path $taskTools 'gradle'
$env:CARGO_TARGET_DIR = Join-Path $taskRoot '.state\native-android-target'
$env:CARGO_TARGET_AARCH64_LINUX_ANDROID_LINKER = $taskClang
$env:CC_aarch64_linux_android = $taskClang
$env:AR_aarch64_linux_android = $taskAr
# Keep the SDK selected for other projects unchanged. Flags cover r27 ELF alignment.
$env:CARGO_TARGET_AARCH64_LINUX_ANDROID_RUSTFLAGS = '-C link-arg=-Wl,-z,max-page-size=16384 -C link-arg=-Wl,-z,common-page-size=16384'
Push-Location (Join-Path $taskRoot 'apps\client')
try {
    $taskTargets = & $taskRustup target list --installed --toolchain 1.99.1
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the project Rust targets.' }
    if ($taskTargets -notcontains 'aarch64-linux-android') { throw 'Install aarch64-linux-android into the project-local Rust toolchain first. No installation was attempted.' }
    if ($Action -eq 'Init') {
        if (Test-Path -LiteralPath src-tauri/gen/android) { throw 'An Android project already exists. Inspect it before regenerating.' }
        $taskCli = npm run --silent tauri -- --version
        if ($taskCli -ne 'tauri-cli 2.12.4') { throw 'Pinned Tauri CLI 2.12.4 is required before Android initialization.' }
        npm run tauri -- android init --ci --skip-targets-install
        if ($LASTEXITCODE -ne 0) { throw "Android project initialization exited with code $LASTEXITCODE" }
    } elseif ($Action -eq 'BuildArm64') {
        $taskAndroid = Join-Path (Get-Location).Path 'src-tauri\gen\android'
        $taskModule = Join-Path $taskAndroid 'app\build.gradle.kts'
        $taskProperties = Join-Path $taskAndroid 'gradle.properties'
        $taskWrapper = Join-Path $taskAndroid 'gradle\wrapper\gradle-wrapper.properties'
        $taskLockfile = Get-Content -LiteralPath (Join-Path $taskRoot 'tools\android-deps.lock.json') -Raw | ConvertFrom-Json
        $taskSdkFull = [IO.Path]::GetFullPath($SdkRoot)
        $taskToolsFull = [IO.Path]::GetFullPath($taskTools).TrimEnd('\') + '\'
        if (-not $taskSdkFull.StartsWith($taskToolsFull, [StringComparison]::OrdinalIgnoreCase)) { throw 'Development APK builds require an SDK inside the MewDex tool directory.' }
        $taskSdkLock = [IO.File]::Open((Join-Path $taskSdkFull '.mewdex-prepare-lock'), 'OpenOrCreate', 'ReadWrite', 'None')
        try {
        $taskPackageFiles = foreach ($taskPackage in (@($taskLockfile.sdk.platform) + @($taskLockfile.sdk.libraryPlatforms) + @($taskLockfile.sdk.agpDefaultBuildTools, $taskLockfile.sdk.buildTools))) {
            Join-Path (Join-Path $SdkRoot $taskPackage.Replace(';','\')) $(if ($taskPackage.StartsWith('platforms;')) { 'android.jar' } else { 'aapt2.exe' })
        }
        foreach ($taskFile in (@($taskModule, $taskProperties, $taskWrapper) + @($taskPackageFiles))) {
            if (-not (Test-Path -LiteralPath $taskFile -PathType Leaf)) { throw "Missing development build dependency: $taskFile. No SDK package installation was attempted." }
        }
        $taskCli = npm run --silent tauri -- --version
        if ($taskCli -ne 'tauri-cli 2.12.4') { throw 'Pinned Tauri CLI 2.12.4 is required.' }
        $taskModuleText = [IO.File]::ReadAllText($taskModule)
        if ($taskModuleText -notmatch 'compileSdk = 37' -or $taskModuleText -notmatch 'targetSdk = 37') { throw 'Generated Android SDK levels changed; inspect them before building.' }
        if ($taskModuleText -notmatch 'ndkPath =') {
            $taskModuleText = $taskModuleText.Replace('android {', 'android {' + "`n" + '    ndkPath = System.getenv("NDK_HOME") ?: throw GradleException("NDK_HOME is required")')
            [IO.File]::WriteAllText($taskModule, $taskModuleText, [Text.UTF8Encoding]::new($false))
        }
        $taskPropertiesText = [IO.File]::ReadAllText($taskProperties)
        foreach ($taskSetting in @('android.builder.sdkDownload=false','org.gradle.daemon=false')) {
            if (-not $taskPropertiesText.Contains($taskSetting)) { $taskPropertiesText += "`n" + $taskSetting + "`n" }
        }
        [IO.File]::WriteAllText($taskProperties, $taskPropertiesText, [Text.UTF8Encoding]::new($false))
        $taskWrapperText = [IO.File]::ReadAllText($taskWrapper)
        if (-not $taskWrapperText.Contains('gradle-9.6.1-bin.zip')) { throw 'Generated Gradle wrapper version changed; inspect it before building.' }
        $taskChecksumLine = 'distributionSha256Sum=' + $taskLockfile.gradle.sha256
        if ($taskWrapperText -match 'distributionSha256Sum=' -and -not $taskWrapperText.Contains($taskChecksumLine)) { throw 'A different Gradle checksum exists; it was preserved.' }
        if (-not $taskWrapperText.Contains($taskChecksumLine)) { [IO.File]::WriteAllText($taskWrapper, $taskWrapperText + "`n" + $taskChecksumLine + "`n", [Text.UTF8Encoding]::new($false)) }
        $taskStatusPath = Join-Path $taskRoot '.state\android-arm64-build.json'
        if (Test-Path -LiteralPath $taskStatusPath) {
            $taskPrevious = Get-Content -LiteralPath $taskStatusPath -Raw | ConvertFrom-Json
            if ($taskPrevious.state -eq 'building') { throw 'A prior Android build is still marked active; inspect it before starting another.' }
        }
        $taskBuildLock = [IO.File]::Open((Join-Path $taskRoot '.state\android-arm64-build.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
        $taskBuild = [ordered]@{state='building'; pid=$PID; startedAt=[DateTime]::UtcNow.ToString('o'); platform='android'; architecture='arm64'; configuration='Debug'; finalCandidate=$false; sdkRoot=$SdkRoot; ndkRoot=$taskNdk; uiLaunched=$false}
        try {
            $taskBuild | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
            $taskLog = Join-Path $taskRoot '.state\android-arm64-build.log'
            Write-Output 'Building one development target: Android / arm64 / Debug APK. Log: .state/android-arm64-build.log'
            npm run tauri -- android build --debug --target aarch64 --apk --ci *> $taskLog
            $taskExit = $LASTEXITCODE
            $taskBuild.state = if ($taskExit -eq 0) { 'succeeded' } else { 'failed' }
            $taskBuild.exitCode = $taskExit
            $taskBuild.finishedAt = [DateTime]::UtcNow.ToString('o')
            $taskBuild | ConvertTo-Json | Set-Content -LiteralPath $taskStatusPath -Encoding utf8
            if ($taskExit -ne 0) { throw "Android development build exited with code $taskExit; inspect $taskLog" }
        } finally { $taskBuildLock.Dispose() }
        } finally { $taskSdkLock.Dispose() }
    } else {
        cargo check --locked --manifest-path src-tauri/Cargo.toml --target aarch64-linux-android
        if ($LASTEXITCODE -ne 0) { throw "Android Rust check exited with code $LASTEXITCODE" }
    }
} finally { Pop-Location }
