import { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { deviceRequest } from "../localDevice";
import type {
  HubProxyInfo,
  HubProxySettings,
  SSHConfigInfo,
  Request,
  Result,
  Snapshot,
} from "../localProtocol";
import { useSettingAutosave } from "../useSettingAutosave";
import { fillSSHProfile } from "../hubProxySSH";

const defaults: HubProxySettings = {
  enabled: false,
  host: "",
  port: 22,
  user: "",
  identityFile: "",
  knownHostsFile: "",
  remoteBind: "127.0.0.1",
  remotePort: 47834,
  publicUrl: "",
};
const states: Record<string, string> = {
  disabled: "反向代理已关闭",
  waiting: "等待 Hub 启动或采用设置",
  connecting: "正在连接",
  connected: "公网入口已连接",
  verifying: "正在检查公网入口",
  reconnecting: "正在重连",
  stopped: "代理已停止",
  error: "代理设置需要处理",
};

function validSettings(value: HubProxySettings) {
  if (!value.enabled) return true;
  if (
    ![
      value.host,
      value.user,
      value.identityFile,
      value.knownHostsFile,
      value.remoteBind,
    ].every((text) => text.trim())
  )
    return false;
  if (
    ![value.port, value.remotePort].every(
      (port) => Number.isInteger(port) && port >= 1 && port <= 65535,
    )
  )
    return false;
  try {
    const url = new URL(value.publicUrl);
    return (
      url.protocol === "https:" &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      url.pathname === "/"
    );
  } catch {
    return false;
  }
}

export function HubProxyPanel({
  snapshot,
  busy,
  revision,
  onWork,
  onChange,
  onPendingChange,
  importDisabled,
  onRead = deviceRequest,
}: {
  snapshot: Snapshot | null;
  busy: boolean;
  revision: number;
  onWork: (work: () => Promise<void>) => Promise<void>;
  onChange: (request: {
    operation: string;
    hubProxy?: HubProxySettings;
  }) => Promise<Result>;
  onPendingChange: (pending: boolean) => void;
  importDisabled: boolean;
  onRead?: (request: Request) => Promise<Result>;
}) {
  const [info, setInfo] = useState<HubProxyInfo | null>(null);
  const [draft, setDraft] = useState(defaults);
  const [error, setError] = useState("");
  const [reading, setReading] = useState(false);
  const [sshConfig, setSSHConfig] = useState<SSHConfigInfo | null>(null);
  const [sshAlias, setSSHAlias] = useState("");
  const [sshReading, setSSHReading] = useState(false);
  const [sshError, setSSHError] = useState("");
  const [sshApplied, setSSHApplied] = useState("");
  const sshGeneration = useRef(0);
  const generation = useRef(0);
  const busyRef = useRef(busy);
  const readingRef = useRef(false);
  busyRef.current = busy;
  const supported = snapshot?.operations?.includes("proxy.apply");
  const sshSupported = snapshot?.operations?.includes("proxy.ssh.config");
  const selectedSSH = sshConfig?.profiles.find(
    (profile) => profile.alias === sshAlias,
  );
  const { queue, state: saveState } = useSettingAutosave(
    defaults,
    async (value) => {
      generation.current++;
      setError("");
      try {
        const result = await onChange({
          operation: "proxy.apply",
          hubProxy: value,
        });
        if (result.hubProxy) setInfo(result.hubProxy);
        return result;
      } finally {
        generation.current++;
        setReading(false);
        readingRef.current = false;
      }
    },
    onPendingChange,
  );

  async function refresh(quiet = false) {
    const current = ++generation.current;
    if (!quiet) {
      setReading(true);
      readingRef.current = true;
    }
    try {
      const result = await onRead({ operation: "proxy.status" });
      if (generation.current !== current) return;
      setInfo(result.hubProxy ?? null);
      if (result.hubProxy && queue.adopt(result.hubProxy.settings)) {
        setDraft(result.hubProxy.settings);
      }
    } catch (cause) {
      if (generation.current === current)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (generation.current === current && !quiet) {
        setReading(false);
        readingRef.current = false;
      }
    }
  }

  useEffect(() => {
    if (!snapshot?.hub || !supported) return;
    setError("");
    void refresh(info !== null);
    const timer = window.setInterval(() => {
      if (!busyRef.current && !readingRef.current && !queue.state.pending)
        void refresh(true);
    }, 5000);
    return () => {
      generation.current++;
      window.clearInterval(timer);
    };
  }, [snapshot?.deviceId, snapshot?.hub, supported, revision]);

  useEffect(() => {
    setSSHConfig(null);
    setSSHAlias("");
    setSSHError("");
    setSSHApplied("");
    setSSHReading(false);
    return () => {
      sshGeneration.current++;
    };
  }, [snapshot?.deviceId]);

  async function readSSHConfig() {
    const current = ++sshGeneration.current;
    setSSHReading(true);
    setSSHError("");
    setSSHApplied("");
    try {
      const result = await onRead({
        operation: "proxy.ssh.config",
        expectedDeviceId: snapshot?.deviceId,
      });
      if (sshGeneration.current !== current) return;
      if (!result.sshConfig)
        throw new Error("本机组件未返回 SSH 配置，请更新本机服务组件。");
      setSSHConfig(result.sshConfig);
      setSSHAlias("");
    } catch (cause) {
      if (sshGeneration.current === current)
        setSSHError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (sshGeneration.current === current) setSSHReading(false);
    }
  }

  if (!snapshot?.hub) return null;
  const locked = busy || reading;
  function edit(patch: Partial<HubProxySettings>) {
    const value = { ...draft, ...patch };
    setDraft(value);
    setError("");
    setSSHApplied("");
    queue.change(value, validSettings(value));
  }
  function useSSHProfile() {
    if (!selectedSSH?.importable) return;
    const value = fillSSHProfile(draft, selectedSSH);
    edit(value);
    setSSHApplied(
      `已填入 ${selectedSSH.alias} 的连接信息；下方所有字段仍可自定义。`,
    );
  }
  async function importLegacy() {
    await onWork(async () => {
      generation.current++;
      setError("");
      try {
        const result = await onChange({ operation: "proxy.import" });
        generation.current++;
        setReading(false);
        readingRef.current = false;
        if (result.hubProxy) {
          setInfo(result.hubProxy);
          if (queue.adopt(result.hubProxy.settings))
            setDraft(result.hubProxy.settings);
        }
      } catch (cause) {
        // Observe the result without resubmitting an uncertain migration.
        await refresh();
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
  }

  return (
    <section
      className="device-data hub-proxy-settings"
      aria-label="Hub 公网反向代理"
    >
      <div className="device-operation-title">
        <h3>公网反向代理</h3>
        <button
          type="button"
          disabled={locked || !supported}
          aria-label="刷新反向代理状态"
          onClick={() => void refresh()}
        >
          <RefreshCw size={14} />
        </button>
      </div>
      <p className="muted">
        由 MewDex Hub 维护连接。修改后自动保存，Hub
        每次启动时自动连接；网络中断后自动重连。
      </p>
      {!supported ? (
        <p>请先在“数据与更新”中更新本机服务组件。</p>
      ) : (
        <>
          {error && (
            <div className="error" role="alert">
              {error}
            </div>
          )}
          {saveState.error && (
            <div className="error" role="alert">
              {saveState.error}
            </div>
          )}
          <p role="status">
            {saveState.pending
              ? "正在自动保存…"
              : saveState.phase === "saved"
                ? "已自动保存"
                : saveState.phase === "invalid"
                  ? "请补全有效的代理配置，完整后将自动保存并启用。"
                  : "设置会自动保存。"}
          </p>
          {info && (
            <p role="status">
              {states[info.state] ?? info.state}
              {info.message && ` · ${info.message}`}
            </p>
          )}
          <section className="hub-ssh-import" aria-label="从本机 SSH 配置读取">
            <div className="device-operation-title">
              <h4>使用本机 SSH 配置</h4>
              <button
                type="button"
                disabled={locked || sshReading || !sshSupported}
                onClick={() => void readSSHConfig()}
              >
                {sshReading
                  ? "正在读取…"
                  : sshConfig
                    ? "重新读取 SSH 配置"
                    : "读取 .ssh/config"}
              </button>
            </div>
            <p className="muted">
              读取这台电脑用户目录下的
              .ssh/config，选择主机后可填入连接信息。只引用密钥文件路径，不读取或显示私钥内容。
            </p>
            {!sshSupported && (
              <p className="muted">
                更新本机服务组件后可读取 SSH 配置；下方手动表单仍可使用。
              </p>
            )}
            {sshError && (
              <p className="error" role="alert">
                {sshError}
              </p>
            )}
            {sshConfig && (
              <>
                <p className="hub-ssh-source">
                  <code>{sshConfig.configPath}</code>
                </p>
                <p className="muted">{sshConfig.message}</p>
                {sshConfig.profiles.length > 0 && (
                  <label>
                    SSH 配置主机
                    <select
                      value={sshAlias}
                      disabled={locked || sshReading}
                      onChange={(event) => {
                        setSSHAlias(event.target.value);
                        setSSHApplied("");
                      }}
                    >
                      <option value="">选择要使用的 Host 别名</option>
                      {sshConfig.profiles.map((profile) => (
                        <option key={profile.alias} value={profile.alias}>
                          {profile.alias} — {profile.user}@{profile.host}:
                          {profile.port}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                {selectedSSH && (
                  <>
                    <dl className="hub-ssh-preview">
                      <div>
                        <dt>SSH 服务器</dt>
                        <dd>
                          {selectedSSH.host}:{selectedSSH.port}
                        </dd>
                      </div>
                      <div>
                        <dt>SSH 用户</dt>
                        <dd>{selectedSSH.user}</dd>
                      </div>
                      <div>
                        <dt>私钥文件</dt>
                        <dd>{selectedSSH.identityFile || "未找到"}</dd>
                      </div>
                      <div>
                        <dt>known_hosts</dt>
                        <dd>{selectedSSH.knownHostsFile || "未找到"}</dd>
                      </div>
                    </dl>
                    {selectedSSH.warnings.map((warning, index) => (
                      <p key={index} className="muted">
                        {warning}
                      </p>
                    ))}
                    <button
                      className="hub-ssh-apply"
                      type="button"
                      disabled={
                        locked ||
                        importDisabled ||
                        saveState.pending ||
                        !selectedSSH.importable
                      }
                      onClick={useSSHProfile}
                    >
                      填入表单并自动保存
                    </button>
                    <p className="muted">
                      保留当前的启用开关、公网 Hub
                      地址和远端转发设置。已启用时，完整配置会自动保存并重新连接。填入后仍可修改下方表单。
                    </p>
                  </>
                )}
              </>
            )}
            {sshApplied && <p role="status">{sshApplied}</p>}
          </section>
          <div className="local-setup-form" onBlur={() => void queue.flush()}>
            <label className="setting-toggle">
              <input
                type="checkbox"
                role="switch"
                checked={draft.enabled}
                disabled={locked || !info}
                onChange={(event) => edit({ enabled: event.target.checked })}
              />
              启用公网反向代理
            </label>
            {draft.enabled && (
              <fieldset disabled={locked || !info}>
                <label>
                  公网 Hub 地址
                  <input
                    type="url"
                    required={draft.enabled}
                    placeholder="https://mewdex.example.com"
                    value={draft.publicUrl}
                    onChange={(event) =>
                      edit({ publicUrl: event.target.value })
                    }
                  />
                  <small className="muted">
                    其他设备访问 Hub 的 HTTPS 地址。它可能与 SSH
                    服务器域名不同。
                  </small>
                </label>
                <label>
                  SSH 服务器
                  <input
                    required={draft.enabled}
                    placeholder="服务器域名或 IP"
                    value={draft.host}
                    onChange={(event) => edit({ host: event.target.value })}
                  />
                </label>
                <div className="hub-proxy-columns">
                  <label>
                    SSH 端口
                    <input
                      type="number"
                      min={1}
                      max={65535}
                      required={draft.enabled}
                      value={draft.port || ""}
                      onChange={(event) =>
                        edit({ port: Number(event.target.value) })
                      }
                    />
                  </label>
                  <label>
                    SSH 用户
                    <input
                      required={draft.enabled}
                      value={draft.user}
                      autoComplete="off"
                      onChange={(event) => edit({ user: event.target.value })}
                    />
                  </label>
                </div>
                <label>
                  SSH 私钥文件
                  <input
                    required={draft.enabled}
                    value={draft.identityFile}
                    placeholder="专用私钥的完整路径"
                    autoComplete="off"
                    onChange={(event) =>
                      edit({ identityFile: event.target.value })
                    }
                  />
                  <small className="muted">
                    填写私钥文件本身的绝对路径，不要选择 .pub 公钥文件。
                  </small>
                </label>
                <label>
                  服务器主机密钥文件（known_hosts）
                  <input
                    required={draft.enabled}
                    value={draft.knownHostsFile}
                    placeholder="已核对主机密钥的 known_hosts 文件路径"
                    onChange={(event) =>
                      edit({ knownHostsFile: event.target.value })
                    }
                  />
                  <small className="muted">
                    通常是用户目录下的
                    .ssh/known_hosts；需已记录并核对该服务器的主机密钥。
                  </small>
                </label>
                <div className="hub-proxy-columns">
                  <label>
                    远端监听地址
                    <input
                      required={draft.enabled}
                      value={draft.remoteBind}
                      onChange={(event) =>
                        edit({ remoteBind: event.target.value })
                      }
                    />
                  </label>
                  <label>
                    远端转发端口
                    <input
                      type="number"
                      min={1}
                      max={65535}
                      required={draft.enabled}
                      value={draft.remotePort || ""}
                      onChange={(event) =>
                        edit({ remotePort: Number(event.target.value) })
                      }
                    />
                  </label>
                </div>
                <p className="muted">
                  远端转发端口需要与服务器 HTTPS
                  反向代理的上游端口一致；默认使用 127.0.0.1:47834。
                </p>
                <p className="muted">
                  服务器需预先配置 HTTPS 反向代理和 SSH
                  转发权限。私钥只引用本机文件，不上传到服务器。
                </p>
              </fieldset>
            )}
            <div className="local-device-actions">
              {info?.legacyAvailable && (
                <button
                  type="button"
                  disabled={
                    locked || importDisabled || queue.dirty || saveState.pending
                  }
                  onClick={() => void importLegacy()}
                >
                  接管旧 SSH 代理
                </button>
              )}
            </div>
          </div>
          {info?.legacyAvailable && (
            <p className="muted">
              接管会保留原连接参数，关闭旧代理的独立登录项和进程，交由 Hub
              管理。
            </p>
          )}
          <p className="muted">
            关闭代理会断开公网连接并保留参数，本机 Hub
            继续运行。开机自启动由上方的独立开关控制。
          </p>
        </>
      )}
    </section>
  );
}
