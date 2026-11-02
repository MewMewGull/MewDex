import { useEffect, useState } from "react";
import { Monitor, RefreshCw, WifiOff } from "lucide-react";
import { sameNetworkAddress } from "../connectionRead";
import { useLocalDevice } from "../useLocalDevice";

export function LocalServiceReconnect({
  address,
  onRetry,
  prominent = false,
  onAvailable,
}: {
  address: string;
  onRetry: () => void;
  prominent?: boolean;
  onAvailable?: (value: boolean) => void;
}) {
  const device = useLocalDevice(
    true,
    async () => {},
    () => {},
  );
  const snapshot = device.snapshot;
  const localAddress = snapshot?.baseUrl;
  const matches =
    !!snapshot?.hub &&
    !!localAddress &&
    sameNetworkAddress(address, localAddress);
  const available =
    matches && (snapshot?.state === "stopped" || device.working);
  useEffect(() => {
    onAvailable?.(available);
    return () => onAvailable?.(false);
  }, [available, onAvailable]);
  if (!matches || !snapshot) return null;
  if (snapshot.state !== "stopped" && !device.working && !device.error)
    return null;
  return (
    <div className="local-service-reconnect">
      <Monitor size={16} />
      <div>
        <strong>
          {device.working
            ? "正在启动本机服务…"
            : device.phase || "本机服务已停止"}
        </strong>
        <p>启动后继续使用已有配对连接这个网络。</p>
        {device.error && (
          <p className="error" role="alert">
            {device.error}
          </p>
        )}
      </div>
      <button
        className={prominent ? "primary" : undefined}
        disabled={device.busy || snapshot.state !== "stopped"}
        onClick={() =>
          void device.work(async () => {
            await device.apply({
              operation: "start",
              expectedDeviceId: snapshot.deviceId,
            });
            onRetry();
          })
        }
      >
        {device.working ? "正在启动…" : "启动服务并重试"}
      </button>
    </div>
  );
}

export function NetworkConnectionNotice({
  connected,
  reconnecting,
  error,
  address,
  localAvailable,
  onRetry,
  onLocalDevice,
}: {
  connected: boolean;
  reconnecting: boolean;
  error?: string;
  address: string;
  localAvailable: boolean;
  onRetry: () => void;
  onLocalDevice?: () => void;
}) {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (connected) {
      setVisible(false);
      return;
    }
    const timer = setTimeout(() => setVisible(true), 1500);
    return () => clearTimeout(timer);
  }, [connected]);
  if (connected || !visible) return null;
  return (
    <section className="network-connection-notice" aria-label="设备网络连接">
      <div className="network-connection-summary" role="status">
        <WifiOff size={18} />
        <div>
          <strong>
            {reconnecting ? "正在重新连接设备网络" : "设备网络暂时不可用"}
          </strong>
          <p>可以继续编辑草稿，连接恢复后再发送。</p>
        </div>
        <button onClick={onRetry}>
          <RefreshCw size={14} /> 立即重试
        </button>
      </div>
      {localAvailable && (
        <LocalServiceReconnect address={address} onRetry={onRetry} />
      )}
      {onLocalDevice && (
        <button className="network-connection-manage" onClick={onLocalDevice}>
          查看本机服务
        </button>
      )}
      {error && (
        <details className="connection-recovery-details">
          <summary>连接详情</summary>
          <p>{address}</p>
          <p>{error}</p>
        </details>
      )}
    </section>
  );
}
