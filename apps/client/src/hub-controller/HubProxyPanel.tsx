import { useEffect, useId, useRef, useState } from "react";
import { Check, ChevronDown, RefreshCw, Server } from "lucide-react";
import { deviceRequest } from "../localDevice";
import type {
  HubProxyInfo,
  HubProxySettings,
  SSHConfigInfo,
  Request,
  Result,
  Snapshot,
} from "./localProtocol";
import { useSettingAutosave } from "../useSettingAutosave";
import { fillSSHProfile } from "./hubProxySSH";

const defaults: HubProxySettings = {
  enabled: false,
  host: "",
  port: 22,
  user: "",
  identityFile: "",
  knownHostsFile: "",
  remoteBind: "127.0.0.1",
  remotePort: 47834,
};
const states: Record<string, string> = {
  disabled: "已关闭",
  waiting: "等待启动",
  connecting: "连接中",
  connected: "已连接",
  verifying: "SSH 已连接",
  reconnecting: "重连中",
  stopped: "已停止",
  error: "配置错误",
};

function validSSHSettings(value: HubProxySettings) {
  return (
    [value.host, value.user, value.identityFile, value.knownHostsFile].every(
      (text) => text.trim(),
    ) &&
    Number.isInteger(value.port) &&
    value.port >= 1 &&
    value.port <= 65535
  );
}

function validSettings(value: HubProxySettings) {
  if (!value.enabled) return true;
  return (
    validSSHSettings(value) &&
    !!value.remoteBind.trim() &&
    Number.isInteger(value.remotePort) &&
    value.remotePort >= 1 &&
    value.remotePort <= 65535
  );
}

export function HubProxyPanel({
  active = true,
  snapshot,
  busy,
  revision,
  onWork,
  onChange,
  onPendingChange,
  importDisabled,
  onRead = deviceRequest,
}: {
  active?: boolean;
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
  const [chooserOpen, setChooserOpen] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{
    ok: boolean;
    message: string;
  } | null>(null);
  const fieldId = useId();
  const hostInput = useRef<HTMLInputElement>(null);
  const testGeneration = useRef(0);
  const sshGeneration = useRef(0);
  const generation = useRef(0);
  const busyRef = useRef(busy);
  const readingRef = useRef(false);
  busyRef.current = busy;
  const supported = snapshot?.operations?.includes("proxy.apply");
  const sshSupported = snapshot?.operations?.includes("proxy.ssh.config");
  const testSupported = snapshot?.operations?.includes("proxy.test");
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
    if (!active || !snapshot?.hub || !supported) return;
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
  }, [active, snapshot?.deviceId, snapshot?.hub, supported, revision]);

  useEffect(() => {
    setSSHConfig(null);
    setSSHAlias("");
    setSSHError("");
    setChooserOpen(false);
    setSSHReading(false);
    return () => {
      sshGeneration.current++;
    };
  }, [snapshot?.deviceId]);

  useEffect(() => {
    setTesting(false);
    setTestResult(null);
    return () => {
      testGeneration.current++;
    };
  }, [active, snapshot?.deviceId]);

  useEffect(() => {
    if (!active || !snapshot?.hub || !sshSupported) return;
    void readSSHConfig();
    return () => {
      sshGeneration.current++;
    };
  }, [active, snapshot?.deviceId, snapshot?.hub, sshSupported]);

  async function readSSHConfig() {
    const current = ++sshGeneration.current;
    setSSHReading(true);
    setSSHError("");
    try {
      const result = await onRead({
        operation: "proxy.ssh.config",
        expectedDeviceId: snapshot?.deviceId,
      });
      if (sshGeneration.current !== current) return;
      if (!result.sshConfig)
        throw new Error("本机组件未返回 SSH 配置，请更新本机服务组件。");
      const config = result.sshConfig;
      setSSHConfig(config);
      setSSHAlias((alias) =>
        config.profiles.some((profile) => profile.alias === alias) ? alias : "",
      );
    } catch (cause) {
      if (sshGeneration.current === current)
        setSSHError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (sshGeneration.current === current) setSSHReading(false);
    }
  }

  if (!snapshot?.hub) return null;
  const locked = busy || reading || testing;
  const localTarget = `127.0.0.1:${snapshot.listen?.split(":").at(-1) || "47830"}`;
  const defaultKnownHosts =
    sshConfig?.configPath.replace(/[^\\/]+$/, "known_hosts") ?? "";
  function edit(patch: Partial<HubProxySettings>) {
    const value = { ...draft, ...patch };
    if (!value.knownHostsFile && patch.knownHostsFile === undefined)
      value.knownHostsFile = defaultKnownHosts;
    if (
      ["host", "port", "user", "identityFile", "knownHostsFile"].some(
        (field) => field in patch,
      )
    )
      setSSHAlias("");
    setDraft(value);
    setError("");
    testGeneration.current++;
    setTestResult(null);
    setTesting(false);
    queue.change(value, validSettings(value));
  }
  function useSSHProfile(alias: string) {
    setSSHAlias(alias);
    const profile = sshConfig?.profiles.find((item) => item.alias === alias);
    if (!profile?.importable) return;
    const value = fillSSHProfile(draft, profile);
    edit(value);
    setSSHAlias(alias);
    setChooserOpen(false);
  }

  function enterManual() {
    setChooserOpen(false);
    if (!draft.knownHostsFile && defaultKnownHosts) {
      edit({ knownHostsFile: defaultKnownHosts });
    }
    hostInput.current?.focus();
  }

  async function testConnection() {
    if (testing || !validSSHSettings(draft)) return;
    const current = ++testGeneration.current;
    setTesting(true);
    setTestResult(null);
    try {
      const result = await onRead({
        operation: "proxy.test",
        expectedDeviceId: snapshot?.deviceId,
        hubProxy: { ...draft },
      });
      if (current !== testGeneration.current) return;
      if (!result.sshTest)
        throw new Error("本机组件未返回测试结果，请更新服务组件。");
      setTestResult({
        ok: true,
        message: `SSH 连接成功 · ${result.sshTest.durationMs} ms`,
      });
    } catch (cause) {
      if (current === testGeneration.current)
        setTestResult({
          ok: false,
          message: cause instanceof Error ? cause.message : String(cause),
        });
    } finally {
      if (current === testGeneration.current) setTesting(false);
    }
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
      aria-label="Hub SSH 反向代理"
    >
      <div className="hub-proxy-heading">
        <div className="hub-proxy-heading-text">
          <h3>SSH 反向代理</h3>
          <span
            className={`hub-proxy-status ${info?.state === "connected" ? "is-connected" : ""}`}
            role="status"
          >
            {info ? (states[info.state] ?? info.state) : "正在读取…"}
          </span>
          {saveState.pending && (
            <span className="muted" role="status">
              保存中…
            </span>
          )}
        </div>
        {supported && (
          <label className="setting-toggle">
            <input
              type="checkbox"
              role="switch"
              aria-label="启用 SSH 反向代理"
              checked={draft.enabled}
              disabled={locked || !info}
              onChange={(event) => edit({ enabled: event.target.checked })}
            />
            启用
          </label>
        )}
      </div>
      {!supported ? (
        <p className="muted">请在“数据与更新”中更新本机服务组件。</p>
      ) : (
        <>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          {saveState.error && (
            <p className="error" role="alert">
              {saveState.error}
            </p>
          )}
          {saveState.phase === "invalid" && (
            <p className="error" role="alert">
              请补全有效的连接参数。
            </p>
          )}
          <div className="hub-ssh-editor">
            <div className="hub-ssh-source-row">
              <button
                className="hub-ssh-source-button"
                type="button"
                aria-expanded={chooserOpen}
                aria-controls={`${fieldId}-hosts`}
                disabled={locked || !info || !sshSupported}
                onClick={() => setChooserOpen((value) => !value)}
              >
                <Server size={16} />
                <span>
                  {selectedSSH?.alias || draft.host || "选择 SSH 主机"}
                </span>
                <ChevronDown size={14} />
              </button>
              <button
                className="icon-button"
                type="button"
                aria-label="刷新 SSH 主机"
                title="刷新 SSH 配置"
                disabled={locked || sshReading || !sshSupported}
                onClick={() => void readSSHConfig()}
              >
                <RefreshCw size={15} />
              </button>
              <button
                className="settings-link"
                type="button"
                disabled={locked || !info}
                onClick={enterManual}
              >
                手动填写
              </button>
            </div>
            {sshError && (
              <p className="error" role="alert">
                {sshError}
              </p>
            )}
            {chooserOpen && (
              <div
                className="hub-ssh-host-list"
                id={`${fieldId}-hosts`}
                aria-label="本机 SSH 主机"
              >
                {sshReading ? (
                  <p className="muted">正在读取 SSH 配置…</p>
                ) : sshConfig?.profiles.length ? (
                  sshConfig.profiles.map((profile) => (
                    <button
                      key={profile.alias}
                      className="hub-ssh-host-option"
                      type="button"
                      disabled={locked || importDisabled || !profile.importable}
                      aria-pressed={selectedSSH?.alias === profile.alias}
                      title={profile.warnings.join("\n")}
                      onClick={() => useSSHProfile(profile.alias)}
                    >
                      <Server size={16} />
                      <span>
                        <strong>{profile.alias}</strong>
                        <small>
                          {profile.user}@{profile.host}:{profile.port}
                        </small>
                      </span>
                      {!profile.importable ? (
                        <small>需手动配置</small>
                      ) : (
                        selectedSSH?.alias === profile.alias && (
                          <Check size={17} />
                        )
                      )}
                    </button>
                  ))
                ) : (
                  <p className="muted">没有可用的 SSH 主机，可手动填写。</p>
                )}
              </div>
            )}
            <fieldset
              className="hub-ssh-fields"
              disabled={locked || !info}
              onBlur={(event) => {
                if (
                  !event.currentTarget.contains(
                    event.relatedTarget as Node | null,
                  )
                )
                  void queue.flush();
              }}
            >
              <div className="hub-ssh-row">
                <label htmlFor={`${fieldId}-host`}>主机</label>
                <div className="hub-ssh-host-row">
                  <input
                    ref={hostInput}
                    id={`${fieldId}-host`}
                    required={draft.enabled}
                    placeholder="服务器域名或 IP"
                    value={draft.host}
                    onChange={(event) => edit({ host: event.target.value })}
                  />
                  <label htmlFor={`${fieldId}-port`}>端口</label>
                  <input
                    id={`${fieldId}-port`}
                    aria-label="SSH 端口"
                    type="number"
                    min={1}
                    max={65535}
                    value={draft.port || ""}
                    onChange={(event) =>
                      edit({ port: Number(event.target.value) })
                    }
                  />
                </div>
              </div>
              <div className="hub-ssh-row">
                <label htmlFor={`${fieldId}-user`}>用户名</label>
                <input
                  id={`${fieldId}-user`}
                  required={draft.enabled}
                  autoComplete="off"
                  value={draft.user}
                  onChange={(event) => edit({ user: event.target.value })}
                />
              </div>
              <div className="hub-ssh-row">
                <label htmlFor={`${fieldId}-key`}>私钥文件</label>
                <input
                  id={`${fieldId}-key`}
                  required={draft.enabled}
                  autoComplete="off"
                  placeholder="私钥文件的绝对路径"
                  value={draft.identityFile}
                  onChange={(event) =>
                    edit({ identityFile: event.target.value })
                  }
                />
              </div>
              <div className="hub-ssh-row">
                <label htmlFor={`${fieldId}-bind`}>远端监听</label>
                <div className="hub-ssh-host-row">
                  <input
                    id={`${fieldId}-bind`}
                    aria-label="远端监听 IP"
                    required={draft.enabled}
                    value={draft.remoteBind}
                    onChange={(event) =>
                      edit({ remoteBind: event.target.value })
                    }
                  />
                  <label htmlFor={`${fieldId}-forward-port`}>端口</label>
                  <input
                    id={`${fieldId}-forward-port`}
                    aria-label="远端监听端口"
                    type="number"
                    min={1}
                    max={65535}
                    value={draft.remotePort || ""}
                    onChange={(event) =>
                      edit({ remotePort: Number(event.target.value) })
                    }
                  />
                </div>
              </div>
              <small className="hub-ssh-target muted">
                → 本机 Hub {localTarget}
              </small>
              <details className="hub-ssh-advanced">
                <summary>更多选项</summary>
                <div className="hub-ssh-row">
                  <label htmlFor={`${fieldId}-known-hosts`}>主机密钥</label>
                  <input
                    id={`${fieldId}-known-hosts`}
                    placeholder="known_hosts 文件的绝对路径"
                    value={draft.knownHostsFile}
                    onChange={(event) =>
                      edit({ knownHostsFile: event.target.value })
                    }
                  />
                </div>
                <p className="muted">
                  服务器反向代理的上游应填写远端监听地址。
                </p>
                {info?.legacyAvailable && (
                  <button
                    type="button"
                    disabled={
                      locked ||
                      importDisabled ||
                      queue.dirty ||
                      saveState.pending
                    }
                    onClick={() => void importLegacy()}
                  >
                    接管旧 SSH 代理
                  </button>
                )}
              </details>
            </fieldset>
            <div className="hub-ssh-test-row">
              <button
                type="button"
                disabled={testing || !testSupported || !validSSHSettings(draft)}
                aria-busy={testing}
                title={
                  testSupported
                    ? "验证 SSH 服务器和私钥认证"
                    : "更新本机服务组件以使用测试连接"
                }
                onClick={() => void testConnection()}
              >
                {testing ? "测试中…" : "测试连接"}
              </button>
              {testResult && (
                <span
                  className={testResult.ok ? "hub-ssh-test-success" : "error"}
                  role={testResult.ok ? "status" : "alert"}
                >
                  {testResult.ok && <Check size={15} />}
                  <span>{testResult.message}</span>
                </span>
              )}
            </div>
          </div>
        </>
      )}
    </section>
  );
}
