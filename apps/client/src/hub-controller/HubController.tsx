import { t } from "../localization.ts";
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bell,
  Copy,
  KeyRound,
  Keyboard,
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
  Brain,
  Puzzle,
  Workflow,
  BookOpen,
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
import { MewDexAccountSettings } from "./MewDexAccountSettings";
import { MessagingSettings } from "./MessagingSettings";
import { ExtensionSettings, type ExtensionSection } from "./ExtensionSettings";
import { WebWorkbenchButton } from "./WebWorkbenchButton";
import { AppUpdateSettings } from "../components/AppUpdates";
import { LanguageSettings } from "../components/LanguageSettings";
import { devicePlatform } from "../devicePresence";

type Section =
  | "account"
  | "general"
  | "connection"
  | "appearance"
  | "drafts"
  | "updates"
  | "pairing"
  | "input-pairing"
  | "models"
  | "messaging"
  | ExtensionSection
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
  initialSection,
  initialProjectId,
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
  initialSection?: Section;
  initialProjectId?: string;
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
  const [role, setRole] = useState<"client" | "admin">("client");
  useEffect(() => {
    if (!open) return;
    if (initialExecutorSetup && identity.role === "admin") {
      setSection("pairing");
      setCode(null);
      setError("");
      return;
    }
    setSection(initialModelDeviceId ? "models" : (initialSection ?? "general"));
  }, [
    open,
    initialModelDeviceId,
    initialExecutorSetup,
    initialSection,
    identity.role,
  ]);
  const [access, setAccess] = useState<Access>(initialAccess);
  const [code, setCode] = useState<{ value: string; context: string } | null>(
    null,
  );
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<Identity | null>(null);
  const [editedAccess, setEditedAccess] = useState<Access>(initialAccess);
  const executorPairing = section === "pairing";
  const pairingRole = executorPairing ? "executor" : role;
  const pairingAccess = executorPairing
    ? { allDevices: false, targets: [], gateway: false }
    : access;
  const pairingContext = JSON.stringify([pairingRole, pairingAccess]);
  const executors = devices.filter(
    (device) =>
      device.roles.includes("executor") &&
      !device.roles.includes("coordinator"),
  );

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
      description={t("输入端负责交互与显示，Hub 统一连接 GPT 并管理执行端。")}
      className="terminal-settings"
      selected={section}
      onSelect={(value) => {
        setSection(value);
        setCode(null);
        setError("");
      }}
      sections={[
        {
          id: "general",
          label: t("通用"),
          icon: <Bell size={16} />,
          group: t("输入端设置"),
        },
        {
          id: "appearance",
          label: t("外观"),
          icon: <Palette size={16} />,
          group: t("输入端设置"),
        },
        {
          id: "drafts",
          label: t("草稿"),
          icon: <FilePenLine size={16} />,
          group: t("输入端设置"),
        },
        {
          id: "account",
          label: t("MewDex 账号"),
          icon: <KeyRound size={16} />,
          group: t("输入端设置"),
        },
        ...(identity.role === "admin"
          ? [
              {
                id: "input-pairing" as const,
                label: t("输入端配对"),
                icon: <KeyRound size={16} />,
                group: t("输入端设置"),
              },
              {
                id: "messaging" as const,
                label: t("消息渠道"),
                icon: <MessageCircle size={16} />,
                group: t("输入端设置"),
              },
            ]
          : []),
        {
          id: "updates",
          label: t("应用更新"),
          icon: <Download size={16} />,
          group: t("输入端设置"),
        },
        {
          id: "connection",
          label: t("Hub 连接"),
          icon: <Globe2 size={16} />,
          group: t("Hub 与执行端设置"),
        },
        {
          id: "models",
          label: t("GPT 账号与网络"),
          icon: <Sparkles size={16} />,
          group: t("Hub 与执行端设置"),
        },
        {
          id: "skills",
          label: "Skills",
          icon: <BookOpen size={16} />,
          group: t("Hub 与执行端设置"),
        },
        {
          id: "memory",
          label: "Memory",
          icon: <Brain size={16} />,
          group: t("Hub 与执行端设置"),
        },
        {
          id: "hooks",
          label: "Hooks",
          icon: <Workflow size={16} />,
          group: t("Hub 与执行端设置"),
        },
        {
          id: "plugins",
          label: "Plugins",
          icon: <Puzzle size={16} />,
          group: t("Hub 与执行端设置"),
        },
        ...(identity.role === "admin"
          ? [
              {
                id: "pairing" as const,
                label: t("执行端"),
                icon: <Monitor size={16} />,
                group: t("Hub 与执行端设置"),
              },
              {
                id: "access" as const,
                label: t("访问权限"),
                icon: <ShieldCheck size={16} />,
                group: t("Hub 与执行端设置"),
              },
            ]
          : []),
      ]}
    >
      {section === "messaging" && identity.role === "admin" && (
        <MessagingSettings client={client} devices={devices} />
      )}
      {(["skills", "memory", "hooks", "plugins"] as string[]).includes(
        section,
      ) && (
        <ExtensionSettings
          key={section}
          client={client}
          devices={devices}
          section={section as ExtensionSection}
          admin={identity.role === "admin"}
          initialProjectId={initialProjectId}
        />
      )}
      {section === "updates" && <AppUpdateSettings />}
      {section === "account" && (
        <>
          <MewDexAccountSettings
            client={client}
            supported={accountSupported}
            baseURL={baseURL}
            loginKey={loginKey}
            onLogout={onDisconnect}
          />
          <section className="network-section">
            <h3>{t(" Hub 的输入入口")}</h3>
            <WebWorkbenchButton baseURL={baseURL} />
          </section>
        </>
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
        <HubConnectionPanel
          baseURL={baseURL}
          identity={identity}
          locallyHosted={!!onLocalModelRouting}
          onChangeHub={onChangeHub}
        />
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
                <Monitor size={16} />
                {t("执行端")}
              </h3>
              <p className="muted">
                {t(
                  "其他电脑或服务器只需安装执行端，在执行端面板填写当前 Hub 地址和一次性配对码，再配置本机目录。",
                )}
              </p>
              <p className="muted">
                {t("GPT 账号和 OpenAI 网络连接统一在 Hub 上配置。")}
              </p>
              {executors.map((device) => (
                <div className="identity-row" key={device.id}>
                  <Monitor size={16} />
                  <div>
                    <strong>{device.name}</strong>
                    <small>
                      {[
                        device.online ? t("在线") : t("离线"),
                        devicePlatform(device.platform),
                        t("{0} 个目录绑定", device.projects.length),
                      ].join(" · ")}
                    </small>
                  </div>
                </div>
              ))}
              {!executors.length && (
                <p className="muted">{t("当前没有已配对的执行端。")}</p>
              )}
              {onLocalDevice && (
                <button
                  className="settings-link"
                  onClick={() => {
                    onOpenChange(false);
                    onLocalDevice();
                  }}
                >
                  {t("查看连接与本机服务")}
                </button>
              )}
            </section>
          )}
          {(section === "pairing" || section === "input-pairing") && (
            <section className="network-section">
              <h3>
                <KeyRound size={15} />
                {executorPairing ? t("添加执行端") : t("添加输入端")}
              </h3>
              {!executorPairing && (
                <>
                  <p className="muted">
                    {t(
                      "手机、桌面和网页输入端可使用同一 MewDex 账号登录。配对码用于连接其他输入端。",
                    )}
                  </p>
                  <label>
                    {t("设备角色")}
                    <select
                      value={role}
                      onChange={(e) => {
                        const role = e.target.value as "client" | "admin";
                        setRole(role);
                        setAccess({
                          allDevices: role === "client",
                          targets: [],
                          gateway: false,
                        });
                        setCode(null);
                      }}
                    >
                      <option value="client">
                        {t("输入端：手机、桌面或网页")}
                      </option>
                      <option value="admin">
                        {t("管理员输入端：管理设备、授权与模型登录")}
                      </option>
                    </select>
                  </label>
                </>
              )}
              {!executorPairing && role === "admin" ? (
                <p className="muted">
                  {t("管理员输入端可以管理整个设备网络。")}
                </p>
              ) : (
                <AccessEditor
                  value={access}
                  onChange={setAccess}
                  devices={devices}
                  executor={executorPairing}
                />
              )}
              <button
                className="primary"
                disabled={busy}
                onClick={() =>
                  void action(async () => {
                    const result = await client.invite(
                      pairingRole,
                      pairingAccess,
                    );
                    setCode({ value: result.code, context: pairingContext });
                  })
                }
              >
                {t("生成一次性配对码")}
              </button>
              {code?.context === pairingContext && (
                <div className="pairing-result">
                  <code>{code.value}</code>
                  <button
                    className="icon-button"
                    aria-label={t("复制配对码")}
                    onClick={() =>
                      void navigator.clipboard.writeText(code.value)
                    }
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
