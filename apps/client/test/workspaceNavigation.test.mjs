import { test } from "node:test";
import assert from "node:assert/strict";
import {
  deviceMatches,
  conversationHubProject,
  orderedDevices,
  quickSwitchGroups,
} from "../src/workspaceNavigation.ts";
import { deviceDescription } from "../src/devicePresence.ts";

test("legacy conversations find their Hub project after a verified device identity replacement", () => {
  const old = {
    id: "old-mac",
    platform: "darwin/arm64",
    projects: [{ id: "old-project", path: "/Users/me/work" }],
    roles: ["executor"],
  };
  const current = {
    id: "current-mac",
    platform: "darwin/arm64",
    projects: [],
    roles: ["executor"],
  };
  const project = {
    id: "hp_work",
    directories: [
      {
        deviceId: current.id,
        projectId: "new-project",
        path: "/Users/me/work",
      },
    ],
  };
  const hub = { id: "hub", roles: ["coordinator"], projects: [project] };
  const session = { deviceId: old.id, projectId: "old-project" };
  assert.equal(conversationHubProject([old, current, hub], session), project);
  assert.equal(
    conversationHubProject(
      [
        old,
        current,
        { ...hub, projects: [project, { ...project, id: "hp_other" }] },
      ],
      session,
    ),
    undefined,
  );
  assert.equal(
    conversationHubProject(
      [old, { ...current, platform: "linux/amd64" }, hub],
      session,
    ),
    undefined,
  );
  assert.equal(
    conversationHubProject([old, current, hub], {
      deviceId: hub.id,
      projectId: project.id,
    }),
    project,
  );
});

test("Hub navigation exposes project groups across machines without a device choice", () => {
  const hub = {
    id: "central",
    name: "Hub",
    platform: "hub",
    roles: ["hub", "coordinator", "executor"],
    online: true,
    projects: [
      {
        id: "hp_sott",
        name: "Sott",
        path: "",
        directories: [
          {
            deviceId: "windows",
            projectId: "game",
            path: "D:\\Sott",
            name: "Windows",
          },
          {
            deviceId: "mac",
            projectId: "game",
            path: "/Users/me/Sott",
            name: "Mac",
          },
        ],
      },
    ],
  };
  const node = {
    id: "windows",
    name: "Win",
    platform: "windows",
    roles: ["executor"],
    online: true,
    projects: [{ id: "game", name: "Local binding", path: "D:\\Sott" }],
  };
  const groups = quickSwitchGroups({
    devices: [node, hub],
    sessions: [],
    search: "",
    deviceId: node.id,
    unreadSessions: new Set(),
  });
  assert.deepEqual(
    groups.map((group) => group.id),
    ["projects"],
  );
  assert.equal(groups[0].entries.length, 1);
  assert.equal(groups[0].entries[0].project.id, "hp_sott");
  assert.equal(groups[0].entries[0].device.id, hub.id);
  assert.equal(orderedDevices([node, hub])[0].id, hub.id);
});

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
