import { t } from "../localization.ts";
import type { LocalDeviceController } from "./useLocalDevice";
import { WebWorkbenchButton } from "./WebWorkbenchButton";

export function LocalServiceActions({
  device,
  onJoinNetwork,
  connectedToHub = false,
}: {
  device: LocalDeviceController;
  onJoinNetwork?: (url: string) => void;
  connectedToHub?: boolean;
}) {
  const { snapshot, busy } = device;
  if (!snapshot) return null;
  const stopped = snapshot.state === "stopped";
  const locked = busy;
  return (
    <>
      <div className="local-device-actions overview-actions">
        {snapshot.hub ? (
          connectedToHub ? null : (
            <button
              className="primary"
              disabled={
                locked ||
                snapshot.pairingRequired ||
                (!snapshot.ready && !stopped && !device.existingHub)
              }
              onClick={() => void device.enter()}
            >
              {device.phase ||
                (device.connectionPending
                  ? t("保存本机输入连接")
                  : device.existingHub
                    ? t("连接已运行的 Hub")
                    : snapshot.ready
                      ? t("使用此 Hub")
                      : t("启动此 Hub 并连接"))}
            </button>
          )
        ) : (
          <button
            className="primary"
            disabled={locked || !stopped || snapshot.pairingRequired}
            onClick={() => void device.start()}
          >
            {device.phase || t("启动本机服务")}
          </button>
        )}
        {!stopped && (
          <button
            disabled={locked || !snapshot.instanceId}
            onClick={() => void device.stop()}
          >
            {t("停止服务")}
          </button>
        )}
      </div>
      {snapshot.hub && snapshot.ready && snapshot.baseUrl && (
        <WebWorkbenchButton baseURL={snapshot.baseUrl} />
      )}
      {device.existingHub && (
        <p className="muted" role="status">
          {t(
            "已检测到运行中的 Hub：{0}。连接后使用该 Hub 的账号或密钥登录。",
            device.existingHub.baseUrl,
          )}
        </p>
      )}
      {!snapshot.hub && snapshot.ready && onJoinNetwork && (
        <section className="local-input-next">
          <h4>{t("打开输入工作台")}</h4>
          <p className="muted">
            {t(
              "本机执行端已加入 MewDex 网络。使用 MewDex 账号、登录密钥或输入端配对码登录工作台后，即可发起对话。GPT 账号和 OpenAI 连接由此 Hub 管理。",
            )}
          </p>
          <button
            className="settings-link"
            onClick={() => onJoinNetwork(snapshot.hubUrl!)}
          >
            {t("连接此 MewDex 网络的工作台")}
          </button>
        </section>
      )}
    </>
  );
}
