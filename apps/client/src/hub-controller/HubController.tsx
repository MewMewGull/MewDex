import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bell,
  Copy,
  KeyRound,
  Keyboard,
  LogOut,
  History,
  Monitor,
  FilePenLine,
  ShieldCheck,
  Unplug,
  Palette,
  MessageCircle,
  Download,
} from "lucide-react";
import {
  MewDexClient,
  type Access,
  type Device,
  type Identity,
} from "@mewdex/sdk";
import { ModelAccountCard } from "./ModelAccountCard";
import { InputAlertSettings } from "../components/InputAlertSettings";
import type { InputAlerts } from "../input-terminal/useInputAlerts";
import { SettingsFrame } from "../components/SettingsFrame";
import { DesktopPreferences } from "../components/DesktopPreferences";
import {
  DraftSettings,
  type DraftSettingsProps,
} from "../components/DraftSettings";
import { keyboardModifierLabel, sendKeyHint, type SendKey } from "../inputKeys";
import { hasTouchInput } from "../inputViewport";
import { AppearanceSettings } from "../components/AppearanceSettings";
import { CopyButton } from "../components/CopyButton";
import { MewDexAccountSettings } from "./MewDexAccountSettings";
import { MessagingSettings } from "./MessagingSettings";
import { WebWorkbenchButton } from "./WebWorkbenchButton";
import { AppUpdateSettings } from "../components/AppUpdates";

type Section =
  | "account"
  | "general"
  | "connection"
  | "appearance"
  | "drafts"
  | "updates"
  | "pairing"
  | "models"
  | "messaging"
  | "access";

const initialAccess = (): Access => ({
  allDevices: true,
  targets: [],
  gateway: false,
});

function AccessEditor({
  value,
  onChange,
  devices,
  executor,
}: {
  value: Access;
  onChange: (access: Access) => void;
  devices: Device[];
  executor: boolean;
}) {
  return (
    <div className="access-editor">
      {executor ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={value.gateway}
            onChange={(e) => onChange({ ...value, gateway: e.target.checked })}
          />
          允许通过本网络的请求出口连接模型服务
        </label>
      ) : (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={value.allDevices}
              onChange={(e) =>
                onChange({ ...value, allDevices: e.target.checked })
              }
            />
            可操作网络内全部执行设备和项目
          </label>
          {!value.allDevices && (
            <div className="access-targets">
              {devices.map((device) => {
                const target = value.targets.find(
                  (t) => t.deviceId === device.id,
                );
                const setTarget = (checked: boolean) =>
                  onChange({
                    ...value,
                    targets: checked
                      ? [
                          ...value.targets,
                          { deviceId: device.id, projectIds: [] },
                        ]
                      : value.targets.filter((t) => t.deviceId !== device.id),
                  });
                return (
                  <div key={device.id}>
                    <label className="checkbox">
                      <input
                        type="checkbox"
                        checked={!!target}
                        onChange={(e) => setTarget(e.target.checked)}
                      />
                      {device.name}
                    </label>
                    {target && (
                      <div className="project-grants">
                        <small>项目留空表示此设备的全部项目</small>
                        {device.projects.map((project) => (
                          <label className="checkbox" key={project.id}>
                            <input
                              type="checkbox"
                              checked={target.projectIds.includes(project.id)}
                              onChange={(e) =>
                                onChange({
                                  ...value,
                                  targets: value.targets.map((t) =>
                                    t.deviceId === device.id
                                      ? {
                                          ...t,
                                          projectIds: e.target.checked
                                            ? [...t.projectIds, project.id]
                                            : t.projectIds.filter(
                                                (p) => p !== project.id,
                                              ),
                                        }
                                      : t,
                                  ),
                                })
                              }
                            />
                            {project.name}
                          </label>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </>
      )}
    </div>
  );
}

export function HubController({
  client,
  devices,
  identity,
  open,
  onOpenChange,
  alerts,
  baseURL,
  loginKey,
  accountSupported = false,
  onLocalDevice,
  onDisconnect,
  draftSettings,
  workspaceView,
  initialModelDeviceId,
  initialExecutorSetup = false,
}: {
  client: MewDexClient;
  devices: Device[];
  identity: Identity;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  alerts: InputAlerts;
  baseURL: string;
  loginKey: string;
  accountSupported?: boolean;
  onLocalDevice?: () => void;
  onDisconnect: () => void;
  draftSettings: DraftSettingsProps;
  workspaceView: {
    restoreLastLocation: boolean;
    onRestoreLastLocationChange: (value: boolean) => void;
    sendKey: SendKey;
    onSendKeyChange: (value: SendKey) => void;
    error: string;
  };
  initialModelDeviceId?: string;
  initialExecutorSetup?: boolean;
}) {
  const cache = useQueryClient();
  const modifier = keyboardModifierLabel();
  const touch = hasTouchInput();
  const [section, setSection] = useState<Section>("general");
  const identities = useQuery({
    queryKey: ["identities"],
    queryFn: () => client.identities(),
    enabled: open && section === "access" && identity.role === "admin",
  });
  const [role, setRole] = useState<"client" | "executor" | "admin">("client");
  const [accountDevice, setAccountDevice] = useState(devices[0]?.id ?? "");
  useEffect(() => {
    if (!open) return;
    if (initialExecutorSetup && identity.role === "admin") {
      setSection("pairing");
      setRole("executor");
      setAccess({ allDevices: false, targets: [], gateway: false });
      setCode("");
      setError("");
      return;
    }
    const target =
      identity.role === "admin" &&
      initialModelDeviceId &&
      devices.some((device) => device.id === initialModelDeviceId)
        ? initialModelDeviceId
        : undefined;
    setSection(target ? "models" : "general");
    if (target) setAccountDevice(target);
  }, [open, initialModelDeviceId, initialExecutorSetup, identity.role]);
  useEffect(() => {
    if (!devices.some((device) => device.id === accountDevice))
      setAccountDevice(devices[0]?.id ?? "");
  }, [devices, accountDevice]);
  const [access, setAccess] = useState<Access>(initialAccess);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<Identity | null>(null);
  const [editedAccess, setEditedAccess] = useState<Access>(initialAccess);

  async function action(work: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await work();
      await cache.invalidateQueries({ queryKey: ["identities"] });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <SettingsFrame<Section>
      open={open}
      onOpenChange={onOpenChange}
      title="设置"
      description="管理输入偏好、设备连接与授权。"
      selected={section}
      onSelect={(value) => {
        setSection(value);
        setError("");
      }}
      sections={[
        {
          id: "general",
          label: "通用",
          icon: <Bell size={16} />,
          group: "工作台",
        },
        {
          id: "appearance",
          label: "外观",
          icon: <Palette size={16} />,
          group: "工作台",
        },
        {
          id: "drafts",
          label: "草稿",
          icon: <FilePenLine size={16} />,
          group: "工作台",
        },
        {
          id: "updates",
          label: "应用更新",
          icon: <Download size={16} />,
          group: "工作台",
        },
        {
          id: "connection",
          label: "连接与服务",
          icon: <Monitor size={16} />,
          group: "连接",
        },
        {
          id: "account",
          label: "MewDex 账号",
          icon: <KeyRound size={16} />,
          group: "连接",
        },
        ...(identity.role === "admin"
          ? [
              {
                id: "pairing" as const,
                label: "添加设备",
                icon: <KeyRound size={16} />,
                group: "连接",
              },
              {
                id: "models" as const,
                label: "节点 ChatGPT",
                icon: <Monitor size={16} />,
                group: "模型与访问",
              },
              {
                id: "messaging" as const,
                label: "消息渠道",
                icon: <MessageCircle size={16} />,
                group: "模型与访问",
              },
              {
                id: "access" as const,
                label: "访问权限",
                icon: <ShieldCheck size={16} />,
                group: "模型与访问",
              },
            ]
          : []),
      ]}
    >
      {section === "messaging" && identity.role === "admin" && (
        <MessagingSettings client={client} devices={devices} />
      )}
      {section === "updates" && <AppUpdateSettings />}
      {section === "account" && (
        <MewDexAccountSettings
          client={client}
          supported={accountSupported}
          baseURL={baseURL}
          loginKey={loginKey}
          onLogout={onDisconnect}
        />
      )}
      {section === "general" && (
        <>
          <InputAlertSettings alerts={alerts} />
          <DesktopPreferences />
          <section className="network-section">
            <h3>
              <Keyboard size={15} /> 输入与键盘
            </h3>
            <label>
              键盘发送方式
              <select
                value={workspaceView.sendKey}
                onChange={(event) =>
                  workspaceView.onSendKeyChange(event.target.value as SendKey)
                }
              >
                <option value="enter">Enter 发送，Shift + Enter 换行</option>
                <option value="modifier-enter">
                  {modifier} + Enter 发送，Enter 换行
                </option>
              </select>
            </label>
            <p className="muted">
              {touch
                ? `触屏回车用于换行，点按发送或使用 ${modifier} + Enter 发送。`
                : `${sendKeyHint(workspaceView.sendKey, false, modifier)}。切换执行设备时保持。`}
            </p>
            <details className="settings-shortcuts">
              <summary>键盘快捷键</summary>
              <dl>
                {[
                  [
                    "发送消息",
                    touch || workspaceView.sendKey === "modifier-enter"
                      ? `${modifier}+Enter`
                      : "Enter",
                  ],
                  [
                    "换行",
                    touch || workspaceView.sendKey === "modifier-enter"
                      ? "Enter"
                      : "Shift+Enter",
                  ],
                  ["快速切换设备、项目与对话", `${modifier}+K`],
                  ["查找当前对话内容", `${modifier}+F`],
                  ["新对话", `${modifier}+N`],
                  ["聚焦输入框", `${modifier}+L`],
                  ["显示或隐藏导航", `${modifier}+B`],
                  ["打开或关闭终端", `${modifier}+J`],
                  ["查看任务与请求", `${modifier}+Shift+J`],
                  ["打开设置", `${modifier}+,`],
                ].map(([label, key]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>
                      <kbd>{key}</kbd>
                    </dd>
                  </div>
                ))}
              </dl>
            </details>
          </section>
          <section className="network-section">
            <h3>
              <History size={15} />
              工作位置
            </h3>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={workspaceView.restoreLastLocation}
                onChange={(event) =>
                  workspaceView.onRestoreLastLocationChange(
                    event.target.checked,
                  )
                }
              />
              打开工作台时继续上次对话
            </label>
            <p className="muted">
              各台设备分别记住新对话使用的项目，导航展开状态也会保留。恢复位置不会启动任务。
            </p>
            {workspaceView.error && (
              <p className="error" role="alert">
                {workspaceView.error}
              </p>
            )}
          </section>
        </>
      )}
      {section === "connection" && (
        <>
          {onLocalDevice && (
            <section className="network-section">
              <h3>
                <Monitor size={15} />
                本机设备
              </h3>
              <p className="muted">管理本机执行服务、项目、自启动和数据。</p>
              <button
                className="settings-link"
                onClick={() => {
                  onOpenChange(false);
                  onLocalDevice();
                }}
              >
                管理本机设备
              </button>
            </section>
          )}
          <section className="network-section">
            <h3>
              <ShieldCheck size={15} />
              当前连接
            </h3>
            <dl className="connection-details">
              <div>
                <dt>输入端</dt>
                <dd>{identity.name}</dd>
              </div>
              <div>
                <dt>设备网络</dt>
                <dd>{baseURL}</dd>
              </div>
              <div>
                <dt>访问范围</dt>
                <dd>
                  {identity.role === "admin"
                    ? "网络管理员"
                    : identity.access.allDevices
                      ? "全部执行设备"
                      : `${identity.access.targets.length} 台执行设备`}
                </dd>
              </div>
            </dl>
            {onLocalDevice && <WebWorkbenchButton baseURL={baseURL} />}
            <details className="connection-advanced">
              <summary>登录密钥</summary>
              <p className="muted">
                保存此密钥后，退出连接也能用相同网络地址和密钥重新登录。密钥拥有当前输入端的访问权限，请妥善保管；管理员可在访问管理中撤销此输入端。
              </p>
              <CopyButton text={loginKey} label="复制登录密钥" />
            </details>
            <button className="settings-disconnect" onClick={onDisconnect}>
              <LogOut size={15} />
              退出 MewDex
            </button>
            <p className="muted">
              退出会清除当前网络的本机草稿，不退出节点的 ChatGPT 账号。
            </p>
          </section>
        </>
      )}
      {section === "drafts" && <DraftSettings {...draftSettings} />}
      {section === "appearance" && <AppearanceSettings />}
      {identity.role === "admin" && (
        <>
          {section === "pairing" && (
            <section className="network-section">
              <h3>
                <KeyRound size={15} /> 添加输入端或执行端
              </h3>
              <label>
                设备角色
                <select
                  value={role}
                  onChange={(e) => {
                    const role = e.target.value as
                      | "client"
                      | "executor"
                      | "admin";
                    setRole(role);
                    setAccess({
                      allDevices: role === "client",
                      targets: [],
                      gateway: false,
                    });
                    setCode("");
                  }}
                >
                  <option value="client">输入端：手机、桌面或网页</option>
                  <option value="executor">执行端：电脑或服务器</option>
                  <option value="admin">
                    管理员输入端：管理设备、授权与模型登录
                  </option>
                </select>
              </label>
              {role === "admin" ? (
                <p className="muted">管理员输入端可以管理整个设备网络。</p>
              ) : (
                <AccessEditor
                  value={access}
                  onChange={setAccess}
                  devices={devices}
                  executor={role === "executor"}
                />
              )}
              <button
                className="primary"
                disabled={busy}
                onClick={() =>
                  void action(async () => {
                    const result = await client.invite(role, access);
                    setCode(result.code);
                  })
                }
              >
                生成一次性配对码
              </button>
              {code && (
                <div className="pairing-result">
                  <code>{code}</code>
                  <button
                    className="icon-button"
                    aria-label="复制配对码"
                    onClick={() => void navigator.clipboard.writeText(code)}
                  >
                    <Copy size={15} />
                  </button>
                  <small>十分钟内有效，只能使用一次</small>
                </div>
              )}
            </section>
          )}
          {section === "models" && (
            <section className="network-section">
              <h3>
                <ShieldCheck size={15} /> 节点的 ChatGPT 账号与订阅
              </h3>
              <label>
                执行设备
                <select
                  value={accountDevice}
                  onChange={(e) => setAccountDevice(e.target.value)}
                >
                  {devices.map((device) => (
                    <option key={device.id} value={device.id}>
                      {device.name}
                      {device.online ? "" : "（离线）"}
                    </option>
                  ))}
                </select>
              </label>
              {accountDevice && (
                <ModelAccountCard
                  key={accountDevice}
                  client={client}
                  deviceId={accountDevice}
                  online={
                    !!devices.find((device) => device.id === accountDevice)
                      ?.online
                  }
                />
              )}
            </section>
          )}
          {section === "access" && (
            <>
              <section className="network-section">
                <h3>
                  <ShieldCheck size={15} /> MewDex 登录与执行端身份
                </h3>
                {identities.isLoading && <p className="muted">正在加载…</p>}
                {identities.error && (
                  <div className="error">{identities.error.message}</div>
                )}
                {identities.data?.map((item) => (
                  <div className="identity-row" key={item.id}>
                    <div>
                      <strong>
                        {item.name}
                        {item.id === identity.id ? "（当前 MewDex 连接）" : ""}
                      </strong>
                      <small>
                        {item.accountId
                          ? "MewDex 账号"
                          : item.role === "executor"
                            ? "执行端"
                            : item.role === "admin"
                              ? "管理员输入端"
                              : "输入端"}
                        {item.access.gateway ? " · 请求出口已授权" : ""}
                      </small>
                    </div>
                    {item.role !== "admin" && (
                      <button
                        onClick={() => {
                          setEditing(item);
                          setEditedAccess(item.access);
                        }}
                      >
                        访问范围
                      </button>
                    )}
                    <button
                      className="icon-button"
                      aria-label={`撤销 ${item.name}`}
                      disabled={busy || item.id === identity.id}
                      onClick={() => void action(() => client.revoke(item.id))}
                    >
                      <Unplug size={15} />
                    </button>
                  </div>
                ))}
              </section>
              {editing && (
                <section className="network-section access-edit-section">
                  <h3>{editing.name} · 访问范围</h3>
                  <AccessEditor
                    value={editedAccess}
                    onChange={setEditedAccess}
                    devices={devices}
                    executor={editing.role === "executor"}
                  />
                  <button
                    className="primary"
                    disabled={busy}
                    onClick={() =>
                      void action(async () => {
                        await client.updateAccess(editing.id, editedAccess);
                        setEditing(null);
                      })
                    }
                  >
                    保存访问范围
                  </button>
                </section>
              )}
            </>
          )}
        </>
      )}
      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}
    </SettingsFrame>
  );
}
