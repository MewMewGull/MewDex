import { t } from "./localization.ts";
import type { Attachment } from "@mewdex/sdk";

export const maxAttachmentBytes = 20 * 1024 * 1024;

export interface UploadTarget {
  draftKey: string;
  deviceId: string;
  projectId: string;
  sessionId: string;
}

export interface AttachmentUpload {
  id: string;
  target: Readonly<UploadTarget>;
  name: string;
  size: number;
  state: "queued" | "uploading" | "ready" | "failed";
  error?: string;
  retryable: boolean;
}

interface Transfer {
  view: AttachmentUpload;
  file?: File;
  controller?: AbortController;
  attachment?: Attachment;
}

type Upload = (
  file: File,
  target: Readonly<UploadTarget>,
  signal: AbortSignal,
) => Promise<Attachment>;
type Complete = (
  target: Readonly<UploadTarget>,
  attachment: Attachment,
) => void;

// The queue owns transient File objects. Only successful attachment references
// reach the original draft; navigation never changes their target or order.
export class AttachmentUploads {
  private nextId = 0;
  private transfers = new Map<string, Transfer>();
  private snapshot: readonly AttachmentUpload[] = [];
  private listeners = new Set<() => void>();

  constructor(
    private readonly upload: Upload,
    private readonly complete: Complete,
  ) {}

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.snapshot;

  private publish() {
    this.snapshot = [...this.transfers.values()].map(
      (transfer) => transfer.view,
    );
    for (const listener of this.listeners) listener();
  }

  add(files: Iterable<File>, target: UploadTarget) {
    for (const file of files) {
      const oversized = file.size > maxAttachmentBytes;
      const id = `attachment-upload-${++this.nextId}`;
      this.transfers.set(id, {
        file: oversized ? undefined : file,
        view: {
          id,
          target: { ...target },
          name: file.name,
          size: file.size,
          state: oversized ? "failed" : "queued",
          retryable: !oversized,
          error: oversized
            ? t("单个附件不能超过 20 MiB，请移除后选择较小文件。")
            : undefined,
        },
      });
    }
    this.publish();
    this.pump();
  }

  retry(id: string) {
    const transfer = this.transfers.get(id);
    if (
      !transfer ||
      transfer.view.state !== "failed" ||
      !transfer.view.retryable
    )
      return;
    transfer.view = { ...transfer.view, state: "queued", error: undefined };
    this.publish();
    this.pump();
  }

  move(from: string, to: string, sessionId: string) {
    for (const transfer of this.transfers.values()) {
      if (transfer.view.target.draftKey === from)
        transfer.view = {
          ...transfer.view,
          target: { ...transfer.view.target, draftKey: to, sessionId },
        };
    }
    this.deliver(to);
    this.publish();
  }

  remove(id: string) {
    const transfer = this.transfers.get(id);
    if (!transfer) return;
    this.transfers.delete(id);
    transfer.controller?.abort();
    this.deliver(transfer.view.target.draftKey);
    this.publish();
    this.pump();
  }

  clear() {
    const transfers = [...this.transfers.values()];
    this.transfers.clear();
    for (const transfer of transfers) transfer.controller?.abort();
    this.publish();
  }

  private pump() {
    const transfers = [...this.transfers.values()];
    const active = transfers.filter(
      (transfer) => transfer.view.state === "uploading",
    ).length;
    const next = transfers
      .filter((transfer) => transfer.view.state === "queued")
      .slice(0, 2 - active);
    for (const transfer of next) {
      transfer.controller = new AbortController();
      transfer.view = { ...transfer.view, state: "uploading" };
    }
    if (next.length) this.publish();
    for (const transfer of next) void this.run(transfer, transfer.controller!);
  }

  private deliver(draftKey: string) {
    for (const [id, transfer] of this.transfers) {
      if (transfer.view.target.draftKey !== draftKey) continue;
      if (transfer.view.state !== "ready") break;
      this.complete(transfer.view.target, transfer.attachment!);
      this.transfers.delete(id);
    }
  }

  private async run(transfer: Transfer, controller: AbortController) {
    const owned = () =>
      this.transfers.get(transfer.view.id) === transfer &&
      transfer.controller === controller;
    try {
      const attachment = await this.upload(
        transfer.file!,
        transfer.view.target,
        controller.signal,
      );
      if (!owned()) return;
      if (
        attachment.targetDeviceId !== transfer.view.target.deviceId ||
        attachment.targetProjectId !== transfer.view.target.projectId
      )
        throw new Error(
          t("上传结果与原执行设备或项目不一致，附件未加入草稿。"),
        );
      transfer.attachment = attachment;
      transfer.file = undefined;
      transfer.view = { ...transfer.view, state: "ready" };
    } catch (error) {
      if (!owned()) return;
      transfer.view = {
        ...transfer.view,
        state: "failed",
        error: error instanceof Error ? error.message : String(error),
      };
    } finally {
      if (owned()) {
        transfer.controller = undefined;
        this.deliver(transfer.view.target.draftKey);
        this.publish();
        this.pump();
      }
    }
  }
}
