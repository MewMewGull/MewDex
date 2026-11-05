import { t } from "./localization.ts";
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
      Number(b.roles.includes("coordinator")) -
        Number(a.roles.includes("coordinator")) ||
      Number(executable(b)) - Number(executable(a)) ||
      Number(b.online) - Number(a.online) ||
      a.name.localeCompare(b.name) ||
      a.id.localeCompare(b.id),
  );
}

export function conversationContext(session: Session, device?: Device) {
  return {
    deviceName: device?.name ?? t("未连接设备"),
    projectName:
      device?.projects.find((project) => project.id === session.projectId)
        ?.name ?? session.projectId,
  };
}

export function conversationHubProject(devices: Device[], session: Session) {
  const hub = devices.find((device) => device.roles.includes("coordinator"));
  if (!hub) return undefined;
  if (session.deviceId === hub.id)
    return hub.projects.find((project) => project.id === session.projectId);
  const direct = hub.projects.find((project) =>
    project.directories?.some(
      (directory) =>
        directory.deviceId === session.deviceId &&
        directory.projectId === session.projectId,
    ),
  );
  if (direct) return direct;
  const originalDevice = devices.find(
    (device) => device.id === session.deviceId,
  );
  const original = originalDevice?.projects.find(
    (project) => project.id === session.projectId,
  );
  if (!originalDevice || !original?.path) return undefined;
  const matches = hub.projects.filter((project) =>
    project.directories?.some(
      (directory) =>
        directory.path === original.path &&
        devices.some(
          (device) =>
            device.id === directory.deviceId &&
            device.platform === originalDevice.platform,
        ),
    ),
  );
  return matches.length === 1 ? matches[0] : undefined;
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
  const central = ordered.find((device) =>
    device.roles.includes("coordinator"),
  );
  const targets: SwitchEntry[] = (central ? [] : ordered)
    .filter((device) => deviceMatches(search, device))
    .map((device) => ({ kind: "device", id: `device:${device.id}`, device }));
  const projects: SwitchEntry[] = (central ? [central] : ordered).flatMap(
    (device) =>
      central || searching || device.id === deviceId
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
    label: t("设备"),
    total: targets.length,
    entries: targets,
  };
  const projectGroup = {
    id: "projects",
    label: central
      ? t("Hub 项目 · 新对话")
      : searching
        ? t("项目 · 新对话")
        : t("当前设备 · 新对话"),
    total: projects.length,
    entries: projects,
  };
  const conversationGroup = {
    id: "sessions",
    label: searching ? t("对话") : t("最近对话"),
    total: conversations.length,
    entries: searching ? conversations : conversations.slice(0, 12),
  };
  return (
    searching
      ? [targetGroup, projectGroup, conversationGroup]
      : [targetGroup, conversationGroup, projectGroup]
  ).filter((group) => group.entries.length > 0);
}
