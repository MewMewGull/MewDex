import { t } from "../localization.ts";
import { useEffect, useMemo, useRef, useState } from "react";
import { MewDexClient } from "@mewdex/sdk";
import type { Request } from "./localProtocol";
import {
  ArchiveRestore,
  Circle,
  Monitor,
  Power,
  RefreshCw,
  Route,
  ShieldCheck,
  Wrench,
  Globe2,
  Sparkles,
} from "lucide-react";
import type { Connection } from "../platform";
import { useLocalDevice, type LocalDeviceController } from "./useLocalDevice";
import { SettingsFrame } from "../components/SettingsFrame";
import { LocalSetupPanel } from "./LocalSetupPanel";
import { DeviceDataPanel } from "./DeviceDataPanel";
import { StartupPanel } from "./StartupPanel";
import { HubListenerPanel } from "./HubListenerPanel";
import { HubProxyPanel } from "./HubProxyPanel";
import { CleanupPanel } from "./CleanupPanel";
import { LocalInstallationPanel } from "./LocalInstallationPanel";
import { LocalServiceActions } from "./LocalServiceActions";
import { ModelRoutingPanel } from "./ModelRoutingPanel";
import { HubConnectionPanel, HubGPTPanel } from "./HubConnectionPanel";
import { isLocalHub } from "./hubConnection";
import { localServiceStates as states } from "../localDevice";

type Section =
  | "overview"
  | "models"
  | "startup"
  | "routing"
  | "data"
  | "maintenance";

interface LocalDeviceDialogProps {
  connection?: Connection;
  hubDeviceId?: string;
  connectionError?: string;
  onChangeHub?: () => void;
  initialSection?: "overview" | "routing";
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConnect: (connection: Connection) => Promise<void>;
  onJoinNetwork?: (url: string) => void;
}

export function LocalDeviceDialog(props: LocalDeviceDialogProps) {
  const [installation, setInstallation] = useState(0);
  return (
    <LocalDeviceContents
      key={installation}
      {...props}
      onInstallationChanged={() => setInstallation((value) => value + 1)}
    />
  );
}

function LocalDeviceContents({
  initialSection = "overview",
  open,
  onOpenChange,
  onConnect,
  onJoinNetwork,
  onInstallationChanged,
  connection,
  hubDeviceId,
  connectionError,
  onChangeHub,
}: LocalDeviceDialogProps & { onInstallationChanged: () => void }) {
  const device = useLocalDevice(
    open,
    onConnect,
    () => onOpenChange(false),
    onJoinNetwork,
    connection?.baseURL,
  );
  const client = useMemo(
    () =>
      connection
        ? new MewDexClient({
            baseURL: connection.baseURL,
            token: connection.credentials.token,
          })
        : null,
    [connection],
  );
  const [section, setSection] = useState<Section>("overview");
  const [portPending, setPortPending] = useState(false);
  const [proxyPending, setProxyPending] = useState(false);
  const [routingPending, setRoutingPending] = useState(false);
  const pendingSettings = portPending || proxyPending || routingPending;
  const settingsQueue = useRef<Promise<unknown>>(Promise.resolve());
  function saveSetting(request: Request) {
    const result = settingsQueue.current.then(() => device.apply(request));
    settingsQueue.current = result.catch(() => {});
    return result;
  }
  const { snapshot, busy } = device;
  const actionsDevice = { ...device, busy: busy || pendingSettings };
  const configured = !!snapshot?.configured;
  const stopped = snapshot?.state === "stopped";
  const locallyHosted =
    !!connection && isLocalHub(snapshot, connection.baseURL, hubDeviceId);
  const remoteHub = !!connection && !locallyHosted;
  const showHosting = !!snapshot?.hub && !remoteHub;
  useEffect(() => {
    if (open) setSection(initialSection);
  }, [open, initialSection]);
  useEffect(() => {
    if (snapshot && (!configured || remoteHub) && section === "routing")
      setSection("overview");
  }, [snapshot, configured, section, remoteHub]);
  return (
    <SettingsFrame<Section>
      open={open}
      onOpenChange={(value) => {
        if (!device.working && !pendingSettings) onOpenChange(value);
      }}
      title={connection ? t("Hub 连接与服务") : t("本机服务")}
      description={
        connection
          ? t("查看当前 Hub 的连接、共用 GPT 账号与本机服务状态。")
          : t("配置这台设备的执行能力、Hub 托管与本机数据。")
      }
      className="local-device-dialog"
      sizeKey="local-device"
      closeDisabled={device.working || pendingSettings}
      closeLabel={t("关闭连接与服务设置")}
      selected={section}
      onSelect={setSection}
      sections={[
        {
          id: "overview",
          label: connection
            ? t("Hub 连接")
            : configured
              ? t("概览")
              : t("开始使用"),
          icon: <Globe2 size={16} />,
          group: connection ? t("当前 Hub") : undefined,
        },
        ...(connection
          ? [
              {
                id: "models" as const,
                label: t("GPT 连接"),
                icon: <Sparkles size={16} />,
                group: t("当前 Hub"),
              },
            ]
          : []),
        ...(remoteHub && !configured
          ? [
              {
                id: "maintenance" as const,
                label: t("本机执行"),
                icon: <Monitor size={16} />,
                group: t("此设备"),
              },
            ]
          : []),
        ...(configured
          ? [
              {
                id: "startup" as const,
                label: t("本机启动"),
                icon: <Power size={16} />,
                group: t("此设备"),
              },
              ...(showHosting
                ? [
                    {
                      id: "routing" as const,
                      label: t("Hub 出站网络"),
                      icon: <Route size={16} />,
                      group: t("此设备"),
                    },
                  ]
                : []),
              {
                id: "maintenance" as const,
                label: t("本机服务与维护"),
                icon: <Wrench size={16} />,
                group: t("此设备"),
              },
            ]
          : []),
        {
          id: "data",
          label: configured ? t("本机数据与更新") : t("已有数据"),
          icon: <ArchiveRestore size={16} />,
          group: t("此设备"),
        },
      ]}
    >
      {device.error && (
        <div className="error" role="alert">
          {device.error}
          <button
            className="icon-button"
            aria-label={t("重新读取本机状态")}
            disabled={busy}
            onClick={() => void device.work(device.refresh)}
          >
            <RefreshCw size={14} />
          </button>
        </div>
      )}
      {connectionError && (
        <p className="error" role="alert">
          {connectionError}
        </p>
      )}
      {device.dataBusy && section !== "data" && (
        <button
          className="local-operation-link"
          onClick={() => setSection("data")}
        >
          {t("本机数据操作 · 查看进度")}
        </button>
      )}
      <div hidden={section !== "overview"}>
        {connection && (
          <HubConnectionPanel
            baseURL={connection.baseURL}
            identity={connection.credentials.identity}
            locallyHosted={locallyHosted}
            onChangeHub={onChangeHub}
          />
        )}
        {connection && snapshot && (
          <section className="network-section local-connection-service">
            <h3>
              <Monitor size={16} />
              {t(" 此设备的后台服务")}
            </h3>
            <p className="local-service-summary">
              {snapshot.hub
                ? t("本机 Hub · {0}", states[snapshot.state] ?? snapshot.state)
                : snapshot.executor
                  ? t(
                      "本机执行端 · {0}",
                      states[snapshot.state] ?? snapshot.state,
                    )
                  : t("本机执行端尚未配置")}
            </p>
            <p className="muted">
              {snapshot.hub && remoteHub
                ? t("使用远端 Hub 时，本机 Hub 的运行与自动启动会关闭。")
                : snapshot.hub
                  ? t("此 Hub 在这台设备上管理项目、模型连接与执行调度。")
                  : snapshot.executor
                    ? t("执行端负责接收 Hub 指令，在这台设备上运行任务。")
                    : t("可将这台设备接入当前 Hub，作为项目执行端。")}
            </p>
            {!snapshot.hub && snapshot.executor && snapshot.hubUrl && (
              <p className="muted">
                {t("执行端所属 Hub ")}
                <code>{snapshot.hubUrl}</code>
              </p>
            )}
            {snapshot.hub ? (
              <>
                {locallyHosted && (
                  <LocalServiceActions device={actionsDevice} connectedToHub />
                )}
                <button
                  className="settings-link"
                  onClick={() => setSection("maintenance")}
                >
                  {t("查看本机服务详情")}
                </button>
              </>
            ) : configured ? (
              <LocalServiceActions
                device={actionsDevice}
                onJoinNetwork={onJoinNetwork}
              />
            ) : (
              <button
                className="settings-link"
                onClick={() => setSection("maintenance")}
              >
                {t("配置本机执行端")}
              </button>
            )}
          </section>
        )}
        {!connection && (
          <>
            <div className="local-device-status" role="status">
              <Monitor size={19} />
              <strong>{snapshot?.name ?? t("这台电脑")}</strong>
              <span>
                {device.phase ||
                  (device.reading
                    ? t("正在读取…")
                    : snapshot
                      ? (states[snapshot.state] ?? snapshot.state)
                      : t("状态未获取"))}
              </span>
              <button
                className="icon-button"
                aria-label={t("刷新本机服务状态")}
                disabled={busy}
                onClick={() => void device.work(device.refresh)}
              >
                <RefreshCw size={15} />
              </button>
            </div>
            {!configured &&
              snapshot &&
              ["unconfigured", "setup-pending"].includes(snapshot.state) && (
                <>
                  <LocalSetupPanel device={device} />
                  {!snapshot.setupPending && (
                    <button
                      className="settings-link"
                      disabled={busy}
                      onClick={() => setSection("data")}
                    >
                      {t("使用已有设备配置或备份")}
                    </button>
                  )}
                </>
              )}
            {configured && snapshot && (
              <>
                <div className="local-service-overview">
                  <span
                    className={`local-service-indicator ${snapshot.ready ? "ready" : ""}`}
                  >
                    <Power size={28} />
                  </span>
                  <h3>
                    {snapshot.ready
                      ? t("本机服务运行中")
                      : stopped
                        ? t("准备开始工作")
                        : (states[snapshot.state] ?? snapshot.state)}
                  </h3>
                  <p>
                    {snapshot.ready
                      ? t("项目与对话统一在 Hub 工作台中管理。")
                      : stopped
                        ? t("启动服务后即可继续本机任务。")
                        : t("查看当前状态后继续操作。")}
                  </p>
                </div>
                <LocalServiceActions
                  device={actionsDevice}
                  onJoinNetwork={onJoinNetwork}
                />
                {snapshot.pairingRequired && (
                  <div className="local-attention">
                    <ShieldCheck size={17} />
                    <span>{t("需要重新连接本机执行的网络授权。")}</span>
                    <button onClick={() => setSection("maintenance")}>
                      {t("处理连接")}
                    </button>
                  </div>
                )}
                {(snapshot.state === "unknown" ||
                  snapshot.state === "unreadable") && (
                  <div className="local-attention">
                    <Wrench size={17} />
                    <span>{t("需要确认上次服务状态，才能继续。")}</span>
                    <button onClick={() => setSection("maintenance")}>
                      {t("查看维护选项")}
                    </button>
                  </div>
                )}
                <div className="local-overview-facts">
                  <div>
                    <Circle size={7} fill="currentColor" />
                    <span>
                      {snapshot.executor
                        ? t("已启用本机任务执行")
                        : t("仅运行设备网络")}
                    </span>
                  </div>
                  <div>
                    <span>{t("关闭工作台后，后台服务继续运行。")}</span>
                  </div>
                </div>
              </>
            )}
          </>
        )}
      </div>
      {section === "models" && connection && client && (
        <HubGPTPanel
          client={client}
          baseURL={connection.baseURL}
          canManage={connection.credentials.identity.role === "admin"}
          onNetworkSettings={
            showHosting ? () => setSection("routing") : undefined
          }
        />
      )}
      <div hidden={section !== "startup"}>
        <HubListenerPanel
          snapshot={showHosting ? snapshot : null}
          busy={busy}
          onPendingChange={setPortPending}
          onChange={(port) =>
            saveSetting({
              operation: "service.port",
              expectedDeviceId: snapshot?.deviceId,
              listener: { port },
            })
          }
        />
        <StartupPanel
          snapshot={snapshot}
          busy={busy || pendingSettings}
          hostingDisabled={remoteHub && !!snapshot?.hub}
          revision={device.revision}
          onWork={device.work}
          onChange={(enabled) =>
            device.apply({
              operation: "startup.apply",
              expectedDeviceId: snapshot?.deviceId,
              startup: { enabled },
            })
          }
        />
        <HubProxyPanel
          active={open && section === "startup"}
          snapshot={showHosting ? snapshot : null}
          busy={busy}
          revision={device.revision}
          onWork={device.work}
          onPendingChange={setProxyPending}
          importDisabled={pendingSettings}
          onChange={(request) =>
            saveSetting({ ...request, expectedDeviceId: snapshot?.deviceId })
          }
        />
      </div>
      <div hidden={section !== "routing"}>
        {configured && snapshot && (
          <>
            <LocalServiceActions
              device={actionsDevice}
              onJoinNetwork={onJoinNetwork}
            />
            <ModelRoutingPanel
              key={snapshot.deviceId}
              snapshot={snapshot}
              busy={busy || portPending || proxyPending}
              onPendingChange={setRoutingPending}
              onChange={(modelRouting) =>
                saveSetting({
                  operation: "model-routing.apply",
                  expectedDeviceId: snapshot.deviceId,
                  modelRouting,
                })
              }
            />
          </>
        )}
      </div>
      <div hidden={section !== "maintenance"}>
        {remoteHub && !configured && snapshot && (
          <LocalSetupPanel device={device} />
        )}
        {configured && snapshot && (
          <>
            {remoteHub && snapshot.hub && (
              <section className="network-section">
                <h3>{t("本机 Hub 托管")}</h3>
                <p className="muted">
                  {t(
                    "当前使用 {0}。切换后可在这台设备上托管独立的 Hub。",
                    connection?.baseURL,
                  )}
                </p>
                <button
                  className="settings-link"
                  disabled={busy || (!stopped && !snapshot.ready)}
                  onClick={() => void device.enter()}
                >
                  {t("切换到本机 Hub")}
                </button>
                {snapshot.ready && (
                  <button
                    className="settings-link"
                    disabled={busy}
                    onClick={() => void device.stop()}
                  >
                    {t("停止本机 Hub")}
                  </button>
                )}
              </section>
            )}
            {!snapshot.hub && snapshot.executor && (
              <ExecutorPairing
                device={actionsDevice}
                onJoinNetwork={onJoinNetwork}
              />
            )}
            <CleanupPanel
              snapshot={snapshot}
              busy={busy || pendingSettings}
              revision={device.revision}
              onWork={device.work}
              onSnapshot={device.receive}
            />
            <details className="local-technical">
              <summary>{t("本机服务详情")}</summary>
              <dl className="connection-details">
                <div>
                  <dt>{t("设备编号")}</dt>
                  <dd>{snapshot.deviceId}</dd>
                </div>
                <div>
                  <dt>{t("服务能力")}</dt>
                  <dd>
                    {[
                      snapshot.hub && "MewDex Hub",
                      snapshot.executor && t("任务执行"),
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </dd>
                </div>
                <div>
                  <dt>
                    {snapshot.hub
                      ? t("本机 Hub 监听地址")
                      : t("执行端连接的 Hub")}
                  </dt>
                  <dd>
                    {snapshot.hub
                      ? snapshot.baseUrl || snapshot.listen
                      : snapshot.hubUrl}
                  </dd>
                </div>
              </dl>
              {snapshot.message && <p className="muted">{snapshot.message}</p>}
            </details>
          </>
        )}
      </div>
      <div hidden={section !== "data"}>
        <LocalInstallationPanel
          busy={busy || pendingSettings}
          onWork={device.work}
          onChanged={onInstallationChanged}
        />
        <DeviceDataPanel
          snapshot={snapshot}
          busy={device.working || pendingSettings}
          onSnapshot={device.receive}
          onActivity={device.setDataBusy}
          onRestored={device.restored}
          onUpgraded={device.upgraded}
          onOpenService={() =>
            setSection(snapshot?.pairingRequired ? "maintenance" : "overview")
          }
        />
      </div>
    </SettingsFrame>
  );
}

function ExecutorPairing({
  device,
  onJoinNetwork,
}: {
  device: LocalDeviceController;
  onJoinNetwork?: (url: string) => void;
}) {
  const { snapshot, setup, busy } = device;
  const stopped = snapshot?.state === "stopped";
  const locked = busy;
  return (
    <section className="device-data executor-repair">
      <h3>{t("本机执行端连接的 MewDex 网络")}</h3>
      {snapshot?.hubUrl && (
        <p className="local-pairing-network">{snapshot.hubUrl}</p>
      )}
      <p className="muted">
        {t(
          "此连接用于接收 Hub 派发的任务并同步执行结果。服务停止后，可继续已有配对或使用新的执行端配对码。",
        )}
      </p>
      <button
        disabled={locked || !stopped}
        onClick={() => void device.repair("")}
      >
        {t("继续已有配对")}
      </button>
      {device.pairingMessage && <p role="status">{device.pairingMessage}</p>}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void device.repair(setup.invitation?.trim() ?? "");
        }}
      >
        <label>
          {t("新的执行端配对码")}
          <input
            value={setup.invitation ?? ""}
            disabled={locked || !stopped}
            autoComplete="off"
            onChange={(event) =>
              device.updateSetup({ invitation: event.target.value })
            }
            required
          />
        </label>
        <button disabled={locked || !stopped || !setup.invitation?.trim()}>
          {t("重新配对本机执行")}
        </button>
      </form>
      {snapshot?.pairingRequired ? (
        <p className="muted">
          {t(
            "从网络管理员取得执行端配对码。完成连接后，可在这里继续启动服务。",
          )}
        </p>
      ) : (
        <div className="local-pairing-next">
          <h4>
            {snapshot?.ready ? t("本机服务已就绪") : t("继续启动本机服务")}
          </h4>
          <LocalServiceActions device={device} onJoinNetwork={onJoinNetwork} />
        </div>
      )}
    </section>
  );
}
