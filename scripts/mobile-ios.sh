#!/bin/sh
# Preflight is read-only: never let `tauri ios init` install/upgrade global tools.
set -eu
export LANG=en_US.UTF-8
if [ -n "${MEWDEX_IOS_TOOLS:-}" ]; then
  test -f "$MEWDEX_IOS_TOOLS/env.sh" || { printf 'Missing project-local iOS tool environment.\n' >&2; exit 2; }
  . "$MEWDEX_IOS_TOOLS/env.sh"
fi
task_action=${1:-check}
task_commands='cargo rustup'
if [ "$task_action" = init ]; then task_commands='cargo rustup node npm xcodegen idevicesyslog pod'; fi
if [ "$task_action" = build-sim ]; then task_commands='cargo rustup node npm xcodebuild ruby'; fi
for task_command in $task_commands; do
  if ! command -v "$task_command" >/dev/null 2>&1; then
    printf 'Missing iOS dependency: %s. No installation was attempted.\n' "$task_command" >&2
    exit 2
  fi
done
if [ "$(uname -s)" != Darwin ]; then printf 'iOS builds require the configured Mac host.\n' >&2; exit 2; fi
xcrun --sdk iphonesimulator --show-sdk-path >/dev/null
case "$task_action" in
  check) cargo check --locked --manifest-path apps/client/src-tauri/Cargo.toml --target aarch64-apple-ios-sim ;;
  init)
    xcodegen --version >/dev/null
    idevicesyslog --version >/dev/null
    pod --version >/dev/null
    xcrun devicectl --version >/dev/null
    test ! -d apps/client/src-tauri/gen/apple || { printf 'Existing iOS project is retained; inspect it before regenerating.\n' >&2; exit 2; }
    cd apps/client
    task_cli=$(npm run --silent tauri -- --version)
    test "$task_cli" = 'tauri-cli 2.12.4' || { printf 'Pinned Tauri CLI 2.12.4 is required before dependency-safe initialization.\n' >&2; exit 2; }
    HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 npm run tauri -- ios init --ci --skip-targets-install
    ;;
  build-sim)
    task_repo=$(pwd -P)
    task_apple="$task_repo/apps/client/src-tauri/gen/apple"
    test -d "$task_apple/mewdex-client.xcodeproj" || { printf 'Generate the iOS development project first.\n' >&2; exit 2; }
    task_guard="$task_apple/.mewdex-build-lock"
    if ! mkdir "$task_guard" 2>/dev/null; then printf 'An iOS build or its retained lock exists: %s\n' "$task_guard" >&2; exit 2; fi
    task_finished=true
    trap 'if [ "$task_finished" = true ]; then rm -f "$task_guard/pid"; rmdir "$task_guard"; fi' EXIT
    mkdir -p "$task_repo/.state"
    task_log="$task_repo/.state/ios-sim-build.log"
    # No developer account is required for the isolated simulator probe. Its
    # keychain groups must be linked as simulated entitlements, not added as
    # restricted host code-signature entitlements after the build.
    /usr/bin/python3 "$task_repo/scripts/remote/prepare-ios-simulator-entitlements.py"
    cd apps/client
    task_cli=$(npm run --silent tauri -- --version)
    test "$task_cli" = 'tauri-cli 2.12.4' || { task_finished=true; printf 'Pinned Tauri CLI 2.12.4 is required.\n' >&2; exit 2; }
    printf 'Building one development target: iOS Simulator / arm64 / Debug / unsigned.\n'
    task_finished=false
    npm run tauri -- ios build --debug --target aarch64-sim --no-sign --archive-only --ci > "$task_log" 2>&1 &
    task_pid=$!
    printf '%s\n' "$task_pid" > "$task_guard/pid"
    /usr/bin/python3 - "$task_repo/.state/ios-sim-build.json" "$task_pid" <<'PY'
import json,os,sys,datetime
p=sys.argv[1]
v={"state":"building","pid":int(sys.argv[2]),"at":datetime.datetime.now(datetime.timezone.utc).isoformat(),"platform":"ios-simulator","architecture":"arm64","configuration":"Debug","signed":False,"simulatorEntitlements":True,"finalCandidate":False,"uiLaunched":False}
with open(p+".tmp","w") as f: json.dump(v,f)
os.replace(p+".tmp",p)
PY
    task_result=0
    wait "$task_pid" || task_result=$?
    task_finished=true
    /usr/bin/python3 - "$task_repo/.state/ios-sim-build.json" "$task_result" <<'PY'
import json,os,sys,datetime
p=sys.argv[1]
with open(p) as f: v=json.load(f)
v.update(state="succeeded" if sys.argv[2]=="0" else "failed",exitCode=int(sys.argv[2]),finishedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
with open(p+".tmp","w") as f:json.dump(v,f)
os.replace(p+".tmp",p)
PY
    tail -n 20 "$task_log"
    exit "$task_result"
    ;;
  *) printf 'Supported actions: check, init, build-sim\n' >&2; exit 2 ;;
esac
