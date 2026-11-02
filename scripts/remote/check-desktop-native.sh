#!/bin/sh
# One Mac desktop compile check; no GUI, installer or model authorization.
set -eu
task_root=$(pwd -P)
case "$task_root" in /Users/mewgull/MewDex/.state/native-risk-*) ;; *) echo 'Unexpected native development directory' >&2; exit 1;; esac
task_tools=/Users/mewgull/MewDex/.local/toolchains
export CARGO_HOME="$task_tools/cargo" RUSTUP_HOME="$task_tools/rustup"
export PATH="$CARGO_HOME/bin:/opt/homebrew/bin:$PATH"
export CARGO_TARGET_DIR="$task_root/apps/client/src-tauri/target"
test "$(uname -s)" = Darwin && test "$(uname -m)" = arm64
mkdir -p "$task_root/.state"
task_guard="$task_root/.state/desktop-compile.lock"
mkdir "$task_guard" || { echo 'Another desktop check or its retained lock exists' >&2; exit 1; }
task_status="$task_root/.state/desktop-compile.json"
task_log="$task_root/.state/desktop-compile.log"
trap 'rmdir "$task_guard"' EXIT
printf '{"state":"running","pid":%s,"platform":"darwin-arm64","uiLaunched":false,"finalCandidate":false}\n' "$$" > "$task_status"
chmod u+x apps/client/src-tauri/binaries/mewdex-node-aarch64-apple-darwin
task_result=0
cargo check --locked --manifest-path apps/client/src-tauri/Cargo.toml > "$task_log" 2>&1 || task_result=$?
/usr/bin/python3 - "$task_status" "$task_result" <<'PY'
import json,sys,datetime,os
p=sys.argv[1]
with open(p) as f:v=json.load(f)
v.update(state="succeeded" if sys.argv[2]=="0" else "failed",exitCode=int(sys.argv[2]),finishedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
with open(p+".tmp","w") as f:json.dump(v,f)
os.replace(p+".tmp",p)
PY
tail -n 12 "$task_log"
exit "$task_result"
