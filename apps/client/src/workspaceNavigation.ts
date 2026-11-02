import type { Device, Project, Session } from "@mewdex/sdk";
import { devicePlatform } from "./devicePresence.ts";

export function navigationMatches(search: string, ...labels: string[]) {
  const text = labels.join(" ").toLocaleLowerCase();
  return search
    .trim()
    .toLocaleLowerCase()
    .split(/\s+/u)
    .every((word) => text.includes(word));
}

export function deviceMatches(search: string, device: Device) {
  return navigationMatches(
    search,
    device.name,
    device.platform,
    devicePlatform(device.platform),
  );
}

export function orderedDevices(devices: Device[], currentId = "") {
  const executable = (device: Device) =>
    device.roles.includes("executor") && device.projects.length > 0;
  return [...devices].sort(
    (a, b) =>
      Number(b.id === currentId) - Number(a.id === currentId) ||
      Number(executable(b)) - Number(executable(a)) ||
      Number(b.online) - Number(a.online) ||
      a.name.localeCompare(b.name) ||
      a.id.localeCompare(b.id),
  );
}

export function conversationContext(session: Session, device?: Device) {
  return {
    deviceName: device?.name ?? "未连接设备",
    projectName:
      device?.projects.find((project) => project.id === session.projectId)
        ?.name ?? session.projectId,
  };
}

export type SwitchEntry =
  | { kind: "device"; id: string; device: Device }
  | { kind: "project"; id: string; device: Device; project: Project }
  | {
      kind: "session";
      id: string;
      session: Session;
      deviceName: string;
      projectName: string;
    };

export interface SwitchGroup {
  id: string;
  label: string;
  total: number;
  entries: SwitchEntry[];
}

export function quickSwitchGroups({
  devices,
  sessions,
  search,
  deviceId,
  unreadSessions,
}: {
  devices: Device[];
  sessions: Session[];
  search: string;
  deviceId: string;
  unreadSessions: ReadonlySet<string>;
}): SwitchGroup[] {
  const searching = !!search.trim();
  const byId = new Map(devices.map((device) => [device.id, device]));
  const ordered = orderedDevices(devices, deviceId);
  const targets: SwitchEntry[] = ordered
    .filter((device) => deviceMatches(search, device))
    .map((device) => ({ kind: "device", id: `device:${device.id}`, device }));
  const projects: SwitchEntry[] = ordered.flatMap((device) =>
    searching || device.id === deviceId
      ? device.projects
          .filter((project) =>
            navigationMatches(search, device.name, project.name),
          )
          .map((project) => ({
            kind: "project" as const,
            id: `project:${JSON.stringify([device.id, project.id])}`,
            device,
            project,
          }))
      : [],
  );
  const conversations: SwitchEntry[] = sessions
    .map((session) => ({
      kind: "session" as const,
      id: `session:${session.id}`,
      session,
      ...conversationContext(session, byId.get(session.deviceId)),
    }))
    .filter(
      (entry) =>
        (!entry.session.archived ||
          searching ||
          unreadSessions.has(entry.session.id)) &&
        navigationMatches(
          search,
          entry.session.title,
          entry.deviceName,
          entry.projectName,
        ),
    )
    .sort(
      (a, b) =>
        Number(!!b.session.pinned) - Number(!!a.session.pinned) ||
        b.session.updatedAt.localeCompare(a.session.updatedAt),
    );
  const targetGroup = {
    id: "devices",
    label: "设备",
    total: targets.length,
    entries: targets,
  };
  const projectGroup = {
    id: "projects",
    label: searching ? "项目 · 新对话" : "当前设备 · 新对话",
    total: projects.length,
    entries: projects,
  };
  const conversationGroup = {
    id: "sessions",
    label: searching ? "对话" : "最近对话",
    total: conversations.length,
    entries: searching ? conversations : conversations.slice(0, 12),
  };
  return (
    searching
      ? [targetGroup, projectGroup, conversationGroup]
      : [targetGroup, conversationGroup, projectGroup]
  ).filter((group) => group.entries.length > 0);
}
