import { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { deviceRequest } from "../localDevice";
import type { HubProxyInfo, HubProxySettings, Result, Snapshot } from "../localProtocol";

const defaults: HubProxySettings = {
  enabled: false, host: "", port: 22, user: "", identityFile: "",
  knownHostsFile: "", remoteBind: "127.0.0.1", remotePort: 47834, publicUrl: "",
};
const states: Record<string, string> = {
  disabled: "反向代理已关闭", waiting: "等待 Hub 启动或采用设置", connecting: "正在连接",
  connected: "公网入口已连接", verifying: "正在检查公网入口", reconnecting: "正在重连",
  stopped: "代理已停止", error: "代理设置需要处理",
};

export function HubProxyPanel({ snapshot, busy, revision, onWork, onChange }: {
  snapshot: Snapshot | null; busy: boolean; revision: number;
  onWork: (work: () => Promise<void>) => Promise<void>;
  onChange: (request: { operation: string; hubProxy?: HubProxySettings }) => Promise<Result>;
}) {
  const [info, setInfo] = useState<HubProxyInfo | null>(null);
  const [draft, setDraft] = useState(defaults);
  const [error, setError] = useState("");
  const [reading, setReading] = useState(false);
  const [dirty, setDirty] = useState(false);
  const generation = useRef(0);
  const dirtyRef = useRef(false);
  const supported = snapshot?.operations?.includes("proxy.apply");

  async function refresh(replace = false) {
    const current = ++generation.current;
    setReading(true);
    try {
      const result = await deviceRequest({ operation: "proxy.status" });
      if (generation.current !== current) return;
      setInfo(result.hubProxy ?? null);
      if ((replace || !dirtyRef.current) && result.hubProxy) {
        setDraft(result.hubProxy.settings); setDirty(false); dirtyRef.current = false;
      }
    } catch (cause) {
      if (generation.current === current) setError(cause instanceof Error ? cause.message : String(cause));
    } finally { if (generation.current === current) setReading(false); }
  }

  useEffect(() => {
    if (!snapshot?.hub || !supported) return;
    setError(""); void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => { generation.current++; window.clearInterval(timer); };
  }, [snapshot?.deviceId, snapshot?.hub, supported, revision]);

  if (!snapshot?.hub) return null;
  const locked = busy || reading;
  function edit(patch: Partial<HubProxySettings>) {
    setDraft((value) => ({ ...value, ...patch })); setDirty(true); dirtyRef.current = true;
  }
  async function save(importLegacy = false) {
    await onWork(async () => {
      setError("");
      try {
        const result = await onChange(importLegacy ? { operation: "proxy.import" } : { operation: "proxy.apply", hubProxy: draft });
        if (result.hubProxy) { setInfo(result.hubProxy); setDraft(result.hubProxy.settings); setDirty(false); dirtyRef.current = false; }
      } catch (cause) {
        // Observe the result without resubmitting an uncertain migration.
        await refresh(); setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
  }

  return <section className="device-data hub-proxy-settings" aria-label="Hub 公网反向代理">
    <div className="device-operation-title">
      <h3>公网反向代理</h3>
      <button type="button" disabled={locked || !supported} aria-label="刷新反向代理状态" onClick={() => void refresh()}><RefreshCw size={14} /></button>
    </div>
    <p className="muted">由 MewDex Hub 维护连接。启用后保存设置，Hub 每次启动时自动连接；网络中断后自动重连。</p>
    {!supported ? <p>请先在“数据与更新”中更新本机服务组件。</p> : <>
      {error && <div className="error" role="alert">{error}</div>}
      {info && <p role="status">{states[info.state] ?? info.state}{info.message && ` · ${info.message}`}</p>}
      <form className="local-setup-form" onSubmit={(event) => { event.preventDefault(); void save(); }}>
        <label className="setting-toggle">
          <input type="checkbox" role="switch" checked={draft.enabled} disabled={locked || !info} onChange={(event) => edit({ enabled: event.target.checked })} />
          启用公网反向代理
        </label>
        <fieldset disabled={locked || !info || !draft.enabled}>
          <label>公网 Hub 地址<input type="url" required={draft.enabled} placeholder="https://mewdex.example.com" value={draft.publicUrl} onChange={(event) => edit({ publicUrl: event.target.value })} /></label>
          <label>SSH 服务器<input required={draft.enabled} placeholder="服务器域名或 IP" value={draft.host} onChange={(event) => edit({ host: event.target.value })} /></label>
          <div className="hub-proxy-columns">
            <label>SSH 端口<input type="number" min={1} max={65535} required={draft.enabled} value={draft.port || ""} onChange={(event) => edit({ port: Number(event.target.value) })} /></label>
            <label>SSH 用户<input required={draft.enabled} value={draft.user} autoComplete="off" onChange={(event) => edit({ user: event.target.value })} /></label>
          </div>
          <label>SSH 私钥文件<input required={draft.enabled} value={draft.identityFile} placeholder="专用私钥的完整路径" autoComplete="off" onChange={(event) => edit({ identityFile: event.target.value })} /></label>
          <label>服务器主机密钥文件（known_hosts）<input required={draft.enabled} value={draft.knownHostsFile} placeholder="已核对主机密钥的 known_hosts 文件路径" onChange={(event) => edit({ knownHostsFile: event.target.value })} /></label>
          <div className="hub-proxy-columns">
            <label>远端监听地址<input required={draft.enabled} value={draft.remoteBind} onChange={(event) => edit({ remoteBind: event.target.value })} /></label>
            <label>远端转发端口<input type="number" min={1} max={65535} required={draft.enabled} value={draft.remotePort || ""} onChange={(event) => edit({ remotePort: Number(event.target.value) })} /></label>
          </div>
          <p className="muted">服务器需预先配置 HTTPS 反向代理和 SSH 转发权限。私钥只引用本机文件，不上传到服务器。</p>
        </fieldset>
        <div className="local-device-actions">
          <button type="submit" disabled={locked || !info || !dirty}>保存代理设置</button>
          {dirty && <button type="button" disabled={locked} onClick={() => { setError(""); void refresh(true); }}>放弃更改</button>}
          {info?.legacyAvailable && <button type="button" disabled={locked || dirty} onClick={() => void save(true)}>接管旧 SSH 代理</button>}
        </div>
      </form>
      {info?.legacyAvailable && <p className="muted">接管会保留原连接参数，关闭旧代理的独立登录项和进程，交由 Hub 管理。</p>}
      <p className="muted">关闭代理会断开公网连接并保留参数，本机 Hub 继续运行。开机自启动由上方的独立开关控制。</p>
    </>}
  </section>;
}
