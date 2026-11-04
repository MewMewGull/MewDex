import { useEffect, useState } from "react";
import type { Result, Snapshot } from "./localProtocol";
import { useSettingAutosave } from "../useSettingAutosave";

export function HubListenerPanel({
  snapshot,
  busy,
  onChange,
  onPendingChange,
}: {
  snapshot: Snapshot | null;
  busy: boolean;
  onChange: (port: number) => Promise<Result>;
  onPendingChange: (pending: boolean) => void;
}) {
  const current = snapshot?.listen?.split(":").at(-1) ?? "47830";
  const [port, setPort] = useState(current);
  const { queue, state } = useSettingAutosave(
    current,
    (value) => onChange(Number(value)),
    onPendingChange,
  );
  useEffect(() => {
    if (queue.adopt(current)) setPort(current);
  }, [snapshot?.deviceId, current, queue]);
  if (!snapshot?.configured || !snapshot.hub) return null;
  const supported = snapshot.operations?.includes("service.port");
  const stopped = snapshot.state === "stopped";

  return (
    <section
      className="device-data hub-listener-settings"
      aria-label="Hub 监听端口设置"
    >
      <h3>Hub 监听端口</h3>
      <p className="muted">
        默认端口为 47830，修改后自动保存。该端口已有 Hub 时可直接连接并登录。
      </p>
      {state.error && (
        <p className="error" role="alert">
          {state.error}
        </p>
      )}
      <div className="local-setup-form">
        <label>
          端口
          <input
            type="number"
            min={1}
            max={65535}
            step={1}
            required
            value={port}
            disabled={busy || !stopped || !supported}
            onChange={(event) => {
              const value = event.target.value,
                number = Number(value);
              setPort(value);
              queue.change(
                value,
                /^\d+$/.test(value) &&
                  Number.isInteger(number) &&
                  number >= 1 &&
                  number <= 65535,
              );
            }}
            onBlur={() => void queue.flush()}
          />
        </label>
        <p role="status">
          {state.pending
            ? "正在自动保存…"
            : state.phase === "saved"
              ? "已自动保存"
              : state.phase === "invalid"
                ? "请输入 1–65535 之间的整数；未完成的输入不会保存。"
                : "设置会自动保存。"}
        </p>
      </div>
      {!stopped && <p className="muted">先停止本机服务，再修改监听端口。</p>}
      {!supported && (
        <p className="muted">请先更新本机组件，以便修改监听端口。</p>
      )}
      <p className="muted">
        直接连接本机端口的客户端或外部代理，需要同步更新其地址。
      </p>
    </section>
  );
}
