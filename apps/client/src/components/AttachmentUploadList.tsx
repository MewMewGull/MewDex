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

export function AttachmentUploadList({
  items,
  onRetry,
  onRemove,
}: {
  items: readonly AttachmentUpload[];
  onRetry: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  if (!items.length) return null;
  const failed = items.filter((item) => item.state === "failed").length;
  return (
    <div className="attachment-upload-list" aria-label="待处理附件">
      <div className="attachment-upload-items">
        {items.map((item) => (
          <article
            key={item.id}
            className={`attachment-upload-item is-${item.state}`}
          >
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
              <strong title={item.name}>{item.name}</strong>
              <small>
                {formatBytes(item.size)} ·{" "}
                {item.state === "uploading"
                  ? "正在上传"
                  : item.state === "queued"
                    ? "等待上传"
                    : item.state === "ready"
                      ? "已上传，等待前面的附件处理"
                      : "上传失败"}
              </small>
              {item.error && (
                <span className="attachment-upload-error">{item.error}</span>
              )}
            </span>
            {item.state === "failed" && item.retryable && (
              <button
                className="icon-button"
                aria-label={`重试上传 ${item.name}`}
                title="重试上传"
                onClick={() => onRetry(item.id)}
              >
                <RotateCcw size={14} />
              </button>
            )}
            <button
              className="icon-button"
              aria-label={`${item.state === "queued" || item.state === "uploading" ? "取消上传" : "移除待处理附件"} ${item.name}`}
              title={
                item.state === "queued" || item.state === "uploading"
                  ? "取消上传"
                  : "移除"
              }
              onClick={() => onRemove(item.id)}
            >
              <X size={14} />
            </button>
          </article>
        ))}
      </div>
      <p className="attachment-upload-note" role="status">
        {failed
          ? `${failed} 个附件未上传，请重试或移除后发送。`
          : "附件准备完成后可发送；切换对话会继续上传到原草稿。"}
      </p>
    </div>
  );
}
