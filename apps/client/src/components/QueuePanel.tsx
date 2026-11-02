import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { MewDexClient, QueueItem, Session } from "@mewdex/sdk";
import { isUnconfirmedCommand } from "../commandRecovery";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

const labels: Record<string, string> = {
  queued: "等待执行",
  dispatching: "正在交给执行端",
  running: "执行中",
  completed: "已完成",
  interrupted: "已中断",
  failed: "失败",
  unknown: "结果待核实",
  cancelled: "已取消",
};

export function QueuePanel({
  client,
  session,
  online,
  onCancel,
  history = false,
}: {
  client: MewDexClient;
  session: Session;
  online: boolean;
  onCancel: (item: QueueItem) => Promise<void>;
  history?: boolean;
}) {
  const query = useQuery({
    queryKey: ["queue", session.id],
    queryFn: () => client.queue(session.id),
  });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const waiting =
    query.data?.filter((item) => item.state === "queued").length ?? 0;
  const retired = session.executionRecovery?.state === "retired";
  if (retired && !history) return null;
  const items = history
    ? query.data
    : query.data?.filter((item) =>
        ["queued", "dispatching", "running", "unknown", "failed"].includes(
          item.state,
        ),
      );
  if (!query.error && !items?.length)
    return history ? <p className="empty-list">还没有排队记录</p> : null;
  return (
    <details className="task-queue" open={history || undefined}>
      <summary>
        {history ? "输入与执行记录" : retired ? "保留的输入" : "待处理队列"}
        {waiting
          ? ` · ${waiting} 条${retired ? "未发送" : "等待"}`
          : retired
            ? ""
            : ` · ${items?.length ?? 0} 条`}
      </summary>
      <p>
        {retired
          ? "此执行已结束；这里只保留历史输入、策略和未确认结果。"
          : "每条保留入队时的输入和策略。停止或执行端重启后，手动恢复队列。"}
      </p>
      {query.error && <div role="alert">暂时无法读取队列。</div>}
      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}
      <ol>
        {items?.map((item) => (
          <li key={item.id}>
            <div className="queue-item-heading">
              <span>
                {retired && item.state === "queued"
                  ? "未发送，输入已保留"
                  : (labels[item.state] ?? item.state)}
              </span>
              <small>
                {item.policy.collaborationMode === "plan" ? "方案讨论 · " : ""}
                {item.policy.mode === "edit"
                  ? "直接修改"
                  : item.policy.mode === "full"
                    ? "完整验证"
                    : "局部验证"}
                {item.policy.noTests ? " · 不要测试" : ""}
              </small>
              {!retired && ["queued", "failed"].includes(item.state) && (
                <button
                  disabled={!!busy || !online}
                  onClick={() => {
                    setBusy(item.id);
                    setError("");
                    void onCancel(item)
                      .catch((e: Error) => {
                        if (!isUnconfirmedCommand(e)) setError(e.message);
                      })
                      .finally(() => setBusy(""));
                  }}
                >
                  {busy === item.id ? "处理中…" : "取消此条"}
                </button>
              )}
            </div>
            <div className="queue-item-input">
              {item.input
                .filter((input) => input.type === "text")
                .map((input) => input.text)
                .join("\n") || "附件任务"}
              {item.input.some((input) => !!input.attachmentId) && (
                <small> · 含附件</small>
              )}
            </div>
            {item.error && <div className="queue-item-error">{item.error}</div>}
          </li>
        ))}
      </ol>
      {history && <small>显示全部待处理项与最近 50 条记录。</small>}
    </details>
  );
}

export function QueueHistoryDialog({
  open,
  onOpenChange,
  ...props
}: Omit<Parameters<typeof QueuePanel>[0], "history"> & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const returnFocus = useDialogReturnFocus();
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog queue-history-dialog"
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>排队与执行记录</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="关闭执行记录">
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            查看输入、执行方式与处理结果。
          </Dialog.Description>
          <QueuePanel {...props} history />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
