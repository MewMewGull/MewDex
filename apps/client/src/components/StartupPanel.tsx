import { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { deviceRequest } from "../localDevice";
import type { Result, Snapshot, StartupInfo } from "../localProtocol";

export function StartupPanel({
  snapshot,
  busy,
  revision,
  onWork,
  onChange,
}: {
  snapshot: Snapshot | null;
  busy: boolean;
  revision: number;
  onWork: (action: () => Promise<void>) => Promise<void>;
  onChange: (enabled: boolean) => Promise<Result>;
}) {
  const [info, setInfo] = useState<StartupInfo | null>(null);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);

  async function refresh() {
    const requestGeneration = ++generation.current;
    setReading(true);
    setError("");
    try {
      const result = await deviceRequest({ operation: "startup.status" });
      if (generation.current === requestGeneration)
        setInfo(result.startup ?? null);
    } catch (cause) {
      if (generation.current === requestGeneration)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (generation.current === requestGeneration) setReading(false);
    }
  }

  useEffect(() => {
    setInfo(null);
    if (snapshot?.configured) void refresh();
    return () => {
      generation.current++;
    };
  }, [snapshot?.deviceId, snapshot?.state, snapshot?.configured, revision]);

  if (!snapshot?.configured) return null;
  const locked = busy || reading;
  const needsReview =
    info?.requestedEnabled &&
    (!info.matchesProfile || !info.entryMatches || !info.enabled);

  async function apply(enabled: boolean) {
    await onWork(async () => {
      setError("");
      try {
        const result = await onChange(enabled);
        generation.current++;
        setReading(false);
        setInfo(result.startup ?? null);
      } catch (cause) {
        // Read the outcome only; never repeat a registration after a lost response.
        await refresh();
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
  }

  return (
    <section
      className="device-data startup-settings"
      aria-label="开机自启动设置"
    >
      <div className="device-operation-title">
        <h3>{snapshot.hub ? "开机自启动 MewDex Hub" : "开机自启动本机服务"}</h3>
        <button
          type="button"
          disabled={locked}
          aria-label="刷新自动启动状态"
          onClick={() => void onWork(refresh)}
        >
          <RefreshCw size={14} />
        </button>
      </div>
      {reading && <p role="status">正在核对自动启动设置…</p>}
      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}
      {info && !info.supported && (
        <p className="muted">此系统暂未提供可用的自动启动管理。</p>
      )}
      {info?.supported && (
        <>
          <p>
            {info.scope === "system-boot"
              ? "开机后自动启动后台服务。"
              : snapshot.hub && info.scope === "user-login" ? "登录本机账户后静默启动 MewDex Hub；Windows 上显示在托盘。" : "登录本机账户后自动启动后台服务。"}
            关闭工作台不影响已经运行的服务。
          </p>
          <p role="status">
            {info.pending
              ? "上次设置结果待核对，刷新不会重复提交。"
              : needsReview
                ? "设备配置或服务入口已变化，需要重新核对并启用。"
                : info.enabled
                  ? "自动启动已启用。"
                  : "自动启动已关闭。"}
          </p>
          <label className="setting-toggle">
            <input type="checkbox" role="switch" checked={info.requestedEnabled} disabled={locked} onChange={(event) => void apply(event.target.checked)} />
            开机自启动
          </label>
          <div className="local-device-actions">
            {needsReview && <button type="button" disabled={locked} onClick={() => void apply(true)}>重新核对并启用</button>}
            {info.registered && !info.requestedEnabled && <button type="button" disabled={locked} onClick={() => void apply(false)}>移除遗留启动项</button>}
          </div>
          <p className="muted">
            关闭此设置会保留当前运行的服务。恢复或修改设备配置后需重新核对；自动启动不会提交模型输入或恢复冻结的队列。
          </p>
        </>
      )}
    </section>
  );
}
