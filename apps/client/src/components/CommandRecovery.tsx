import * as Dialog from "@radix-ui/react-dialog";
import { AlertCircle, RefreshCw, X } from "lucide-react";
import type { CommandResult, MewDexClient, Session } from "@mewdex/sdk";
import type { CommandRecoveryView } from "../commandRecovery";
import { CommandReceipt } from "./CommandReceipt";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function CommandRecoveryNotice({
  view,
  hasMore,
  error,
  onOpen,
}: {
  view: CommandRecoveryView;
  hasMore: boolean;
  error: string;
  onOpen: () => void;
}) {
  if (!view.currentCount && !view.otherCount && !hasMore && !error) return null;
  const relevant = view.currentCount > 0 || hasMore || !!error;
  const label = error
    ? "待确认操作暂时无法读取"
    : view.currentCount
      ? `${view.currentDeviceName} · ${view.currentCount} 项操作待确认`
      : hasMore
        ? `${view.currentDeviceName} · 还有待确认操作`
        : `其他设备有 ${view.otherCount} 项操作待确认`;
  return (
    <div
      className="command-recovery-notice"
      data-attention={relevant || undefined}
      role="status"
    >
      <AlertCircle size={16} aria-hidden="true" />
      <span>{label}</span>
      <button onClick={onOpen}>查看操作</button>
    </div>
  );
}

export function CommandRecoveryDialog({
  open,
  onOpenChange,
  client,
  view,
  after,
  hasMore,
  busy,
  ready,
  connected,
  error,
  canAdvance,
  onRefresh,
  onFirst,
  onNext,
  onResolved,
  onDismiss,
  onSession,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  client: MewDexClient;
  view: CommandRecoveryView;
  after: number;
  hasMore: boolean;
  busy: boolean;
  ready: boolean;
  connected: boolean;
  error: string;
  canAdvance: boolean;
  onRefresh: () => void;
  onFirst: () => void;
  onNext: () => void;
  onResolved: (id: string, result: CommandResult) => void;
  onDismiss: (id: string) => void;
  onSession: (session: Session) => void;
}) {
  const empty = view.groups.length === 0;
  const returnFocus = useDialogReturnFocus();
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog command-recovery-dialog"
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>待确认操作</Dialog.Title>
            <div className="command-recovery-heading-actions">
              <button
                className="icon-button"
                disabled={!ready || busy}
                onClick={onRefresh}
                aria-label="刷新待确认操作"
              >
                <RefreshCw size={17} />
              </button>
              <Dialog.Close className="icon-button" aria-label="关闭待确认操作">
                <X size={18} />
              </Dialog.Close>
            </div>
          </div>
          <Dialog.Description>
            查询原操作的回执。关闭窗口会保留待确认记录，不取消任务或重发输入。
          </Dialog.Description>
          <div className="command-recovery-context">
            <span>
              {view.currentDeviceName} · {after ? "后续记录" : "第一页"}
            </span>
            <span>
              {view.currentCount} 项已载入
              {view.otherCount ? ` · 其他设备 ${view.otherCount} 项暂存` : ""}
            </span>
          </div>
          <div className="command-recovery-body" aria-busy={busy}>
            {!connected && (
              <p className="command-recovery-note" role="status">
                网络未连接，显示最近获取的记录。
              </p>
            )}
            {error && (
              <p className="error" role="alert">
                无法读取待确认操作：{error}
              </p>
            )}
            {view.groups.map((group) => (
              <section
                className="command-recovery-group"
                key={group.deviceId}
                aria-label={group.deviceName}
              >
                <h3>
                  {group.deviceName}
                  <small>
                    {group.current ? "当前设备" : "本次暂存"} ·{" "}
                    {group.items.length} 项
                  </small>
                </h3>
                {group.items.map(({ pending, targetLabel, session }) => (
                  <CommandReceipt
                    key={pending.command.id}
                    client={client}
                    pending={pending}
                    targetLabel={targetLabel}
                    canQuery={ready}
                    onOpen={
                      session
                        ? () => {
                            onOpenChange(false);
                            onSession(session);
                          }
                        : undefined
                    }
                    onResolved={(result) =>
                      onResolved(pending.command.id, result)
                    }
                    onDismiss={() => onDismiss(pending.command.id)}
                  />
                ))}
              </section>
            ))}
            {empty && !error && (
              <p className="empty-list">
                {!ready
                  ? "连接后可读取操作记录"
                  : busy
                    ? "正在读取操作记录…"
                    : hasMore
                      ? "本页操作已处理，可以继续读取下一页。"
                      : "没有待确认操作"}
              </p>
            )}
          </div>
          <footer className="command-recovery-footer">
            <p>
              {hasMore
                ? view.currentCount
                  ? "先查询或关闭本页当前设备的提示，再读取后续记录。"
                  : "还有后续记录，可以继续读取下一页。"
                : "这里只显示当前输入端的操作。查询列表不会重复执行。"}
            </p>
            <div>
              {after > 0 && (
                <button disabled={!ready || busy} onClick={onFirst}>
                  返回第一页
                </button>
              )}
              {hasMore && (
                <button disabled={!ready || !canAdvance} onClick={onNext}>
                  读取下一页
                </button>
              )}
            </div>
          </footer>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
