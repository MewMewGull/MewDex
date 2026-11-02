import { useEffect, useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  Activity,
  AlertCircle,
  Archive,
  ArrowUpRight,
  LoaderCircle,
  MessageCircleQuestion,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import type { Device, Session, SessionActivity } from "@mewdex/sdk";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { currentActivity } from "../activityPresentation";

function activityLabel(item: SessionActivity): string {
  const session = item.session;
  if (item.pendingInteractions)
    return item.blockingInteractions ? "等待回复" : "有待处理请求";
  if (
    session.state === "unknown" ||
    session.stopStatus === "unconfirmed" ||
    item.unconfirmedInputs
  )
    return "执行结果待核实";
  if (session.stopStatus === "processes-unchecked") return "后台进程待核实";
  if (item.failedInputs) return "排队消息未完成";
  if (session.frozen && item.queuedInputs) return "队列已暂停";
  if (session.state === "stopping") return "正在停止";
  if (session.stopStatus === "background-running") return "后台任务运行中";
  if (session.state === "running") return "执行中";
  return "等待执行";
}

export function ActivityDialog({
  open,
  onOpenChange,
  items,
  devices,
  loading,
  refreshing,
  ready,
  error,
  onRefresh,
  onSession,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items?: SessionActivity[];
  devices: Device[];
  loading: boolean;
  refreshing: boolean;
  ready: boolean;
  error: string;
  onRefresh: () => void;
  onSession: (session: Session, focusRequests: boolean) => void;
}) {
  const [search, setSearch] = useState("");
  const returnFocus = useDialogReturnFocus();
  const [filter, setFilter] = useState<
    "all" | "attention" | "running" | "unconfirmed"
  >("all");
  const input = useRef<HTMLInputElement>(null);
  const deviceMap = useMemo(
    () => new Map(devices.map((device) => [device.id, device])),
    [devices],
  );
  const active = items?.filter((item) => currentActivity(item, devices)) ?? [];
  const attention = active.filter((item) => item.needsAttention).length;
  const visible = useMemo(
    () =>
      (items ?? []).filter((item) => {
        if (
          filter === "unconfirmed"
            ? currentActivity(item, devices)
            : !currentActivity(item, devices)
        )
          return false;
        if (
          (filter === "attention" && !item.needsAttention) ||
          (filter === "running" && item.needsAttention)
        )
          return false;
        const device = deviceMap.get(item.session.deviceId);
        const project = device?.projects.find(
          (project) => project.id === item.session.projectId,
        );
        return `${item.session.title} ${device?.name ?? ""} ${project?.name ?? ""}`
          .toLocaleLowerCase()
          .includes(search.trim().toLocaleLowerCase());
      }),
    [items, filter, search, deviceMap],
  );
  useEffect(() => {
    if (!open) {
      setSearch("");
      setFilter("all");
    }
  }, [open]);
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog activity-dialog"
          onOpenAutoFocus={(event) => {
            returnFocus.capture(event);
            event.preventDefault();
            input.current?.focus();
          }}
          onCloseAutoFocus={returnFocus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>任务与请求</Dialog.Title>
            <div className="activity-heading-actions">
              <button
                className={`icon-button ${refreshing ? "refreshing" : ""}`}
                aria-label="刷新任务与请求"
                disabled={!ready || refreshing}
                onClick={onRefresh}
              >
                <RefreshCw size={17} />
              </button>
              <Dialog.Close className="icon-button" aria-label="关闭任务与请求">
                <X size={18} />
              </Dialog.Close>
            </div>
          </div>
          <Dialog.Description>
            这里汇总正在工作的对话，以及等待你批准或回复的请求。点击任务回到对应对话。
          </Dialog.Description>
          <label className="search-field">
            <Search size={16} />
            <input
              ref={input}
              aria-label="搜索任务与请求"
              placeholder="搜索对话、设备或项目…"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <div className="activity-filters" aria-label="任务范围">
            {(
              [
                ["all", "当前任务", active.length],
                ["attention", "待处理", items ? attention : undefined],
                ["running", "进行中", active.length - attention],
                [
                  "unconfirmed",
                  "历史待确认",
                  (items?.length ?? 0) - active.length,
                ],
              ] as const
            ).map(([id, label, count]) => (
              <button
                key={id}
                aria-pressed={filter === id}
                onClick={() => setFilter(id)}
              >
                {label}
                {count !== undefined && <span>{count}</span>}
              </button>
            ))}
          </div>
          {filter === "unconfirmed" && (
            <p className="field-note">
              设备离线或原执行结果尚未确认的记录保留在这里，不计入当前任务。
            </p>
          )}
          {!ready && (
            <p className="activity-connection-note" role="status">
              网络未连接，显示最近获取的状态。
            </p>
          )}
          {error && (
            <p className="error" role="alert">
              暂时无法读取任务与请求：{error}
            </p>
          )}
          <div className="activity-list" aria-busy={loading}>
            {visible.map((item) => {
              const session = item.session;
              const device = deviceMap.get(session.deviceId);
              const project = device?.projects.find(
                (project) => project.id === session.projectId,
              );
              const label = activityLabel(item);
              const Icon = item.pendingInteractions
                ? MessageCircleQuestion
                : item.needsAttention
                  ? AlertCircle
                  : session.state === "running"
                    ? LoaderCircle
                    : Activity;
              return (
                <button
                  className={`activity-row ${item.needsAttention ? "needs-attention" : ""}`}
                  key={session.id}
                  onClick={() => {
                    onSession(session, item.pendingInteractions > 0);
                    onOpenChange(false);
                  }}
                >
                  <span
                    className={`activity-icon ${!item.needsAttention && session.state === "running" && device?.online ? "running" : ""}`}
                  >
                    <Icon size={19} />
                  </span>
                  <span className="activity-copy">
                    <span className="activity-row-title">
                      <strong>{session.title}</strong>
                      <span
                        className={`activity-status ${device?.online ? "" : "is-offline"}`}
                      >
                        {device?.online ? label : "设备离线"}
                      </span>
                    </span>
                    <small>
                      {device?.name ?? "离线设备"} · {project?.name ?? "原项目"}
                      {!device?.online ? ` · 上次状态：${label}` : ""}
                    </small>
                    <span className="activity-details">
                      {item.pendingInteractions > 0 && (
                        <span>{item.pendingInteractions} 项待回复</span>
                      )}
                      {item.queuedInputs > 0 && (
                        <span>{item.queuedInputs} 条排队输入</span>
                      )}
                      {item.unconfirmedInputs > 0 && (
                        <span>{item.unconfirmedInputs} 条待核实</span>
                      )}
                      {item.failedInputs > 0 && (
                        <span>{item.failedInputs} 条未完成</span>
                      )}
                      {session.archived && (
                        <span>
                          <Archive size={12} />
                          已归档
                        </span>
                      )}
                    </span>
                  </span>
                  <ArrowUpRight size={16} className="activity-open-arrow" />
                </button>
              );
            })}
            {!visible.length && !error && (
              <div className="activity-empty">
                {loading ? (
                  <LoaderCircle size={28} className="running" />
                ) : (
                  <Activity size={28} />
                )}
                <strong>
                  {loading
                    ? "正在读取任务…"
                    : !ready && !items
                      ? "连接后查看任务与请求"
                      : search
                        ? "没有匹配的任务"
                        : filter === "attention"
                          ? "当前没有待处理的任务"
                          : "当前没有进行中的任务"}
                </strong>
                {!loading && ready && (
                  <p>
                    {search
                      ? "试试其他名称、设备或项目。"
                      : "开始对话后，可从这里回到正在进行的工作。"}
                  </p>
                )}
              </div>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
