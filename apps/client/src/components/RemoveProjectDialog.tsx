import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, X } from "lucide-react";
import type { Project } from "@mewdex/sdk";
import { t } from "../localization.ts";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function RemoveProjectDialog({
  project,
  returnFocus,
  onRemove,
  onClose,
}: {
  project: Project;
  returnFocus: HTMLElement | null;
  onRemove: () => Promise<void>;
  onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submitting = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);
  const focus = useDialogReturnFocus();

  async function remove() {
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError("");
    try {
      await onRemove();
      onClose();
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : t("移除项目失败，请重试。"),
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
          className="settings-dialog remove-project-dialog"
          aria-busy={busy}
          onOpenAutoFocus={(event) => {
            focus.capture(event);
            event.preventDefault();
            cancel.current?.focus();
          }}
          onCloseAutoFocus={(event) => {
            const target = returnFocus?.isConnected
              ? returnFocus
              : (document.querySelector<HTMLElement>(
                  ".sidebar .session[aria-current='page']",
                ) ?? document.querySelector<HTMLElement>(".sidebar .new-chat"));
            if (target) {
              event.preventDefault();
              target.focus({ preventScroll: true });
            } else focus.restore(event);
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("移除 {0}？", project.name)}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭")}
              disabled={busy}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t(
              "这只会从应用的项目列表中移除该项目。电脑上的文件和现有对话不会被删除，该项目下的对话会保留在“最近对话”中。",
            )}
          </Dialog.Description>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="session-dialog-actions">
            <Dialog.Close ref={cancel} className="secondary" disabled={busy}>
              {t("取消")}
            </Dialog.Close>
            <button
              className="project-remove-confirm"
              disabled={busy}
              onClick={() => void remove()}
            >
              {busy && <LoaderCircle size={16} className="spin" />}
              {busy ? t("正在移除…") : t("移除项目")}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
