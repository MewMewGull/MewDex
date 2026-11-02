#!/usr/bin/env python3
"""Run XCUITest only on one recorded MewDex-owned simulator.

Xcode does not enumerate the earlier custom device set. This runner creates one
named device in the default set, records its identity, and preserves every
pre-existing device. It never uses a real phone or installs development tools.
"""
import argparse
import datetime
import fcntl
import hashlib
import json
import os
import pathlib
import plistlib
import re
import subprocess
import time
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--app", required=True)
    parser.add_argument("--mode", choices=["observe", "network", "recover", "viewport", "attachment-picker", "attachment-files", "attachment-file-upload", "attachment-gallery", "attachment-upload", "notification-settings", "notification-enable", "notification-disable", "notification-warm", "notification-cold", "notification-cold-open", "notification-offline", "notification-revoked"], default="observe")
    parser.add_argument("--notification-id", help="Explicit retained no-model fixture instance and event number")
    parser.add_argument("--plan", help="Private, explicit input-network probe plan")
    parser.add_argument("--media", help="One synthetic PNG inside the owned UI fixtures directory")
    parser.add_argument("--document", help="One synthetic text document for the owned simulator's local Files provider")
    parser.add_argument("--photo-label", help="Exact previously observed accessibility label of the synthetic photo")
    parser.add_argument("--upload-id", help="Explicit fresh attempt ID after observing any earlier upload result")
    args = parser.parse_args()
    os.umask(0o077)
    root = pathlib.Path.cwd().resolve()
    if str(root.parent) != "/Users/mewgull/MewDex/.state" or not root.name.startswith("native-risk-"):
        raise RuntimeError("Use the existing MewDex native development checkout")
    app = pathlib.Path(args.app).resolve(strict=True)
    with (app / "Info.plist").open("rb") as stream:
        info = plistlib.load(stream)
    if not app.is_relative_to(root) or info.get("CFBundleIdentifier") != "app.mewdex.client" or "iPhoneSimulator" not in info.get("CFBundleSupportedPlatforms", []):
        raise RuntimeError("Choose this project's existing MewDex simulator archive")
    binary_sha = hashlib.sha256((app / info["CFBundleExecutable"]).read_bytes()).hexdigest()
    work = root / ".state/ios-ui-network-risk"
    if work.is_symlink() or work.resolve() != work:
        raise RuntimeError("UI evidence must remain in its project directory")
    work.mkdir(exist_ok=True)
    guard = (work / "run.lock").open("a+")
    fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
    retained_path = work / "device.json"
    report_path = work / (args.mode + "-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d-%H%M%S") + ".json")

    def command(argv, **options):
        result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=options.pop("timeout", 90), **options)
        if result.returncode:
            raise RuntimeError("Command failed: " + " ".join(argv[:3]) + ": " + result.stderr[-2000:])
        return result.stdout.strip()

    def devices():
        values = json.loads(command(["xcrun", "simctl", "list", "devices", "--json"]))["devices"]
        return [dict(device, runtime=runtime) for runtime, group in values.items() for device in group]

    def write(path, value):
        temporary = path.with_suffix(".tmp")
        temporary.write_text(json.dumps(value, indent=2) + "\n")
        os.replace(temporary, path)

    all_devices = devices()
    if retained_path.exists():
        retained = json.loads(retained_path.read_text())
        matches = [device for device in all_devices if device["name"] == retained["name"] and device["udid"] not in retained["baselineIds"]]
        if len(matches) != 1 or (retained.get("deviceId") and matches[0]["udid"] != retained["deviceId"]):
            raise RuntimeError("The retained UI simulator could not be confirmed; no replacement was created")
        retained["deviceId"] = matches[0]["udid"]
    else:
        retained = {"format": 1, "name": "MewDex Input UI Risk " + uuid.uuid4().hex[:8],
                    "baselineIds": sorted(device["udid"] for device in all_devices), "state": "creating"}
        write(retained_path, retained)
        retained["deviceId"] = command(["xcrun", "simctl", "create", retained["name"], "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro", "com.apple.CoreSimulator.SimRuntime.iOS-26-5"])
    retained["state"] = "created"
    write(retained_path, retained)
    device_id = retained["deviceId"]
    target = next(device for device in devices() if device["udid"] == device_id)
    if target["state"] != "Shutdown":
        raise RuntimeError("The owned UI simulator is already active; inspect it before another run")
    report = {"format": 1, "mode": args.mode, "deviceId": device_id, "deviceName": retained["name"],
              "app": str(app), "binarySha256": binary_sha, "defaultSetOwnedDeviceAdded": True,
              "preExistingDevicesTargeted": False, "modelCredentialsAccessed": False,
              "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(), "state": "preparing", "finalCandidate": False}
    write(report_path, report)
    media = None
    if args.mode in ("attachment-gallery", "attachment-upload"):
        supplied = pathlib.Path(args.media or "").absolute()
        media = supplied.resolve(strict=True)
        if supplied != media or media.parent != work / "fixtures" or not media.is_file() or not (0 < media.stat().st_size <= 20 * 1024 * 1024):
            raise RuntimeError("Use one synthetic fixture inside this UI workspace")
        with media.open("rb") as stream:
            if stream.read(8) != b"\x89PNG\r\n\x1a\n":
                raise RuntimeError("The explicit UI fixture must be PNG")
        media_sha = hashlib.sha256(media.read_bytes()).hexdigest()
        media_state_path = work / "media-import.json"
        media_state = {"deviceId": device_id, "sha256": media_sha, "state": "not-imported"}
        if media_state_path.exists():
            media_state = json.loads(media_state_path.read_text())
            if media_state.get("deviceId") != device_id or media_state.get("sha256") != media_sha or media_state.get("state") != "imported":
                raise RuntimeError("Inspect the retained media import before trying another")
        report["syntheticMediaSha256"] = media_sha
    if args.document:
        if args.mode not in ("attachment-files", "attachment-file-upload"):
            raise RuntimeError("Use an explicit file-selector mode for a synthetic document")
        supplied = pathlib.Path(args.document).absolute()
        document = supplied.resolve(strict=True)
        if supplied != document or document.parent != work / "fixtures" or document.suffix != ".txt" or not document.name.startswith("mewdex-mobile-text-"):
            raise RuntimeError("Choose one explicitly labelled text fixture inside this UI workspace")
        raw = document.read_bytes()
        if not (0 < len(raw) <= 1024 * 1024) or not raw.decode("utf-8").startswith("MewDex mobile synthetic text input.\n"):
            raise RuntimeError("The local Files fixture must be bounded synthetic UTF-8 text")
        document_sha = hashlib.sha256(raw).hexdigest()
        containers = pathlib.Path("/Users/mewgull/Library/Developer/CoreSimulator/Devices") / device_id / "data/Containers/Shared/AppGroup"
        providers = [path.parent for path in containers.glob("*/.com.apple.mobile_container_manager.metadata.plist")
                     if plistlib.loads(path.read_bytes()).get("MCMMetadataIdentifier") == "group.com.apple.FileProvider.LocalStorage"]
        if len(providers) != 1:
            raise RuntimeError("Confirm the owned simulator's existing local Files provider first")
        storage = providers[0] / "File Provider Storage"
        if not storage.is_dir() or storage.resolve() != storage or not storage.is_relative_to(containers):
            raise RuntimeError("Local Files storage must remain in this recorded simulator")
        destination = storage / document.name
        imported = work / "document-import.json"
        import_state = {"deviceId": device_id, "sha256": document_sha, "destination": str(destination), "state": "importing"}
        if imported.exists():
            retained_import = json.loads(imported.read_text())
            if retained_import != dict(import_state, state="imported"):
                raise RuntimeError("Observe the retained document import before trying another")
        else:
            if destination.exists() or destination.is_symlink():
                raise RuntimeError("An existing local document was preserved")
            write(imported, import_state)
            with destination.open("xb") as stream:
                stream.write(raw)
                stream.flush()
                os.fsync(stream.fileno())
            import_state["state"] = "imported"
            write(imported, import_state)
        if destination.is_symlink() or hashlib.sha256(destination.read_bytes()).hexdigest() != document_sha:
            raise RuntimeError("The retained synthetic document changed")
        report["syntheticDocumentSha256"] = document_sha
    elif args.mode == "attachment-file-upload":
        raise RuntimeError("A bounded synthetic document is required for file selection")
    plans = list((work / "derived/Build/Products").glob("*.xctestrun"))
    plans = [path for path in plans if not path.name.startswith("MewDex-explicit-")]
    if len(plans) != 1:
        raise RuntimeError("Build exactly one arm64 UI-test runner first")
    with plans[0].open("rb") as stream:
        test_plan = plistlib.load(stream)
    environment = {"MEWDEX_UI_MODE": args.mode}
    if args.document:
        environment["MEWDEX_UI_DOCUMENT_NAME"] = document.name
    if args.mode in ("network", "recover", "viewport", "attachment-picker", "attachment-files", "attachment-file-upload", "attachment-gallery", "attachment-upload") or args.mode.startswith("notification-"):
        path = pathlib.Path(args.plan or "").resolve(strict=True)
        if not path.is_relative_to(work) or path.is_symlink():
            raise RuntimeError("The explicit network plan must be inside the owned UI workspace")
        plan = json.loads(path.read_text())
        if args.mode == "network":
            if plan.get("submitted"):
                raise RuntimeError("A UI submission was already armed; observe its existing receipts without rerunning it")
            plan["submitted"] = True
            plan["armedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
            write(path, plan)
            report["planArmed"] = True
        elif not plan.get("submitted"):
            raise RuntimeError("Read-only UI recovery requires an existing submitted plan")
        report["modelTurnSubmissionAllowed"] = args.mode == "network"
        environment.update({"MEWDEX_UI_HUB": plan["baseURL"], "MEWDEX_UI_PAIR_CODE": plan["pairCode"],
                            "MEWDEX_UI_NAME": plan["inputName"], "MEWDEX_UI_PROMPT": plan["prompt"], "MEWDEX_UI_REPLY": plan["expectedReply"],
                            "MEWDEX_UI_ALREADY_PAIRED": "1" if plan.get("alreadyPaired") else "0"})
    if args.mode in ("attachment-upload", "attachment-file-upload"):
        if args.mode == "attachment-upload" and (not args.photo_label or len(args.photo_label) > 256 or any(ord(c) < 32 for c in args.photo_label)):
            raise RuntimeError("Provide the exact previously observed synthetic photo label")
        upload_id = args.upload_id or "attempt"
        if upload_id != "attempt" and (len(upload_id) != 32 or any(c not in "0123456789abcdef" for c in upload_id)):
            raise RuntimeError("Use a new explicit 32-character upload attempt ID")
        upload_attempt = work / (args.mode + "-" + upload_id + ".json")
        with upload_attempt.open("x") as stream:
            json.dump({"deviceId": device_id, "sha256": media_sha if args.mode == "attachment-upload" else document_sha, "state": "armed", "modelInputAllowed": False}, stream)
        if args.mode == "attachment-upload":
            environment["MEWDEX_UI_PHOTO_LABEL"] = args.photo_label
        report["syntheticUploadAllowed"] = True
        report["uploadAttempt"] = upload_id
    if args.mode in ("notification-warm", "notification-cold", "notification-cold-open", "notification-offline"):
        if not re.fullmatch(r"[0-9a-f]{32}-[1-4]", args.notification_id or ""):
            raise RuntimeError("Provide the explicit retained no-model fixture event ID")
        environment["MEWDEX_UI_NOTIFICATION_ID"] = args.notification_id
        report["syntheticNotificationEvent"] = args.notification_id
    for configuration in test_plan.get("TestConfigurations", []):
        for target in configuration.get("TestTargets", []):
            target.setdefault("EnvironmentVariables", {}).update(environment)
    # xcodebuild currently emits the original (dictionary target) format for
    # this standalone runner. Support both explicit Apple xctestrun formats.
    for name, target in test_plan.items():
        if isinstance(target, dict) and target.get("IsUITestBundle"):
            target.setdefault("EnvironmentVariables", {}).update(environment)
    explicit = plans[0].with_name("MewDex-explicit-" + report_path.stem + ".xctestrun")
    with explicit.open("xb") as stream:
        plistlib.dump(test_plan, stream)
    boot_requested = False
    try:
        boot_requested = True
        command(["xcrun", "simctl", "boot", device_id])
        command(["xcrun", "simctl", "bootstatus", device_id, "-b"])
        command(["xcrun", "simctl", "install", device_id, str(app)])
        if media is not None:
            if media_state["state"] != "imported":
                media_state["state"] = "importing"
                write(media_state_path, media_state)
                command(["xcrun", "simctl", "addmedia", device_id, str(media)])
                media_state["state"] = "imported"
                write(media_state_path, media_state)
            report["syntheticMediaImported"] = True
        report.update(state="running", phase="ui-test")
        write(report_path, report)
        output = work / (report_path.stem + ".log")
        result_path = work / (report_path.stem + ".xcresult")
        with output.open("x") as log:
            result = subprocess.run(["xcodebuild", "test-without-building", "-xctestrun", str(explicit),
                                     "-destination", "platform=iOS Simulator,id=" + device_id,
                                     "-destination-timeout", "15", "-parallel-testing-enabled", "NO",
                                     "-maximum-concurrent-test-simulator-destinations", "1", "-resultBundlePath", str(result_path),
                                     "-disableAutomaticPackageResolution"], stdout=log, stderr=log, timeout=240)
        report.update(state="succeeded" if result.returncode == 0 else "failed", exitCode=result.returncode,
                      log=str(output), resultBundle=str(result_path))
        screenshot = work / (report_path.stem + ".png")
        command(["xcrun", "simctl", "io", device_id, "screenshot", str(screenshot)])
        report["screenshot"] = str(screenshot)
        if args.mode.startswith("notification-"):
            # Only this owned simulator's system routing for this app. Do not
            # collect webview/network logs or another application's messages.
            system_log = work / (report_path.stem + "-system.log")
            with system_log.open("x") as log:
                diagnostic = subprocess.run(["xcrun", "simctl", "spawn", device_id, "log", "show", "--last", "3m", "--style", "compact", "--info", "--predicate",
                    '(process == "SpringBoard" OR process == "runningboardd" OR process == "usernotificationsd") AND eventMessage CONTAINS "app.mewdex.client"'],
                    stdout=log, stderr=log, text=True, timeout=30)
            report["systemRoutingLog"] = str(system_log)
            report["systemRoutingLogExitCode"] = diagnostic.returncode
    except Exception as error:
        report.update(state="failed", error=str(error))
        raise
    finally:
        if boot_requested:
            try:
                command(["xcrun", "simctl", "shutdown", device_id])
                report["simulatorStopped"] = True
            except Exception as error:
                report.update(simulatorStopped=False, stopError=str(error))
        report["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        report["baselineDevicesRetained"] = set(retained["baselineIds"]).issubset({device["udid"] for device in devices()})
        write(report_path, report)
        print(json.dumps(report), flush=True)
    if report["state"] != "succeeded" or not report.get("simulatorStopped"):
        raise RuntimeError("Inspect the retained UI evidence before another run")


if __name__ == "__main__":
    main()
