import { t } from "../localization.ts";
import { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { deviceRequest } from "../localDevice";
import type { CleanupInfo, Result, Snapshot } from "./localProtocol";

export function CleanupPanel({
  snapshot,
  busy,
  revision,
  onWork,
  onSnapshot,
}: {
  snapshot: Snapshot | null;
  busy: boolean;
  revision: number;
  onWork: (action: () => Promise<void>) => Promise<void>;
  onSnapshot: (snapshot: Snapshot) => void;
}) {
  const [info, setInfo] = useState<CleanupInfo | null>(null);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const preparation = useRef("");
  const supported =
    !!snapshot?.configured && !!snapshot.operations?.includes("cleanup.status");

  function accept(result: Result, requestGeneration: number) {
    if (generation.current !== requestGeneration) return;
    setInfo(result.cleanup ?? null);
    onSnapshot(result.snapshot);
  }

  async function refresh(id?: string) {
    const requestGeneration = ++generation.current;
    setReading(true);
    setError("");
    try {
      const result = await deviceRequest({
        operation: "cleanup.status",
        cleanup: id ? { id } : undefined,
      });
      accept(result, requestGeneration);
      return generation.current === requestGeneration
        ? result.cleanup
        : undefined;
    } catch (cause) {
      if (generation.current === requestGeneration)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (generation.current === requestGeneration) setReading(false);
    }
  }

  useEffect(() => {
    preparation.current = "";
    setInfo(null);
    if (supported) void refresh();
    return () => {
      generation.current++;
    };
  }, [supported, snapshot?.deviceId, snapshot?.state, revision]);

  const ordinaryLifecycle =
    snapshot?.ready ||
    ["starting", "running", "stopping"].includes(snapshot?.state ?? "");
  if (
    !supported ||
    (ordinaryLifecycle && (!info?.id || info.applied)) ||
    (!reading && !error && info?.state === "clear")
  )
    return null;
  const locked = busy || reading;
  const sameDevice = info?.deviceId === snapshot?.deviceId;

  async function mutate(operation: "cleanup.prepare" | "cleanup.apply") {
    await onWork(async () => {
      setError("");
      const id =
        operation === "cleanup.apply"
          ? info?.id
          : preparation.current ||
            (preparation.current = crypto.randomUUID().replaceAll("-", ""));
      if (!id || !snapshot?.deviceId) return;
      const requestGeneration = ++generation.current;
      try {
        const result = await deviceRequest({
          operation,
          expectedDeviceId: snapshot.deviceId,
          cleanup: { id },
        });
        accept(result, requestGeneration);
      } catch (cause) {
        if (generation.current !== requestGeneration) return;
        // A response can be lost after the marker switch. Read this exact plan;
        // never retry a mutation automatically or invent a second plan.
        const observed = await refresh(id);
        if (operation === "cleanup.apply" && observed?.applied) return;
        if (
          operation === "cleanup.prepare" &&
          observed?.id === id &&
          observed.state !== "changed"
        )
          return;
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
  }

  return (
    <section className="device-data" aria-label={t("本机服务状态恢复")}>
      <div className="device-operation-title">
        <h3>{t("恢复本机状态")}</h3>
        <button
          type="button"
          disabled={locked}
          aria-label={t("重新检查本机恢复状态")}
          onClick={() =>
            void onWork(async () => {
              await refresh();
            })
          }
        >
          <RefreshCw size={14} />
        </button>
      </div>
      {reading && <p role="status">{t("正在核对上次服务的退出状态…")}</p>}
      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}
      {info?.pending && info.state !== "in-use" && (
        <p>
          {t("上次服务的后台任务尚未确认全部退出，当前设备保留了启动限制。")}
        </p>
      )}
      {!!info?.unconfirmedExecutions && !info.pending && !info.applied && (
        <p>
          {t(
            "有 {0} 个旧执行的结果仍未核实。保存检查记录并重新启动电脑后，可继续处理原对话的项目占用。",
            info.unconfirmedExecutions,
          )}
        </p>
      )}
      {info?.message && <p role="status">{info.message}</p>}
      {info?.canPrepare && !info.id && (
        <p>
          {t(
            "先保存本次检查记录。需要重新启动电脑时，可在重启后回到这里继续。",
          )}
        </p>
      )}
      {info?.id && (
        <p className="muted">
          {t("检查记录已保存在这台电脑上，关闭窗口后仍可继续查看。")}
        </p>
      )}
      {info?.canApply && (
        <p>
          {t(
            "确认后可重新启动本机服务。原任务结果和队列仍需核对，此操作不会启动模型或重新发送输入。",
          )}
        </p>
      )}
      <div className="local-device-actions">
        {info?.canPrepare && !info.id && (
          <button
            type="button"
            disabled={locked || !sameDevice}
            onClick={() => void mutate("cleanup.prepare")}
          >
            {t("保存检查记录")}
          </button>
        )}
        {info?.id && !info.applied && (
          <button
            type="button"
            disabled={locked || !sameDevice || !info.canApply}
            onClick={() => void mutate("cleanup.apply")}
          >
            {t("确认恢复")}
          </button>
        )}
        {info?.state === "changed" && (
          <button
            type="button"
            disabled={locked}
            onClick={() =>
              void onWork(async () => {
                await refresh();
              })
            }
          >
            {t("核对当前状态")}
          </button>
        )}
      </div>
    </section>
  );
}
