import { test } from "node:test";
import assert from "node:assert/strict";
import { fillSSHProfile } from "../src/hub-controller/hubProxySSH.ts";

test("SSH selection retains the listener, enable switch and legacy metadata", () => {
  const current = {
    enabled: false,
    host: "old-host",
    port: 22,
    user: "old-user",
    identityFile: "old-key",
    knownHostsFile: "old-hosts",
    publicUrl: "https://mewdex.example.com",
    remoteBind: "127.0.0.1",
    remotePort: 55001,
  };
  const profile = {
    alias: "chosen-host",
    host: "ssh.example.com",
    port: 2222,
    user: "deploy",
    identityFile: "C:\\Users\\Demo\\.ssh\\key",
    knownHostsFile: "C:\\Users\\Demo\\.ssh\\known_hosts",
    importable: true,
    warnings: [],
  };
  const imported = fillSSHProfile(current, profile);
  assert.equal(imported.host, "ssh.example.com");
  assert.equal(imported.port, 2222);
  assert.equal(imported.user, "deploy");
  assert.equal(imported.identityFile, profile.identityFile);
  assert.equal(imported.knownHostsFile, profile.knownHostsFile);
  assert.equal(imported.enabled, false);
  assert.equal(imported.publicUrl, current.publicUrl);
  assert.equal(imported.remoteBind, current.remoteBind);
  assert.equal(imported.remotePort, 55001);
  imported.host = "manually-overridden.example.com";
  assert.equal(current.host, "old-host");
  assert.equal(profile.host, "ssh.example.com");
  assert.throws(
    () => fillSSHProfile(current, { ...profile, importable: false }),
    /手动/,
  );
});
