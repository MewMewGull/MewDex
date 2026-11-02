import type { LocalDeviceController } from "../useLocalDevice";

export function LocalServiceActions({
  device,
  onJoinNetwork,
}: {
  device: LocalDeviceController;
  onJoinNetwork?: (url: string) => void;
}) {
  const { snapshot, busy } = device;
  if (!snapshot) return null;
  const stopped = snapshot.state === "stopped";
  const locked = busy;
  return (
    <>
      <div className="local-device-actions overview-actions">
        {snapshot.hub ? (
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
                ? "保存本机输入连接"
                : device.existingHub
                  ? "连接已运行的 Hub"
                  : snapshot.ready
                    ? "进入本机工作台"
                    : "启动并进入工作台")}
          </button>
        ) : (
          <button
            className="primary"
            disabled={locked || !stopped || snapshot.pairingRequired}
            onClick={() => void device.start()}
          >
            {device.phase || "启动本机服务"}
          </button>
        )}
        {!stopped && (
          <button
            disabled={locked || !snapshot.instanceId}
            onClick={() => void device.stop()}
          >
            停止服务
          </button>
        )}
      </div>
      {device.existingHub && (
        <p className="muted" role="status">
          已检测到运行中的 Hub：{device.existingHub.baseUrl}。连接后使用该 Hub
          的账号或密钥登录。
        </p>
      )}
      {!snapshot.hub && snapshot.ready && onJoinNetwork && (
        <section className="local-input-next">
          <h4>打开输入工作台</h4>
          <p className="muted">
            本机执行已加入网络。使用输入端配对码，就能从这里发起对话；执行端配对码用于这台电脑的后台服务。
          </p>
          <button
            className="settings-link"
            onClick={() => onJoinNetwork(snapshot.hubUrl!)}
          >
            连接这个网络的工作台
          </button>
        </section>
      )}
    </>
  );
}
