import { t } from "../localization.ts";
import { useState } from "react";
import { ArrowUpRight, RefreshCw } from "lucide-react";
import { MewDexClient, type CommandResult } from "@mewdex/sdk";
import type { PendingCommand } from "../commandRecovery";

export function CommandReceipt({
  client,
  pending,
  onResolved,
  onDismiss,
  targetLabel,
  onOpen,
  canQuery,
}: {
  client: MewDexClient;
  pending: PendingCommand;
  onResolved: (result: CommandResult) => void;
  onDismiss: () => void;
  targetLabel: string;
  onOpen?: () => void;
  canQuery: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState(t("指令结果尚未确认"));
  async function observe() {
    setBusy(true);
    setError("");
    try {
      const result = await client.result(pending.command.id);
      if (result.state === "completed" || result.state === "failed")
        onResolved(result);
      else
        setStatus(
          result.state === "accepted"
            ? t("执行端仍在处理指令")
            : (result.error ?? t("执行端仍无法确认结果")),
        );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  const message = ["start", "steer", "enqueue"].includes(pending.command.kind);
  const labels: Record<string, string> = {
    create: t("创建对话"),
    start: t("发送消息"),
    steer: t("追加消息"),
    enqueue: t("加入队列"),
    stop: t("停止任务"),
    interrupt: t("中断回合"),
    resume: t("恢复任务"),
    "queue.cancel": t("取消队列项"),
    reconcile: t("核实执行状态"),
    "execution.retire": t("结束旧执行"),
    respond: t("回复请求"),
    "file.export": t("回传文件快照"),
  };
  return (
    <section className="command-receipt" aria-label={t("待确认指令")}>
      <header>
        <div>
          <small>{labels[pending.command.kind] ?? t("控制指令")}</small>
          <strong>{targetLabel}</strong>
        </div>
        {onOpen && (
          <button
            className="receipt-open-target"
            onClick={onOpen}
            aria-label={t("打开对话：{0}", targetLabel)}
          >
            <ArrowUpRight size={15} />
            {t("打开对话")}
          </button>
        )}
      </header>
      <p role="status">{status}</p>
      <details className="receipt-technical-details">
        <summary>{t("操作编号")}</summary>
        <code>{pending.command.id}</code>
      </details>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="receipt-card-actions">
        <button disabled={!canQuery || busy} onClick={() => void observe()}>
          <RefreshCw size={13} />
          {busy ? t("正在查询…") : t("查询回执")}
        </button>
        <button onClick={onDismiss}>
          {message && pending.draftKey
            ? t("清除本次已发送内容并关闭提示")
            : t("关闭状态提示")}
        </button>
      </div>
    </section>
  );
}
