import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, X } from "lucide-react";
import { t } from "../localization.ts";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function WorkspaceSectionDialog({
  mode,
  name,
  count,
  onSave,
  onClose,
}: {
  mode: "create" | "rename" | "remove";
  name: string;
  count: number;
  onSave: (name: string) => Promise<void>;
  onClose: () => void;
}) {
  const [value, setValue] = useState(mode === "create" ? "" : name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submitting = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const focus = useDialogReturnFocus();
  const removing = mode === "remove";
  async function save() {
    if (submitting.current || (!removing && !value.trim())) return;
    submitting.current = true;
    setBusy(true);
    setError("");
    try {
      await onSave(value.trim());
      onClose();
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : t("分区更新失败，请重试。"),
      );
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !submitting.current) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog rename-session-dialog"
          aria-busy={busy}
          onOpenAutoFocus={(event) => {
            focus.capture(event);
            event.preventDefault();
            if (removing) cancel.current?.focus();
            else {
              input.current?.focus();
              input.current?.select();
            }
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            document
              .querySelector<HTMLElement>(
                ".sidebar .conversation-nav-heading .icon-button",
              )
              ?.focus();
          }}
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void save();
            }}
          >
            <div className="dialog-heading">
              <Dialog.Title>
                {removing
                  ? t("移除分区 {0}？", name)
                  : mode === "create"
                    ? t("新建分区")
                    : t("重命名分区")}
              </Dialog.Title>
              <Dialog.Close
                type="button"
                className="icon-button"
                disabled={busy}
                aria-label={t("关闭")}
              >
                <X size={18} />
              </Dialog.Close>
            </div>
            <Dialog.Description>
              {removing
                ? t(
                    "分区中的 {0} 个项目将移到默认分区，对话和文件会保留。",
                    count,
                  )
                : t("用分区整理项目，可拖拽调整分区和项目顺序。")}
            </Dialog.Description>
            {!removing && (
              <label className="section-name-field">
                {t("分区名称")}
                <input
                  ref={input}
                  value={value}
                  maxLength={80}
                  disabled={busy}
                  onChange={(event) => setValue(event.target.value)}
                />
              </label>
            )}
            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}
            <div className="session-dialog-actions">
              <Dialog.Close
                ref={cancel}
                type="button"
                className="secondary"
                disabled={busy}
              >
                {t("取消")}
              </Dialog.Close>
              <button
                className={removing ? "project-remove-confirm" : ""}
                disabled={busy || (!removing && !value.trim())}
                type="submit"
              >
                {busy && <LoaderCircle size={15} className="spinning" />}
                {removing ? t("移除分区") : t("保存")}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
