import { getLanguage, t } from "../localization.ts";
import { useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Archive, ArchiveRestore, ArrowUpRight, Search, X } from "lucide-react";
import type { Device, Session, SessionPatch } from "@mewdex/sdk";
import { conversationContext, navigationMatches } from "../workspaceNavigation";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export type UpdateSession = (
  session: Session,
  patch: SessionPatch,
) => Promise<void>;

export function RenameSessionDialog({
  session,
  onClose,
  onUpdate,
  busy,
  returnFocus,
}: {
  session: Session;
  onClose: () => void;
  onUpdate: UpdateSession;
  busy: boolean;
  returnFocus?: HTMLElement | null;
}) {
  const [title, setTitle] = useState(session.title);
  const focusReturn = useDialogReturnFocus();
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const length = Array.from(title.trim()).length;
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
          className="settings-dialog rename-session-dialog"
          onCloseAutoFocus={(event) => {
            if (returnFocus?.isConnected) {
              event.preventDefault();
              returnFocus.focus({ preventScroll: true });
            } else focusReturn.restore(event);
          }}
          onOpenAutoFocus={(event) => {
            focusReturn.capture(event);
            event.preventDefault();
            input.current?.focus();
            input.current?.select();
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("重命名对话")}</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label={t("关闭重命名")}>
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t("新名称会同步到有权访问此对话的输入端。")}
          </Dialog.Description>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              setError("");
              void onUpdate(session, { title: title.trim() })
                .then(onClose)
                .catch((error: Error) => setError(error.message));
            }}
          >
            <label htmlFor="session-title">{t("对话名称")}</label>
            <input
              id="session-title"
              ref={input}
              value={title}
              disabled={busy}
              maxLength={240}
              onChange={(event) => setTitle(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && event.nativeEvent.isComposing)
                  event.preventDefault();
              }}
            />
            <div
              className={`session-title-count ${length > 120 ? "error" : ""}`}
            >
              {length} / 120
            </div>
            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}
            <div className="session-dialog-actions">
              <Dialog.Close className="secondary">{t("取消")}</Dialog.Close>
              <button
                className="primary"
                disabled={
                  busy ||
                  length < 1 ||
                  length > 120 ||
                  title.trim() === session.title
                }
              >
                {busy ? t("正在保存…") : t("保存名称")}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function ArchivedSessionsDialog({
  open,
  onOpenChange,
  sessions,
  devices,
  onUpdate,
  onSession,
  canManage,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessions: Session[];
  devices: Device[];
  onUpdate: UpdateSession;
  onSession: (session: Session) => void;
  canManage: boolean;
}) {
  const [search, setSearch] = useState("");
  const focusReturn = useDialogReturnFocus();
  const [error, setError] = useState("");
  const devicesById = useMemo(
    () => new Map(devices.map((device) => [device.id, device])),
    [devices],
  );
  const archived = useMemo(
    () =>
      sessions
        .filter(
          (session) =>
            session.archived &&
            navigationMatches(
              search,
              session.title,
              ...Object.values(
                conversationContext(session, devicesById.get(session.deviceId)),
              ),
            ),
        )
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)),
    [sessions, devicesById, search, getLanguage()],
  );
  function changeOpen(value: boolean) {
    onOpenChange(value);
    if (!value) {
      setSearch("");
      setError("");
    }
  }
  return (
    <Dialog.Root open={open} onOpenChange={changeOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog archived-sessions-dialog"
          onOpenAutoFocus={focusReturn.capture}
          onCloseAutoFocus={focusReturn.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("已归档对话")}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭已归档对话")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t("查看网络内的归档内容，或恢复到对话列表。")}
          </Dialog.Description>
          <label className="search-field">
            <Search size={16} />
            <input
              aria-label={t("搜索已归档对话")}
              placeholder={t("搜索名称、设备或项目…")}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="archived-session-list">
            {archived.map((session) => (
              <div className="archived-session-row" key={session.id}>
                <button
                  className="archived-session-open"
                  onClick={() => {
                    onSession(session);
                    changeOpen(false);
                  }}
                >
                  <Archive size={18} />
                  <span>
                    <strong>{session.title}</strong>
                    <small>
                      {Object.values(
                        conversationContext(
                          session,
                          devicesById.get(session.deviceId),
                        ),
                      ).join(" · ")}{" "}
                      ·{" "}
                      {new Date(session.updatedAt).toLocaleDateString(
                        getLanguage(),
                      )}
                      {session.state === "running" ? t(" · 正在执行") : ""}
                    </small>
                  </span>
                  <ArrowUpRight size={15} />
                </button>
                <button
                  className="restore-session-button"
                  disabled={!canManage}
                  onClick={() => {
                    setError("");
                    void onUpdate(session, { archived: false }).catch(
                      (error: Error) => setError(error.message),
                    );
                  }}
                >
                  <ArchiveRestore size={16} />
                  {t("恢复")}
                </button>
              </div>
            ))}
            {!archived.length && (
              <div className="archived-sessions-empty">
                <Archive size={30} />
                <strong>
                  {search ? t("没有匹配的归档对话") : t("还没有归档对话")}
                </strong>
                <p>
                  {search
                    ? t("试试其他名称、设备或项目。")
                    : t("在对话选项中归档，内容会保留在这里。")}
                </p>
              </div>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
