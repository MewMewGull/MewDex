#!/usr/bin/env python3
"""Check native notification metadata on the recorded MewDex-owned simulator.

Runs the two Swift decoder checks only. Does not launch MewDex, request notification
permission, send a notification, read credentials, or submit a model conversation.
"""
import datetime
import fcntl
import json
import pathlib
import subprocess


def main():
    root = pathlib.Path.cwd().resolve()
    if str(root.parent) != "/Users/mewgull/MewDex/.state" or not root.name.startswith("native-risk-"):
        raise RuntimeError("Use the owned native development checkout")
    work = root / ".state/notification-native-risk"
    if work.is_symlink():
        raise RuntimeError("Notification evidence cannot redirect outside the workspace")
    work.mkdir(exist_ok=True)
    guard = (work / "run.lock").open("a+")
    fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def command(argv, timeout=90):
        result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=timeout)
        if result.returncode:
            raise RuntimeError("Native check command failed: " + " ".join(argv[:3]) + ": " + result.stderr[-1500:])
        return result.stdout

    def devices():
        values = json.loads(command(["xcrun", "simctl", "list", "devices", "--json"]))["devices"]
        return [device for group in values.values() for device in group]

    retained = json.loads((root / ".state/ios-ui-network-risk/device.json").read_text())
    device_id = retained["deviceId"]
    matches = [device for device in devices() if device["udid"] == device_id and device["name"] == retained["name"]]
    if len(matches) != 1 or device_id in retained["baselineIds"] or matches[0]["state"] != "Shutdown":
        raise RuntimeError("The recorded owned simulator must be confirmed and shut down")
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d-%H%M%S")
    report_path = work / ("metadata-" + stamp + ".json")
    log_path = work / ("metadata-" + stamp + ".log")
    result_path = work / ("metadata-" + stamp + ".xcresult")
    report = {"state": "preparing", "deviceId": device_id, "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "onlyOwnedSimulator": True, "notificationSent": False, "permissionRequested": False,
              "modelCredentialsRead": False, "packagingValidation": False}

    def write():
        temporary = report_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(report, indent=2) + "\n")
        temporary.replace(report_path)

    write()
    boot_requested = False
    try:
        boot_requested = True
        command(["xcrun", "simctl", "boot", device_id])
        command(["xcrun", "simctl", "bootstatus", device_id, "-b"])
        report["state"] = "running"
        write()
        with log_path.open("x") as log:
            result = subprocess.run([
                "xcodebuild", "test", "-scheme", "tauri-plugin-notification", "-configuration", "Debug",
                "-destination", "platform=iOS Simulator,id=" + device_id,
                "-derivedDataPath", str(work / "derived"), "-clonedSourcePackagesDirPath",
                str(root / ".state/notification-swift-check-20261029/SourcePackages"),
                "-resultBundlePath", str(result_path), "-parallel-testing-enabled", "NO",
                "-maximum-concurrent-test-simulator-destinations", "1", "-only-testing:MewDexNotificationTests",
                "ARCHS=arm64", "ONLY_ACTIVE_ARCH=YES", "CODE_SIGNING_ALLOWED=NO",
            ], cwd=root / "third_party/tauri-plugin-notification/ios", stdout=log, stderr=log, timeout=240)
        report.update(exitCode=result.returncode, log=str(log_path), resultBundle=str(result_path), state="failed")
        if result.returncode == 0:
            summary = json.loads(command(["xcrun", "xcresulttool", "get", "test-results", "summary", "--path", str(result_path)]))
            report["summary"] = summary
            if summary.get("passedTests") == 2 and summary.get("failedTests") == 0:
                report["state"] = "succeeded"
    except Exception as error:
        report.update(state="failed", error=str(error))
    finally:
        if boot_requested:
            try:
                command(["xcrun", "simctl", "shutdown", device_id])
                report["simulatorStopped"] = True
            except Exception as error:
                report.update(simulatorStopped=False, stopError=str(error))
        report["baselineDevicesRetained"] = set(retained["baselineIds"]).issubset({device["udid"] for device in devices()})
        report["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        write()
        print(json.dumps(report), flush=True)
    if report["state"] != "succeeded" or not report.get("simulatorStopped"):
        raise RuntimeError("Inspect the retained native metadata evidence before rerunning")


if __name__ == "__main__":
    main()
