import { t } from "../localization.ts";
import { useState } from "react";
import type { MewDexClient } from "@mewdex/sdk";
import {
  AlertCircle,
  Check,
  FileText,
  LoaderCircle,
  RotateCcw,
  X,
} from "lucide-react";
import type { AttachmentUpload } from "../attachmentUploads";
import { formatBytes } from "../formatBytes";
import { canPreviewImage } from "../attachmentFiles";
import { AttachmentPreview } from "./AttachmentList";
import { AttachmentThumbnail } from "./AttachmentThumbnail";

export function AttachmentUploadList({
  client,
  items,
  onRetry,
  onRemove,
}: {
  client: MewDexClient;
  items: readonly AttachmentUpload[];
  onRetry: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  const [opened, setOpened] = useState<AttachmentUpload>();
  if (!items.length && !opened) return null;
  const failed = items.filter((item) => item.state === "failed").length;
  return (
    <>
      <div
        className="attachment-upload-list"
        aria-label={t("待处理附件")}
        hidden={!items.length}
      >
        <div className="attachment-upload-items">
          {items.map((item) => {
            const image = canPreviewImage(item.preview?.type ?? "");
            return (
              <article
                key={item.id}
                className={`attachment-upload-item is-${item.state} ${image ? "is-image" : ""}`}
                title={item.error ? `${item.name}\n${item.error}` : item.name}
              >
                {image && item.preview && (
                  <button
                    className="attachment-upload-preview"
                    aria-label={t("查看附件 {0}", item.name)}
                    onClick={() => setOpened(item)}
                  >
                    <AttachmentThumbnail
                      content={item.preview}
                      mediaType={item.preview.type}
                      name={item.name}
                    />
                  </button>
                )}
                <span className="attachment-upload-icon">
                  {item.state === "uploading" ? (
                    <LoaderCircle size={16} className="spinning" />
                  ) : item.state === "failed" ? (
                    <AlertCircle size={16} />
                  ) : item.state === "ready" ? (
                    <Check size={16} />
                  ) : (
                    <FileText size={16} />
                  )}
                </span>
                <span className="attachment-upload-copy">
                  <strong
                    title={item.name}
                    className={image ? "sr-only" : undefined}
                  >
                    {item.name}
                  </strong>
                  <small>
                    {!image && <>{formatBytes(item.size)} · </>}
                    {item.state === "uploading"
                      ? t("正在上传")
                      : item.state === "queued"
                        ? t("等待上传")
                        : item.state === "ready"
                          ? t("已上传，等待前面的附件处理")
                          : t("上传失败")}
                  </small>
                  {item.error && (
                    <span
                      className={`attachment-upload-error ${image ? "sr-only" : ""}`}
                    >
                      {item.error}
                    </span>
                  )}
                </span>
                {item.state === "failed" && item.retryable && (
                  <button
                    className="icon-button attachment-upload-retry"
                    aria-label={t("重试上传 {0}", item.name)}
                    title={t("重试上传")}
                    onClick={() => onRetry(item.id)}
                  >
                    <RotateCcw size={14} />
                  </button>
                )}
                <button
                  className="icon-button"
                  aria-label={`${item.state === "queued" || item.state === "uploading" ? t("取消上传") : t("移除待处理附件")} ${item.name}`}
                  title={
                    item.state === "queued" || item.state === "uploading"
                      ? t("取消上传")
                      : t("移除")
                  }
                  onClick={() => onRemove(item.id)}
                >
                  <X size={14} />
                </button>
              </article>
            );
          })}
        </div>
        <p className="attachment-upload-note" role="status">
          {failed
            ? t("{0} 个附件未上传，请重试或移除后发送。", failed)
            : t("附件准备完成后可发送；切换对话会继续上传到原草稿。")}
        </p>
      </div>
      {opened?.preview && (
        <AttachmentPreview
          client={client}
          file={{
            id: opened.id,
            name: opened.name,
            mediaType: opened.preview.type,
            size: opened.size,
          }}
          initialContent={opened.preview}
          onClose={() => setOpened(undefined)}
        />
      )}
    </>
  );
}
