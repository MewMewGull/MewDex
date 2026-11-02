#!/bin/sh
# Isolated Linux source check. Does not replace or restart any device service.
set -eu
task_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
case "$task_root" in /root/MewDex/.state/source-check-*) ;; *) echo 'Unexpected source-check directory' >&2; exit 1;; esac
exec 9>"$task_root/check.lock"
flock -n 9 || { echo 'A source check is already running' >&2; exit 1; }
task_tools=/root/MewDex/.local/go-source-check
mkdir -p "$task_tools"
export GOPATH="$task_tools/path" GOMODCACHE="$task_tools/modules" GOCACHE="$task_tools/cache"
if test -x "$task_tools/runtime/go1.27.0/go/bin/go"; then
  export PATH="$task_tools/runtime/go1.27.0/go/bin:$PATH"
  export GOTOOLCHAIN=local
else
  export GOTOOLCHAIN=go1.27.0
fi
export CGO_ENABLED=1
task_mode=${1:-all}
case "$task_mode" in
  all) set -- ./internal/childprocess ./internal/executor ./internal/localnode ./internal/engine ./internal/lifecycle ./internal/gateway ./internal/transport ;;
  executor) set -- ./internal/executor ;;
  backup) set -- ./internal/backup ./internal/localnode ./internal/engine ./internal/lifecycle ./internal/store ;;
  restore) set -- ./internal/backup ./internal/localnode ./internal/store ./internal/executor ;;
  jobs) set -- ./internal/localnode -run 'Test(CompletedJob|LostWorker|JobObservation|CancellationDuring|RestoreJob)' ;;
  pairing) set -- ./internal/localnode -run 'Test(Executor|RemoteSetupRetains|RestoredExecutorMustPair)' ;;
  processes) set -- ./internal/childprocess ./internal/engine ./internal/executor ./internal/localnode ;;
  startup) set -- ./internal/startup ./internal/localnode ./internal/runtimefiles ./cmd/mewdex ;;
  scope) set -- ./internal/attachments ./internal/executionscope ./internal/executor ./internal/engine ./internal/runtimefiles ./internal/store ./internal/hub ./internal/transport ./scripts/engine-sandbox-risk ./scripts/executor-scope-risk ;;
  *) echo 'Supported checks: all, executor, backup, restore, jobs, pairing, processes, startup, scope' >&2; exit 1 ;;
esac
task_status="$task_root/check-status.json"
printf '{"state":"running","go":"1.27.0","race":true,"serviceChanged":false}\n' > "$task_status"
trap 'printf "{\"state\":\"failed\",\"race\":true,\"serviceChanged\":false}\n" > "$task_status"' EXIT
cd "$task_root"
{
  go version
  go test -race -p 1 "$@"
} > "$task_root/check.log" 2>&1
printf '{"state":"succeeded","go":"1.27.0","race":true,"serviceChanged":false,"finalCandidate":false}\n' > "$task_status"
trap - EXIT
cat "$task_status"
