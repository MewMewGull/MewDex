import { useEffect, useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  Download,
  File,
  FileText,
  Image as ImageIcon,
  LoaderCircle,
  X,
} from "lucide-react";
import type { Attachment, MewDexClient } from "@mewdex/sdk";
import { canSaveAttachment, saveAttachmentFile } from "../attachmentFiles";
import { formatBytes } from "../formatBytes";
import { CopyButton } from "./CopyButton";
import { TextFilePreview } from "./TextFilePreview";
import { decodeTextPreview, type TextPreviewEncoding } from "../textPreview";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

interface AttachmentItem {
  id: string;
  metadata?: Attachment;
  initialLine?: number;
}
const textPreviewLimit = 256 * 1024;
const imageTypes = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/bmp",
]);
const mimeOf = (file: Attachment) => file.mediaType.split(";")[0];

function SaveButton({
  client,
  file,
  content,
}: {
  client: MewDexClient;
  file: Attachment;
  content?: Blob;
}) {
  const support = useQuery({
    queryKey: ["attachment-save-support"],
    queryFn: canSaveAttachment,
    staleTime: Infinity,
  });
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState<"" | "saved" | "download-started">("");
  const [error, setError] = useState("");
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function save() {
    if (controller.current) return;
    const active = new AbortController();
    controller.current = active;
    setBusy(true);
    setError("");
    setSaved("");
    try {
      const blob =
        content ?? (await client.attachmentContent(file.id, active.signal));
      active.signal.throwIfAborted();
      const result = await saveAttachmentFile(file.name, blob);
      setSaved(result === "cancelled" ? "" : result);
    } catch (cause) {
      if (!active.signal.aborted)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      controller.current = null;
      setBusy(false);
    }
  }
  return (
    <div className="file-save-control">
      <button
        className="file-save-button"
        disabled={busy || !support.data}
        aria-label={`保存附件 ${file.name}`}
        title={support.data ? "保存到本机" : "当前输入端暂未提供文件保存"}
        onClick={() => void save()}
      >
        {busy ? (
          <LoaderCircle className="spinning" size={14} />
        ) : saved ? (
          <Check size={14} />
        ) : (
          <Download size={14} />
        )}
        <span>
          {busy
            ? "正在保存…"
            : saved === "saved"
              ? "已保存"
              : saved === "download-started"
                ? "已开始下载"
                : "保存"}
        </span>
      </button>
      {error && (
        <small className="file-error" role="alert">
          {error}
        </small>
      )}
    </div>
  );
}

export function AttachmentPreview({
  client,
  file,
  onClose,
  initialLine,
}: {
  client: MewDexClient;
  file: Attachment;
  onClose: () => void;
  initialLine?: number;
}) {
  const returnFocus = useDialogReturnFocus();
  const mime = mimeOf(file);
  const image = imageTypes.has(mime);
  const text =
    mime.startsWith("text/") ||
    mime === "application/json" ||
    mime === "application/xml";
  const content = useQuery({
    queryKey: ["attachment-content", file.id],
    queryFn: ({ signal }) => client.attachmentContent(file.id, signal),
    enabled: image || text,
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  });
  const [imageURL, setImageURL] = useState("");
  const [imageError, setImageError] = useState(false);
  const [previewBytes, setPreviewBytes] = useState<Uint8Array>();
  const [encoding, setEncoding] = useState<TextPreviewEncoding>("auto");
  const preview = useMemo(
    () =>
      previewBytes
        ? decodeTextPreview(
            previewBytes,
            encoding,
            file.size > textPreviewLimit,
          )
        : undefined,
    [previewBytes, encoding, file.size],
  );
  const previewText = preview?.text;
  const [findOpen, setFindOpen] = useState(false);
  useEffect(() => {
    if (!image || !content.data) return;
    const url = URL.createObjectURL(
      new Blob([content.data], { type: file.mediaType }),
    );
    setImageURL(url);
    setImageError(false);
    return () => URL.revokeObjectURL(url);
  }, [image, content.data, file.mediaType]);
  useEffect(() => {
    if (!text || !content.data) return;
    let active = true;
    void content.data
      .slice(0, textPreviewLimit)
      .arrayBuffer()
      .then((value) => {
        if (active) setPreviewBytes(new Uint8Array(value));
      });
    return () => {
      active = false;
    };
  }, [text, content.data]);
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog attachment-preview-dialog"
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
          onKeyDown={(event) => {
            if (
              text &&
              previewText !== undefined &&
              !event.nativeEvent.isComposing &&
              !event.altKey &&
              !event.shiftKey &&
              (event.ctrlKey || event.metaKey) &&
              event.key.toLowerCase() === "f"
            ) {
              event.preventDefault();
              event.stopPropagation();
              setFindOpen(true);
            }
          }}
          onEscapeKeyDown={(event) => {
            if (findOpen) {
              event.preventDefault();
              setFindOpen(false);
            }
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>{file.name}</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="关闭附件预览">
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>{formatBytes(file.size)}</Dialog.Description>
          <div
            className={`attachment-preview-body ${text ? "is-text-preview" : ""}`}
          >
            {(image || text) && content.isPending && (
              <div className="file-preview-empty">
                <LoaderCircle className="spinning" size={24} />
                <span>正在读取附件…</span>
              </div>
            )}
            {content.error && (
              <div className="file-preview-empty">
                <span>暂时无法读取附件。</span>
                <button
                  className="settings-link"
                  onClick={() => void content.refetch()}
                >
                  重试读取
                </button>
              </div>
            )}
            {image && imageURL && !imageError && (
              <div className="image-preview">
                <img
                  src={imageURL}
                  alt={file.name}
                  onError={() => setImageError(true)}
                />
              </div>
            )}
            {imageError && (
              <p className="muted">无法预览这张图片，可以保存到本机查看。</p>
            )}
            {text && content.data && (
              <>
                {previewText === undefined ? (
                  <p className="muted">正在读取文本…</p>
                ) : (
                  <TextFilePreview
                    text={previewText}
                    initialLine={initialLine}
                    findOpen={findOpen}
                    onFindOpen={setFindOpen}
                    encoding={encoding}
                    resolvedEncoding={preview!.encoding}
                    onEncodingChange={setEncoding}
                  />
                )}
                {file.size > textPreviewLimit && (
                  <p className="muted">预览前 256 KiB，保存可获得完整文件。</p>
                )}
              </>
            )}
            {!image && !text && (
              <div className="file-preview-empty">
                <File size={32} />
                <span>此类型可保存后使用本机应用打开。</span>
              </div>
            )}
          </div>
          <footer className="attachment-preview-actions">
            {text && previewText !== undefined && (
              <CopyButton text={previewText} label="复制预览内容" />
            )}
            <SaveButton client={client} file={file} content={content.data} />
          </footer>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function AttachmentCard({
  client,
  item,
  onRemove,
}: {
  client: MewDexClient;
  item: AttachmentItem;
  onRemove?: (id: string) => void;
}) {
  const file = useQuery({
    queryKey: ["attachment", item.id],
    queryFn: ({ signal }) => client.attachment(item.id, signal),
    initialData: item.metadata,
    staleTime: Infinity,
    retry: false,
  });
  const [preview, setPreview] = useState(false);
  const image = file.data && imageTypes.has(mimeOf(file.data));
  return (
    <article className={`file-card ${onRemove ? "draft" : ""}`}>
      <button
        className="file-card-main"
        disabled={!file.data}
        onClick={() => setPreview(true)}
        aria-label={file.data ? `查看附件 ${file.data.name}` : "正在读取附件"}
      >
        <span className="file-card-icon">
          {image ? <ImageIcon size={19} /> : <FileText size={19} />}
        </span>
        <span>
          <strong title={file.data?.name}>
            {file.data?.name ?? (file.error ? "附件暂不可用" : "正在读取附件…")}
          </strong>
          <small>{file.data ? formatBytes(file.data.size) : ""}</small>
        </span>
      </button>
      {file.error && (
        <button className="file-retry" onClick={() => void file.refetch()}>
          重试
        </button>
      )}
      {onRemove ? (
        <button
          className="icon-button"
          aria-label={`移除附件 ${file.data?.name ?? ""}`}
          onClick={() => onRemove(item.id)}
        >
          <X size={14} />
        </button>
      ) : (
        file.data && <SaveButton client={client} file={file.data} />
      )}
      {preview && file.data && (
        <AttachmentPreview
          client={client}
          file={file.data}
          initialLine={item.initialLine}
          onClose={() => setPreview(false)}
        />
      )}
    </article>
  );
}

export function AttachmentList({
  client,
  items,
  onRemove,
}: {
  client: MewDexClient;
  items: AttachmentItem[];
  onRemove?: (id: string) => void;
}) {
  return (
    <div
      className={`message-attachments ${onRemove ? "composer-attachments" : ""}`}
    >
      {items.map((item, index) => (
        <AttachmentCard
          client={client}
          key={`${item.id}:${index}`}
          item={item}
          onRemove={onRemove}
        />
      ))}
    </div>
  );
}
