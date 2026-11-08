import type { Device, Project, ProjectDirectory } from "@mewdex/sdk";
import { devicePlatform, devicePresence } from "../devicePresence";
import { t } from "../localization.ts";
import { DeviceIcon } from "./DeviceIcon";

export function ExecutorStatus({
  devices,
  owner,
  project,
  connected,
  onComputer,
}: {
  devices: Device[];
  owner?: Device;
  project?: Project;
  connected: boolean;
  onComputer?: (deviceId: string) => void;
}) {
  const bindings = new Map<string, ProjectDirectory[]>();
  if (project?.directories) {
    for (const directory of project.directories) {
      const current = bindings.get(directory.deviceId) ?? [];
      current.push(directory);
      bindings.set(directory.deviceId, current);
    }
  } else if (project && owner && !owner.roles.includes("coordinator")) {
    bindings.set(owner.id, [
      {
        deviceId: owner.id,
        projectId: project.id,
        name: project.name,
        path: project.path,
      },
    ]);
  } else if (!project) {
    for (const device of devices) {
      if (
        device.roles.includes("executor") &&
        !device.roles.includes("coordinator")
      )
        bindings.set(device.id, []);
    }
  }
  const nodes = [...bindings].map(([id, directories]) => ({
    id,
    device: devices.find((device) => device.id === id),
    paths: [...new Set(directories.map((directory) => directory.path))],
  }));
  const online = nodes.filter((node) => node.device?.online).length;
  return (
    <section
      className="executor-status"
      aria-label={project ? t("{0} 的执行端", project.name) : t("执行端节点")}
    >
      <div className="executor-status-heading">
        <strong>{project ? t("项目执行端") : t("执行端节点")}</strong>
        {!!nodes.length && (
          <span role="status">
            {connected
              ? t("{0}/{1} 在线", online, nodes.length)
              : t("节点状态待同步")}
          </span>
        )}
      </div>
      {nodes.length ? (
        <ul aria-live="polite">
          {nodes.map(({ id, device, paths }) => {
            const presence = devicePresence(device?.online, connected);
            const label =
              device || !connected ? presence.label : t("未找到执行端");
            return (
              <li
                key={id}
                data-executor-id={id}
                data-online={connected && !!device?.online}
                title={[devicePlatform(device?.platform ?? ""), ...paths]
                  .filter(Boolean)
                  .join("\n")}
              >
                <div className="executor-status-node">
                  <DeviceIcon platform={device?.platform ?? ""} size={14} />
                  <span className="executor-status-name">
                    {device?.name ?? id}
                  </span>
                  <span className="executor-status-presence">
                    <i className={presence.dot} aria-hidden="true" />
                    {label}
                  </span>
                  {onComputer && (
                    <button
                      className="computer-use-entry"
                      onClick={() => onComputer(id)}
                    >
                      {t("窗口控制")}
                    </button>
                  )}
                </div>
                {!!paths.length && (
                  <small className="executor-status-path">
                    {paths.join(" · ")}
                  </small>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="muted">
          {project ? t("当前项目未绑定执行端") : t("暂无执行端节点")}
        </p>
      )}
    </section>
  );
}
