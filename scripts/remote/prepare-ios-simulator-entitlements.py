#!/usr/bin/env python3
"""Prepare simulator-only link-time entitlements in an owned generated project.

The unsigned simulator target needs the simulated entitlement sections used by
Xcode. Hardware/Release configurations and all signing accounts are untouched.
"""
import argparse
import hashlib
import json
import os
import pathlib
import platform
import plistlib
import struct
import subprocess
import uuid


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--inspect-app")
    args = parser.parse_args()
    if platform.system() != "Darwin":
        raise RuntimeError("Simulator setup requires the configured Mac")
    repo = pathlib.Path.cwd().resolve()
    project = repo / "apps/client/src-tauri/gen/apple/mewdex-client.xcodeproj"
    pbx = project / "project.pbxproj"
    work = repo / ".state/ios-simulator-entitlements"
    work.mkdir(parents=True, exist_ok=True)
    if work.resolve() != work or not pbx.is_file():
        raise RuntimeError("Use the existing generated project inside this workspace")
    xml, der = work / "simulator.xcent", work / "simulator.xcent.der"
    expected = {"application-identifier": "app.mewdex.client", "keychain-access-groups": ["app.mewdex.client"]}
    xml_bytes = plistlib.dumps(expected)
    if xml.exists() and xml.read_bytes() != xml_bytes:
        raise RuntimeError("Existing simulator entitlements changed; they were preserved")
    if not xml.exists():
        with xml.open("xb") as stream:
            stream.write(xml_bytes)
    temporary_der = work / ("simulator." + uuid.uuid4().hex + ".der.tmp")
    try:
        subprocess.run(["/usr/bin/derq", "query", "-f", "xml", "-i", str(xml), "-o", str(temporary_der), "--raw"], check=True)
        if der.exists():
            if der.read_bytes() != temporary_der.read_bytes():
                raise RuntimeError("Simulator DER entitlements do not match their source")
        else:
            os.link(temporary_der, der)
    finally:
        if temporary_der.exists():
            temporary_der.unlink()
    record_path = work / "setup.json"
    if args.inspect_app:
        app = pathlib.Path(args.inspect_app).resolve(strict=True)
        if not app.is_relative_to(repo):
            raise RuntimeError("Inspect only this project's simulator output")
        info = plistlib.load((app / "Info.plist").open("rb"))
        if info.get("CFBundleIdentifier") != "app.mewdex.client" or info.get("CFBundleSupportedPlatforms") != ["iPhoneSimulator"]:
            raise RuntimeError("The output is not the MewDex simulator app")
        binary = (app / info["CFBundleExecutable"]).read_bytes()
        if struct.unpack_from("<II", binary) != (0xfeedfacf, 0x0100000c):
            raise RuntimeError("A thin arm64 Mach-O executable is required")
        count = struct.unpack_from("<I", binary, 16)[0]
        offset, sections = 32, {}
        for _ in range(count):
            command, size = struct.unpack_from("<II", binary, offset)
            if command == 0x19:
                section_count = struct.unpack_from("<I", binary, offset + 64)[0]
                for index in range(section_count):
                    section = offset + 72 + index * 80
                    name = binary[section:section+16].split(b"\0")[0].decode()
                    segment = binary[section+16:section+32].split(b"\0")[0].decode()
                    length = struct.unpack_from("<Q", binary, section + 40)[0]
                    start = struct.unpack_from("<I", binary, section + 48)[0]
                    if segment == "__TEXT" and name in ("__entitlements", "__ents_der"):
                        sections[name] = binary[start:start+length]
            offset += size
        if plistlib.loads(sections.get("__entitlements", b"")) != expected or sections.get("__ents_der") != der.read_bytes():
            raise RuntimeError("The simulator binary is missing the prepared entitlement sections")
        print(json.dumps({"simulatorSectionsVerified": True, "binarySha256": digest(binary), "entitlements": expected, "certificateUsed": False, "finalCandidate": False}))
        return
    before = pbx.read_bytes()
    record = json.loads(record_path.read_text()) if record_path.exists() else None
    if record and digest(before) == record["afterSha256"]:
        print(json.dumps(record))
        return
    flags = ["$(inherited)", "-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", str(xml),
             "-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__ents_der", "-Xlinker", str(der)]
    ruby = r'''
v = JSON.parse(STDIN.read)
p = Xcodeproj::Project.open(v.fetch('project'))
targets = p.native_targets.select { |t| t.name == 'mewdex-client_iOS' && t.product_type == 'com.apple.product-type.application' }
raise 'Expected one generated iOS target' unless targets.length == 1
configs = targets[0].build_configurations.select { |c| c.name.casecmp('Debug').zero? }
raise 'Expected one Debug configuration' unless configs.length == 1
key = 'OTHER_LDFLAGS[sdk=iphonesimulator*]'
old = configs[0].build_settings[key]
raise 'Simulator linker flags already exist; inspect them first' unless old.nil? || old == v.fetch('flags')
if old == v.fetch('flags')
  puts JSON.generate({'modified' => false})
  exit
end
configs[0].build_settings[key] = v.fetch('flags')
p.save
puts JSON.generate({'modified' => true})
'''
    # Xcode and the CLI can normalize unrelated project serialization. Inspect
    # this owned setting rather than resetting the entire project by old hash.
    backup = work / ("before-" + digest(before) + ".pbxproj")
    if not backup.exists():
        with backup.open("xb") as stream:
            stream.write(before)
    action = subprocess.run(["ruby", "-rjson", "-rxcodeproj", "-e", ruby], input=json.dumps({"project": str(project), "flags": flags}), text=True, stdout=subprocess.PIPE, check=True)
    if not json.loads(action.stdout)["modified"] and record:
        print(json.dumps(dict(record, observedProjectSha256=digest(pbx.read_bytes()), alreadyConfigured=True)))
        return
    if record:
        history = work / ("setup-" + record["afterSha256"] + ".json")
        if not history.exists():
            with history.open("x") as stream:
                json.dump(record, stream, indent=2)
    record = {"format": 1, "project": str(project), "configuration": "Debug", "sdk": "iphonesimulator*",
              "beforeSha256": digest(before), "afterSha256": digest(pbx.read_bytes()), "entitlements": expected,
              "xmlSha256": digest(xml.read_bytes()), "derSha256": digest(der.read_bytes()),
              "hardwareConfigurationChanged": False, "releaseConfigurationChanged": False,
              "certificateUsed": False, "developerAccountUsed": False, "finalCandidate": False}
    temporary = record_path.with_suffix(".tmp")
    temporary.write_text(json.dumps(record, indent=2) + "\n")
    os.replace(temporary, record_path)
    print(json.dumps(record))


if __name__ == "__main__":
    main()
