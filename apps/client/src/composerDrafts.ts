import type { Attachment, Device, Session } from "@mewdex/sdk";
import type { AttachmentUpload } from "./attachmentUploads";

export type ComposerDraft = { text: string; uploads: Attachment[] };
export type ComposerDrafts = Record<string, ComposerDraft>;

export const composerKey = (
  deviceId: string,
  sessionId: string,
  projectId: string,
) => JSON.stringify([deviceId, sessionId, sessionId ? "" : projectId]);

// A receipt clears only its submitted content. Edits and attachments added while
// waiting remain in the original draft even when another conversation is open.
export function completeComposerDraft(
  draft: ComposerDraft,
  text: string,
  attachmentIds: readonly string[],
): ComposerDraft {
  return {
    text: draft.text === text ? "" : draft.text,
    uploads: draft.uploads.filter(
      (upload) => !attachmentIds.includes(upload.id),
    ),
  };
}

export interface DraftEntry {
  key: string;
  deviceId: string;
  sessionId: string;
  projectId: string;
  deviceName: string;
  projectName: string;
  title: string;
  text: string;
  attachmentNames: string[];
  uploadedCount: number;
  pendingCount: number;
  failedCount: number;
  canOpen: boolean;
  canEdit: boolean;
  notice: string;
}

export function draftDestination(
  key: string,
  devices: readonly Device[],
  sessions: readonly Session[],
) {
  const [deviceId, sessionId, savedProject] = JSON.parse(key) as [
    string,
    string,
    string,
  ];
  const device = devices.find((item) => item.id === deviceId);
  const session = sessionId
    ? sessions.find(
        (item) => item.id === sessionId && item.deviceId === deviceId,
      )
    : undefined;
  const projectId = session?.projectId ?? savedProject;
  const project = device?.projects.find((item) => item.id === projectId);
  const retired = session?.executionRecovery?.state === "retired";
  return {
    deviceId,
    sessionId,
    projectId,
    device,
    session,
    project,
    canOpen: !!device && (sessionId ? !!session : !!project),
    canEdit: !!device && !!project && (!sessionId || !!session) && !retired,
    notice: !device
      ? "原设备暂不可用，草稿仍保留在本机。"
      : sessionId && !session
        ? "原对话暂不可用，可复制草稿内容。"
        : !project
          ? session
            ? "原项目暂不可用，可查看对话或复制草稿内容。"
            : "原项目暂不可用，可复制草稿内容。"
          : retired
            ? "原执行已结束，可查看对话或复制草稿内容。"
            : session?.archived
              ? "对话已归档，恢复后可发送。"
              : !device.online
                ? "设备离线，可以继续编辑草稿。"
                : "",
  };
}

// A local projection of saved input and transient transfers. No server query,
// navigation, content mutation or execution happens while building this list.
export function draftEntries(
  drafts: Readonly<ComposerDrafts>,
  uploads: readonly AttachmentUpload[],
  devices: readonly Device[],
  sessions: readonly Session[],
): DraftEntry[] {
  const transfers = new Map<string, AttachmentUpload[]>();
  for (const upload of uploads) {
    const items = transfers.get(upload.target.draftKey) ?? [];
    items.push(upload);
    transfers.set(upload.target.draftKey, items);
  }
  const keys = new Set([...Object.keys(drafts), ...transfers.keys()]);
  return [...keys]
    .map((key) => {
      const draft = drafts[key];
      const pending = transfers.get(key) ?? [];
      const target = draftDestination(key, devices, sessions);
      return {
        key,
        deviceId: target.deviceId,
        sessionId: target.sessionId,
        projectId: target.projectId,
        deviceName: target.device?.name ?? "设备暂不可用",
        projectName: target.project?.name ?? "项目暂不可用",
        title:
          target.session?.title ??
          (target.sessionId ? "原对话草稿" : "新对话草稿"),
        text: draft?.text ?? "",
        attachmentNames: [
          ...(draft?.uploads.map((file) => file.name) ?? []),
          ...pending.map((file) => file.name),
        ],
        uploadedCount: draft?.uploads.length ?? 0,
        pendingCount: pending.length,
        failedCount: pending.filter((file) => file.state === "failed").length,
        canOpen: target.canOpen,
        canEdit: target.canEdit,
        notice: target.notice,
      };
    })
    .sort(
      (a, b) =>
        a.deviceName.localeCompare(b.deviceName, "zh-CN") ||
        a.projectName.localeCompare(b.projectName, "zh-CN") ||
        a.title.localeCompare(b.title, "zh-CN"),
    );
}

export function filterDraftEntries(
  entries: readonly DraftEntry[],
  query: string,
): readonly DraftEntry[] {
  const needle = query.trim().toLocaleLowerCase();
  return needle
    ? entries.filter((entry) =>
        [
          entry.title,
          entry.deviceName,
          entry.projectName,
          entry.text,
          ...entry.attachmentNames,
        ]
          .join("\n")
          .toLocaleLowerCase()
          .includes(needle),
      )
    : entries;
}
