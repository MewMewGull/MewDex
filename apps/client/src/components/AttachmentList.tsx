import { t } from "../localization.ts";
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
import {
  canPreviewImage,
  canSaveAttachment,
  saveAttachmentFile,
} from "../attachmentFiles";
import { formatBytes } from "../formatBytes";
import { CopyButton } from "./CopyButton";
import { TextFilePreview } from "./TextFilePreview";
import { decodeTextPreview, type TextPreviewEncoding } from "../textPreview";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { useBlobURL } from "../useBlobURL";
import { AttachmentThumbnail } from "./AttachmentThumbnail";

interface AttachmentItem {
  id: string;
  metadata?: Attachment;
  initialLine?: number;
}
const textPreviewLimit = 256 * 1024;

function SaveButton({
  client,
  file,
  content,
}: {
  client: MewDexClient;
  file: Pick<Attachment, "id" | "name">;
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
        aria-label={t("保存附件 {0}", file.name)}
        title={support.data ? t("保存到本机") : t("当前输入端暂未提供文件保存")}
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
            ? t("正在保存…")
            : saved === "saved"
              ? t("已保存")
              : saved === "download-started"
                ? t("已开始下载")
                : t("保存")}
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
  initialContent,
}: {
  client: MewDexClient;
  file: Pick<Attachment, "id" | "name" | "mediaType" | "size">;
  onClose: () => void;
  initialLine?: number;
  initialContent?: Blob;
}) {
  const returnFocus = useDialogReturnFocus();
  const mime = file.mediaType.split(";")[0].trim().toLowerCase();
  const image = canPreviewImage(mime);
  const text =
    mime.startsWith("text/") ||
    mime === "application/json" ||
    mime === "application/xml";
  const content = useQuery({
    queryKey: ["attachment-content", file.id],
    queryFn: ({ signal }) => client.attachmentContent(file.id, signal),
    enabled: !initialContent && (image || text),
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  });
  const blob = initialContent ?? content.data;
  const imageURL = useBlobURL(image ? blob : undefined, file.mediaType);
  const [failedImageURL, setFailedImageURL] = useState("");
  const imageError = !!imageURL && failedImageURL === imageURL;
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
    if (!text || !blob) return;
    let active = true;
    void blob
      .slice(0, textPreviewLimit)
      .arrayBuffer()
      .then((value) => {
        if (active) setPreviewBytes(new Uint8Array(value));
      });
    return () => {
      active = false;
    };
  }, [text, blob]);
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
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭附件预览")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>{formatBytes(file.size)}</Dialog.Description>
          <div
            className={`attachment-preview-body ${text ? "is-text-preview" : ""}`}
          >
            {(image || text) && !blob && content.isPending && (
              <div className="file-preview-empty">
                <LoaderCircle className="spinning" size={24} />
                <span>{t("正在读取附件…")}</span>
              </div>
            )}
            {content.error && (
              <div className="file-preview-empty">
                <span>{t("暂时无法读取附件。")}</span>
                <button
                  className="settings-link"
                  onClick={() => void content.refetch()}
                >
                  {t("重试读取")}
                </button>
              </div>
            )}
            {image && imageURL && !imageError && (
              <div className="image-preview">
                <img
                  src={imageURL}
                  alt={file.name}
                  onError={() => setFailedImageURL(imageURL)}
                />
              </div>
            )}
            {imageError && (
              <p className="muted">
                {t("无法预览这张图片，可以保存到本机查看。")}
              </p>
            )}
            {text && blob && (
              <>
                {previewText === undefined ? (
                  <p className="muted">{t("正在读取文本…")}</p>
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
                  <p className="muted">
                    {t("预览前 256 KiB，保存可获得完整文件。")}
                  </p>
                )}
              </>
            )}
            {!image && !text && (
              <div className="file-preview-empty">
                <File size={32} />
                <span>{t("此类型可保存后使用本机应用打开。")}</span>
              </div>
            )}
          </div>
          <footer className="attachment-preview-actions">
            {text && previewText !== undefined && (
              <CopyButton text={previewText} label={t("复制预览内容")} />
            )}
            <SaveButton client={client} file={file} content={blob} />
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
  const image = file.data && canPreviewImage(file.data.mediaType);
  const thumbnail = !!onRemove && !!image;
  const content = useQuery({
    queryKey: ["attachment-content", item.id],
    queryFn: ({ signal }) => client.attachmentContent(item.id, signal),
    enabled: thumbnail,
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  });
  return (
    <article
      className={`file-card ${onRemove ? "draft" : ""} ${thumbnail ? "is-image" : ""}`}
    >
      <button
        className="file-card-main"
        disabled={!file.data}
        onClick={() => setPreview(true)}
        title={file.data?.name}
        aria-label={
          file.data ? t("查看附件 {0}", file.data.name) : t("正在读取附件")
        }
      >
        {thumbnail && file.data ? (
          <AttachmentThumbnail
            content={content.data}
            mediaType={file.data.mediaType}
            name={file.data.name}
            failed={content.isError}
          />
        ) : (
          <>
            <span className="file-card-icon">
              {image ? <ImageIcon size={19} /> : <FileText size={19} />}
            </span>
            <span>
              <strong title={file.data?.name}>
                {file.data?.name ??
                  (file.error ? t("附件暂不可用") : t("正在读取附件…"))}
              </strong>
              <small>{file.data ? formatBytes(file.data.size) : ""}</small>
            </span>
          </>
        )}
      </button>
      {file.error && (
        <button className="file-retry" onClick={() => void file.refetch()}>
          {t("重试")}
        </button>
      )}
      {onRemove ? (
        <button
          className={`icon-button ${thumbnail ? "attachment-image-remove" : ""}`}
          aria-label={t("移除附件 {0}", file.data?.name ?? "")}
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
