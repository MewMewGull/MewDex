import { test } from "node:test";
import assert from "node:assert/strict";
import {
  deviceMatches,
  orderedDevices,
  quickSwitchGroups,
} from "../src/workspaceNavigation.ts";
import { deviceDescription } from "../src/devicePresence.ts";

test("default devices prefer usable execution while preserving explicit location and visible history targets", () => {
  const project = { id: "project", name: "My project" };
  const hub = {
    id: "hub",
    name: "A relay",
    platform: "windows/amd64",
    roles: ["hub", "gateway"],
    projects: [],
    online: true,
  };
  const empty = {
    ...hub,
    id: "empty",
    name: "B empty",
    roles: ["executor"],
  };
  const offline = {
    ...hub,
    id: "offline",
    name: "Mac Mini",
    platform: "darwin/arm64",
    roles: ["executor"],
    projects: [project],
    online: false,
  };
  const ready = {
    ...offline,
    id: "ready",
    name: "Z workstation",
    platform: "windows/amd64",
    online: true,
  };
  const devices = [hub, empty, offline, ready];
  assert.deepEqual(
    orderedDevices(devices).map((item) => item.id),
    ["ready", "offline", "hub", "empty"],
  );
  assert.equal(orderedDevices(devices, hub.id)[0].id, hub.id);
  assert.deepEqual(
    devices.map((item) => item.id),
    ["hub", "empty", "offline", "ready"],
  );
  assert.equal(deviceMatches("  MACOS mini ", offline), true);
  assert.equal(deviceMatches("windows workstation", ready), true);
  assert.equal(deviceMatches("macos workstation", ready), false);
  assert.match(deviceDescription(hub, true), /未开启执行能力/);
  assert.match(deviceDescription(empty, true), /暂无可用项目/);
  assert.match(deviceDescription(ready, false), /上次在线/);

  const groups = quickSwitchGroups({
    devices,
    sessions: [],
    search: "",
    deviceId: "",
    unreadSessions: new Set(),
  });
  assert.deepEqual(
    groups
      .find((group) => group.id === "devices")
      .entries.map((item) => item.device.id),
    ["ready", "offline", "hub", "empty"],
  );
});
