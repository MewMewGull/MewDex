#!/usr/bin/env python3
"""Launch one existing simulator archive in a project-owned, isolated device set.

Never installs tools, downloads runtimes, accesses accounts or uses a real phone.
Existing default simulators are untouched. The owned device is retained shut down.
"""
import argparse
import datetime
import fcntl
import hashlib
import json
import os
import pathlib
import platform
import plistlib
import shutil
import subprocess
import sys
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--app", required=True)
    parser.add_argument("--settle-seconds", type=int, default=15)
    parser.add_argument("--update-owned-app", action="store_true")
    parser.add_argument("--native-probe", action="store_true")
    args = parser.parse_args()
    if args.settle_seconds < 1 or args.settle_seconds > 45:
        raise RuntimeError("Choose an observation interval between 1 and 45 seconds")
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise RuntimeError("This probe requires the configured arm64 Mac")
    repo = pathlib.Path.cwd().resolve()
    app = pathlib.Path(args.app).resolve(strict=True)
    if not app.is_relative_to(repo) or app.suffix != ".app":
        raise RuntimeError("Choose an existing simulator archive inside this project")
    with (app / "Info.plist").open("rb") as stream:
        info = plistlib.load(stream)
    bundle = info.get("CFBundleIdentifier")
    if bundle != "app.mewdex.client" or "iPhoneSimulator" not in info.get("CFBundleSupportedPlatforms", []):
        raise RuntimeError("The archive is not the MewDex simulator input application")
    binary = app / info["CFBundleExecutable"]
    digest = hashlib.sha256()
    with binary.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    work = repo / ".state" / "ios-runtime-risk"
    work.mkdir(parents=True, exist_ok=True)
    if work.is_symlink() or work.resolve() != work:
        raise RuntimeError("Simulator state must be an owned project directory")
    if shutil.disk_usage(work).free < 3 << 30:
        raise RuntimeError("At least 3 GiB of free space is required for the isolated simulator")
    lock = (work / "probe.lock").open("a+")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    device_set = work / "devices"
    device_set.mkdir(exist_ok=True)
    if device_set.is_symlink():
        raise RuntimeError("The device set must not be a symbolic link")
    state_path = work / "probe.json"
    state = {"format": 1, "state": "preparing", "phase": "preflight",
             "pid": os.getpid(),
             "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
             "app": str(app), "bundleId": bundle, "binarySha256": digest.hexdigest(),
             "deviceSet": str(device_set), "defaultDevicesChanged": False,
             "modelCredentialsAccessed": False, "inputPaired": False,
             "inferenceRun": False, "finalCandidate": False}
    previous = json.loads(state_path.read_text()) if state_path.exists() else None
    if previous and (previous.get("deviceSet") != str(device_set) or previous.get("bundleId") != bundle):
        raise RuntimeError("Existing simulator evidence belongs to another device set")
    if previous and previous.get("installed"):
        state["installedBinarySha256"] = previous.get("installedBinarySha256", previous.get("binarySha256"))

    def save(phase, **fields):
        state.update(fields, phase=phase, updatedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
        temporary = state_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(state, indent=2) + "\n")
        os.replace(temporary, state_path)
        print(json.dumps({"phase": phase, "state": state["state"]}), flush=True)

    def command(arguments, output=None, env=None):
        if output:
            with pathlib.Path(output).open("a") as stream:
                subprocess.run(arguments, stdout=stream, stderr=stream, env=env, check=True)
            return ""
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
        if result.returncode:
            raise RuntimeError("Command failed: " + " ".join(arguments[:4]) + ": " + result.stderr[-3000:])
        return result.stdout.strip()

    def sim(*arguments, output=None, env=None):
        return command(["xcrun", "simctl", "--set", str(device_set), *arguments], output, env)

    device_id = None
    device_state = "Shutdown"
    boot_requested = False
    try:
        runtimes = json.loads(command(["xcrun", "simctl", "list", "runtimes", "--json"]))["runtimes"]
        runtime = next((r for r in runtimes if r.get("identifier") == "com.apple.CoreSimulator.SimRuntime.iOS-26-5" and r.get("isAvailable")), None)
        if runtime is None:
            raise RuntimeError("The previously observed iOS 26.5 runtime is unavailable; no download was started")
        devices = json.loads(sim("list", "devices", "--json"))["devices"]
        all_devices = [d for values in devices.values() for d in values]
        name = "MewDex Input Runtime Risk"
        if previous and previous.get("deviceId"):
            device_id = previous["deviceId"]
            found = [d for d in all_devices if d["udid"] == device_id and d["name"] == name]
            if len(found) != 1:
                raise RuntimeError("The retained owned simulator cannot be confirmed; no replacement was created")
            if found[0]["state"] not in ("Shutdown", "Booted"):
                raise RuntimeError("The retained simulator has an unconfirmed operation; inspect it before continuing")
            device_state = found[0]["state"]
        else:
            if any(d["name"] == name for d in all_devices):
                raise RuntimeError("An unrecorded simulator already exists; no duplicate was created")
            device_id = sim("create", name, "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro", runtime["identifier"])
            state["deviceId"] = device_id
            save("created")
        state["deviceId"] = device_id
        save("booting", state="running")
        boot_requested = True
        if device_state == "Shutdown":
            sim("boot", device_id)
        sim("bootstatus", device_id, "-b", output=work / "boot.log")
        save("installing")
        existing = subprocess.run(["xcrun", "simctl", "--set", str(device_set), "get_app_container", device_id, bundle, "app"], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        if existing.returncode == 0:
            installed = pathlib.Path(existing.stdout.strip()) / info["CFBundleExecutable"]
            installed_hash = hashlib.sha256(installed.read_bytes()).hexdigest()
            if installed_hash != state["binarySha256"]:
                if not args.update_owned_app or installed_hash != state.get("installedBinarySha256"):
                    raise RuntimeError("A different input application is installed in the owned simulator; it was preserved")
                # Explicit development update on this owned simulator only.
                # install retains the application data; never uninstall/erase.
                sim("install", device_id, str(app))
                state["ownedAppUpdated"] = True
        else:
            sim("install", device_id, str(app))
        state["installedBinarySha256"] = state["binarySha256"]
        save("launching", installed=True)
        launch_env = os.environ.copy()
        if args.native_probe:
            launch_env["SIMCTL_CHILD_MEWDEX_NATIVE_RISK"] = "1"
        launched = sim("launch", "--stdout=" + str(work / "stdout.log"), "--stderr=" + str(work / "stderr.log"), device_id, bundle, env=launch_env)
        state["launchResult"] = launched
        save("rendering", uiLaunched=True)
        time.sleep(args.settle_seconds)
        evidence = repo / ".state" / "evidence"
        evidence.mkdir(exist_ok=True)
        screenshot = evidence / ("ios-runtime-startup-" + str(args.settle_seconds) + "s.png")
        sim("io", device_id, "screenshot", str(screenshot))
        save("captured", screenshot=str(screenshot), screenshotCaptured=True)
        if args.native_probe:
            observations = {}
            container = pathlib.Path(sim("get_app_container", device_id, bundle, "data")).resolve()
            if not container.is_relative_to(device_set):
                raise RuntimeError("The application's data container is outside the owned simulator")
            files = list(container.rglob("mewdex-development-probe.jsonl"))
            if len(files) != 1 or files[0].is_symlink():
                raise RuntimeError("The current app's explicit native probe record is unavailable")
            with files[0].open("rb") as stream:
                stream.seek(0, 2)
                stream.seek(max(0, stream.tell() - (64 << 10)))
                for line in stream.read().decode("utf-8", errors="replace").splitlines():
                    value = json.loads(line)
                    observations[value["stage"]] = value["observation"]
            state["nativeObservations"] = observations
            storage = observations.get("storage", {})
            capabilities = observations.get("input-capabilities", {})
            startup = observations.get("startup", {})
            if startup.get("pid") != int(launched.rsplit(": ", 1)[-1]) or startup.get("platform") != "ios" or not startup.get("mobile") or not storage.get("roundTrip") or not storage.get("temporaryEntryRemoved") or capabilities != {"localService": False, "helperPresent": False, "enginePresent": False}:
                raise RuntimeError("Native storage or the input-only bridge could not be confirmed; inspect the retained probe evidence")
            save("native-verified", nativeStorageVerified=True, inputOnlyBridgeVerified=True)
        # A captured SpringBoard or launch screen alone is not proof of a usable
        # input. Inspect the pixels separately before recording UI success.
        state["state"] = "captured-awaiting-visual-review"
    except Exception as error:
        save("failed", state="failed", error=str(error))
        if device_id and boot_requested:
            try:
                sim("spawn", device_id, "log", "show", "--last", "3m", "--style", "compact", "--predicate", 'eventMessage CONTAINS "app.mewdex.client" OR process == "MewDex"', output=work / "failure.log")
                state["failureLog"] = str(work / "failure.log")
            except Exception:
                pass
        raise
    finally:
        if device_id and boot_requested:
            try:
                sim("shutdown", device_id)
                save("stopped", simulatorStopped=True)
            except Exception as error:
                save("stop-unconfirmed", stopError=str(error), simulatorStopped=False)
        lock.close()
    print(json.dumps(state), flush=True)


if __name__ == "__main__":
    main()
