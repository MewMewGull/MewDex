import { t } from "../localization.ts";
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
  Globe2,
  Sparkles,
} from "lucide-react";
import {
  MewDexClient,
  type Access,
  type Device,
  type Identity,
} from "@mewdex/sdk";
import { HubConnectionPanel, HubGPTPanel } from "./HubConnectionPanel";
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
import { LanguageSettings } from "../components/LanguageSettings";

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
        <p className="muted">
          {t(
            "执行端连接 Hub 后接收执行指令，模型会话和 ChatGPT 账号统一由 Hub 管理。",
          )}
        </p>
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
            {t("可操作网络内全部执行设备和项目")}
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
                        <small>{t("项目留空表示此设备的全部项目")}</small>
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
  onLocalModelRouting,
  onDisconnect,
  onChangeHub,
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
  onLocalModelRouting?: () => void;
  onDisconnect: () => void;
  onChangeHub: () => void;
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
    setSection(initialModelDeviceId ? "models" : "general");
  }, [open, initialModelDeviceId, initialExecutorSetup, identity.role]);
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
      title={t("设置")}
      description={t("管理输入偏好、Hub 连接、共用 GPT 账号和访问权限。")}
      selected={section}
      onSelect={(value) => {
        setSection(value);
        setError("");
      }}
      sections={[
        {
          id: "general",
          label: t("通用"),
          icon: <Bell size={16} />,
          group: t("工作台"),
        },
        {
          id: "appearance",
          label: t("外观"),
          icon: <Palette size={16} />,
          group: t("工作台"),
        },
        {
          id: "drafts",
          label: t("草稿"),
          icon: <FilePenLine size={16} />,
          group: t("工作台"),
        },
        {
          id: "updates",
          label: t("应用更新"),
          icon: <Download size={16} />,
          group: t("工作台"),
        },
        {
          id: "connection",
          label: t("Hub 连接"),
          icon: <Globe2 size={16} />,
          group: t("连接"),
        },
        {
          id: "account",
          label: t("MewDex 账号"),
          icon: <KeyRound size={16} />,
          group: t("连接"),
        },
        ...(identity.role === "admin"
          ? [
              {
                id: "pairing" as const,
                label: t("添加设备"),
                icon: <KeyRound size={16} />,
                group: t("连接"),
              },
            ]
          : []),
        {
          id: "models",
          label: t("GPT 连接"),
          icon: <Sparkles size={16} />,
          group: t("模型与访问"),
        },
        ...(identity.role === "admin"
          ? [
              {
                id: "messaging" as const,
                label: t("消息渠道"),
                icon: <MessageCircle size={16} />,
                group: t("模型与访问"),
              },
              {
                id: "access" as const,
                label: t("访问权限"),
                icon: <ShieldCheck size={16} />,
                group: t("模型与访问"),
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
          <LanguageSettings />
          <InputAlertSettings alerts={alerts} />
          <DesktopPreferences />
          <section className="network-section">
            <h3>
              <Keyboard size={15} />
              {t(" 输入与键盘")}
            </h3>
            <label>
              {t("键盘发送方式")}
              <select
                value={workspaceView.sendKey}
                onChange={(event) =>
                  workspaceView.onSendKeyChange(event.target.value as SendKey)
                }
              >
                <option value="enter">
                  {t("Enter 发送，Shift + Enter 换行")}
                </option>
                <option value="modifier-enter">
                  {t("{0} + Enter 发送，Enter 换行", modifier)}
                </option>
              </select>
            </label>
            <p className="muted">
              {touch
                ? t(
                    "触屏回车用于换行，点按发送或使用 {0} + Enter 发送。",
                    modifier,
                  )
                : t(
                    "{0}。切换执行设备时保持。",
                    sendKeyHint(workspaceView.sendKey, false, modifier),
                  )}
            </p>
            <details className="settings-shortcuts">
              <summary>{t("键盘快捷键")}</summary>
              <dl>
                {[
                  [
                    t("发送消息"),
                    touch || workspaceView.sendKey === "modifier-enter"
                      ? `${modifier}+Enter`
                      : "Enter",
                  ],
                  [
                    t("换行"),
                    touch || workspaceView.sendKey === "modifier-enter"
                      ? "Enter"
                      : "Shift+Enter",
                  ],
                  [t("快速切换设备、项目与对话"), `${modifier}+K`],
                  [t("查找当前对话内容"), `${modifier}+F`],
                  [t("新对话"), `${modifier}+N`],
                  [t("聚焦输入框"), `${modifier}+L`],
                  [t("显示或隐藏导航"), `${modifier}+B`],
                  [t("打开或关闭终端"), `${modifier}+J`],
                  [t("查看任务与请求"), `${modifier}+Shift+J`],
                  [t("打开设置"), `${modifier}+,`],
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
              {t("工作位置")}
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
              {t("打开工作台时继续上次对话")}
            </label>
            <p className="muted">
              {t(
                "各台设备分别记住新对话使用的项目，导航展开状态也会保留。恢复位置不会启动任务。",
              )}
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
          <HubConnectionPanel
            baseURL={baseURL}
            identity={identity}
            locallyHosted={!!onLocalModelRouting}
            onChangeHub={onChangeHub}
          />
          <section className="network-section">
            <h3>
              <Globe2 size={16} />
              {t(" Hub 的输入入口")}
            </h3>
            <WebWorkbenchButton baseURL={baseURL} />
            <details className="connection-advanced">
              <summary>{t("登录密钥")}</summary>
              <p className="muted">
                {t(
                  "登录密钥拥有当前输入端的访问权限，可用于连接此 Hub。管理员可在访问权限中撤销对应身份。",
                )}
              </p>
              <CopyButton text={loginKey} label={t("复制登录密钥")} />
            </details>
            <button className="settings-disconnect" onClick={onDisconnect}>
              <LogOut size={15} />
              {t("退出 MewDex")}
            </button>
            <p className="muted">{t("退出会清除当前网络的本机草稿。")}</p>
          </section>
          {onLocalDevice && (
            <section className="network-section">
              <h3>
                <Monitor size={16} />
                {t(" 此设备的服务")}
              </h3>
              <p className="muted">
                {t("管理此设备的执行能力、后台服务、自启动和本机数据。")}
              </p>
              <button
                className="settings-link"
                onClick={() => {
                  onOpenChange(false);
                  onLocalDevice();
                }}
              >
                {t("查看连接与本机服务")}
              </button>
            </section>
          )}
        </>
      )}
      {section === "models" && (
        <HubGPTPanel
          client={client}
          baseURL={baseURL}
          devices={devices}
          canManage={identity.role === "admin"}
          onNetworkSettings={
            onLocalModelRouting
              ? () => {
                  onOpenChange(false);
                  onLocalModelRouting();
                }
              : undefined
          }
        />
      )}
      {section === "drafts" && <DraftSettings {...draftSettings} />}
      {section === "appearance" && <AppearanceSettings />}
      {identity.role === "admin" && (
        <>
          {section === "pairing" && (
            <section className="network-section">
              <h3>
                <KeyRound size={15} />
                {t(" 添加输入端或执行端")}
              </h3>
              <label>
                {t("设备角色")}
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
                  <option value="client">
                    {t("输入端：手机、桌面或网页")}
                  </option>
                  <option value="executor">{t("执行端：电脑或服务器")}</option>
                  <option value="admin">
                    {t("管理员输入端：管理设备、授权与模型登录")}
                  </option>
                </select>
              </label>
              {role === "admin" ? (
                <p className="muted">
                  {t("管理员输入端可以管理整个设备网络。")}
                </p>
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
                {t("生成一次性配对码")}
              </button>
              {code && (
                <div className="pairing-result">
                  <code>{code}</code>
                  <button
                    className="icon-button"
                    aria-label={t("复制配对码")}
                    onClick={() => void navigator.clipboard.writeText(code)}
                  >
                    <Copy size={15} />
                  </button>
                  <small>{t("十分钟内有效，只能使用一次")}</small>
                </div>
              )}
            </section>
          )}
          {section === "access" && (
            <>
              <section className="network-section">
                <h3>
                  <ShieldCheck size={15} />
                  {t(" MewDex 登录与执行端身份")}
                </h3>
                {identities.isLoading && (
                  <p className="muted">{t("正在加载…")}</p>
                )}
                {identities.error && (
                  <div className="error">{identities.error.message}</div>
                )}
                {identities.data?.map((item) => (
                  <div className="identity-row" key={item.id}>
                    <div>
                      <strong>
                        {item.name}
                        {item.id === identity.id
                          ? t("（当前 MewDex 连接）")
                          : ""}
                      </strong>
                      <small>
                        {item.accountId
                          ? t("MewDex 账号")
                          : item.role === "executor"
                            ? t("执行端")
                            : item.role === "admin"
                              ? t("管理员输入端")
                              : t("输入端")}
                      </small>
                    </div>
                    {item.role !== "admin" && (
                      <button
                        onClick={() => {
                          setEditing(item);
                          setEditedAccess(item.access);
                        }}
                      >
                        {t("访问范围")}
                      </button>
                    )}
                    <button
                      className="icon-button"
                      aria-label={t("撤销 {0}", item.name)}
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
                  <h3>{t("{0} · 访问范围", editing.name)}</h3>
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
                    {t("保存访问范围")}
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
