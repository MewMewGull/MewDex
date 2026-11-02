import { useEffect, useRef, useState } from "react";
import type { Request } from "../localProtocol";
import {
  ArchiveRestore,
  Circle,
  Monitor,
  Power,
  RefreshCw,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import type { Connection } from "../platform";
import { useLocalDevice, type LocalDeviceController } from "../useLocalDevice";
import { SettingsFrame } from "./SettingsFrame";
import { LocalSetupPanel } from "./LocalSetupPanel";
import { DeviceDataPanel } from "./DeviceDataPanel";
import { StartupPanel } from "./StartupPanel";
import { HubListenerPanel } from "./HubListenerPanel";
import { HubProxyPanel } from "./HubProxyPanel";
import { CleanupPanel } from "./CleanupPanel";
import { LocalInstallationPanel } from "./LocalInstallationPanel";
import { LocalServiceActions } from "./LocalServiceActions";
import { localServiceStates as states } from "../localDevice";

type Section = "overview" | "startup" | "data" | "maintenance";

interface LocalDeviceDialogProps {
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
  open,
  onOpenChange,
  onConnect,
  onJoinNetwork,
  onInstallationChanged,
}: LocalDeviceDialogProps & { onInstallationChanged: () => void }) {
  const device = useLocalDevice(
    open,
    onConnect,
    () => onOpenChange(false),
    onJoinNetwork,
  );
  const [section, setSection] = useState<Section>("overview");
  const [portPending, setPortPending] = useState(false);
  const [proxyPending, setProxyPending] = useState(false);
  const pendingSettings = portPending || proxyPending;
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
  useEffect(() => {
    if (open) setSection("overview");
  }, [open]);
  return (
    <SettingsFrame<Section>
      open={open}
      onOpenChange={(value) => {
        if (!device.working && !pendingSettings) onOpenChange(value);
      }}
      title="本机设备"
      description="管理这台电脑的后台服务、启动与数据。"
      className="local-device-dialog"
      closeDisabled={device.working || pendingSettings}
      closeLabel="关闭本机设备设置"
      selected={section}
      onSelect={setSection}
      sections={[
        {
          id: "overview",
          label: configured ? "概览" : "开始使用",
          icon: <Monitor size={16} />,
        },
        ...(configured
          ? [
              {
                id: "startup" as const,
                label: "启动设置",
                icon: <Power size={16} />,
              },
              {
                id: "maintenance" as const,
                label: "连接与维护",
                icon: <Wrench size={16} />,
              },
            ]
          : []),
        {
          id: "data",
          label: configured ? "数据与更新" : "已有数据",
          icon: <ArchiveRestore size={16} />,
        },
      ]}
    >
      {device.error && (
        <div className="error" role="alert">
          {device.error}
          <button
            className="icon-button"
            aria-label="重新读取本机状态"
            disabled={busy}
            onClick={() => void device.work(device.refresh)}
          >
            <RefreshCw size={14} />
          </button>
        </div>
      )}
      {device.dataBusy && section !== "data" && (
        <button
          className="local-operation-link"
          onClick={() => setSection("data")}
        >
          本机数据操作 · 查看进度
        </button>
      )}
      <div hidden={section !== "overview"}>
        <div className="local-device-status" role="status">
          <Monitor size={19} />
          <strong>{snapshot?.name ?? "这台电脑"}</strong>
          <span>
            {device.phase ||
              (device.reading
                ? "正在读取…"
                : snapshot
                  ? (states[snapshot.state] ?? snapshot.state)
                  : "状态未获取")}
          </span>
          <button
            className="icon-button"
            aria-label="刷新本机服务状态"
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
                  使用已有设备配置或备份
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
                  ? "本机服务运行中"
                  : stopped
                    ? "准备开始工作"
                    : (states[snapshot.state] ?? snapshot.state)}
              </h3>
              <p>
                {snapshot.ready
                  ? "项目与对话统一在 Hub 工作台中管理。"
                  : stopped
                    ? "启动服务后即可继续本机任务。"
                    : "查看当前状态后继续操作。"}
              </p>
            </div>
            <LocalServiceActions
              device={actionsDevice}
              onJoinNetwork={onJoinNetwork}
            />
            {snapshot.pairingRequired && (
              <div className="local-attention">
                <ShieldCheck size={17} />
                <span>需要重新连接本机执行的网络授权。</span>
                <button onClick={() => setSection("maintenance")}>
                  处理连接
                </button>
              </div>
            )}
            {(snapshot.state === "unknown" ||
              snapshot.state === "unreadable") && (
              <div className="local-attention">
                <Wrench size={17} />
                <span>需要确认上次服务状态，才能继续。</span>
                <button onClick={() => setSection("maintenance")}>
                  查看维护选项
                </button>
              </div>
            )}
            <div className="local-overview-facts">
              <div>
                <Circle size={7} fill="currentColor" />
                <span>
                  {snapshot.executor ? "已启用本机任务执行" : "仅运行设备网络"}
                </span>
              </div>
              <div>
                <span>关闭工作台后，后台服务继续运行。</span>
              </div>
            </div>
          </>
        )}
      </div>
      <div hidden={section !== "startup"}>
        <HubListenerPanel
          snapshot={snapshot}
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
          snapshot={snapshot}
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
      <div hidden={section !== "maintenance"}>
        {configured && snapshot && (
          <>
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
              <summary>设备与连接详情</summary>
              <dl className="connection-details">
                <div>
                  <dt>设备编号</dt>
                  <dd>{snapshot.deviceId}</dd>
                </div>
                <div>
                  <dt>服务能力</dt>
                  <dd>
                    {[
                      snapshot.hub && "设备网络",
                      snapshot.executor && "任务执行",
                      snapshot.gateway && "模型请求转发",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </dd>
                </div>
                <div>
                  <dt>网络地址</dt>
                  <dd>
                    {snapshot.hub
                      ? snapshot.baseUrl || snapshot.listen
                      : snapshot.hubUrl}
                  </dd>
                </div>
              </dl>
              {snapshot.existingModelAccount && (
                <p className="muted">
                  使用已有模型环境，模型账号可在工作台设置中管理。
                </p>
              )}
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
      <h3>本机执行的网络连接</h3>
      {snapshot?.hubUrl && (
        <p className="local-pairing-network">{snapshot.hubUrl}</p>
      )}
      <p className="muted">
        服务停止后，可继续已有配对或使用新的执行端配对码。
      </p>
      <button
        disabled={locked || !stopped}
        onClick={() => void device.repair("")}
      >
        继续已有配对
      </button>
      {device.pairingMessage && <p role="status">{device.pairingMessage}</p>}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void device.repair(setup.invitation?.trim() ?? "");
        }}
      >
        <label>
          新的执行端配对码
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
          重新配对本机执行
        </button>
      </form>
      {snapshot?.pairingRequired ? (
        <p className="muted">
          从网络管理员取得执行端配对码。完成连接后，可在这里继续启动服务。
        </p>
      ) : (
        <div className="local-pairing-next">
          <h4>{snapshot?.ready ? "本机服务已就绪" : "继续启动本机服务"}</h4>
          <LocalServiceActions device={device} onJoinNetwork={onJoinNetwork} />
        </div>
      )}
    </section>
  );
}
