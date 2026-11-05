import { t } from "../localization.ts";
import { Layers3, Monitor, Plus } from "lucide-react";
import type { Device } from "@mewdex/sdk";
import { devicePresence } from "../devicePresence";
import type { ExecutionTarget } from "../executionTarget";

export function ConversationWelcome({
  device,
  target,
  connected,
  loading,
  hasExecutors,
  canPair,
  onChoose,
  onPair,
  onLocalDevice,
  onCreateProject,
}: {
  device?: Device;
  target: ExecutionTarget;
  connected: boolean;
  loading: boolean;
  hasExecutors: boolean;
  canPair: boolean;
  onChoose: () => void;
  onPair: () => void;
  onLocalDevice?: () => void;
  onCreateProject?: () => void;
}) {
  const presence = devicePresence(device?.online, connected);
  const missingExecutor = !device && !hasExecutors;
  return (
    <div className="welcome">
      <div className="welcome-eyebrow">
        <Layers3 size={18} />
        <span>{target.project?.name ?? t("MewDex 工作台")}</span>
      </div>
      <h1>
        {loading
          ? t("正在读取设备…")
          : missingExecutor
            ? t("先接入一台执行设备")
            : target.issue
              ? t("选择可以继续工作的地方")
              : t("从这里开始下一项工作")}
      </h1>
      <p>
        {loading
          ? t("连接后即可选择设备和项目。")
          : missingExecutor
            ? canPair
              ? t("接入电脑或服务器后，就可以在这里发起对话。")
              : t("请联系网络管理员接入执行设备，并授予设备和项目的访问权限。")
            : target.issue
              ? target.message
              : t("在下方描述任务，随时查看执行进度和项目变更。")}
      </p>
      {!loading && !missingExecutor && (
        <button
          className="target-card"
          onClick={onChoose}
          aria-label={t("选择 Hub 项目")}
        >
          <span className="target-icon">
            <Monitor size={22} />
          </span>
          <div>
            <strong>{target.project?.name ?? t("选择 Hub 项目")}</strong>
            <small>{device?.name ?? t("选择执行设备")}</small>
            {target.project?.path && (
              <span className="target-path" title={target.project.path}>
                {target.project.path}
              </span>
            )}
            {!!target.project?.directories?.length && (
              <span className="target-path">
                {t(
                  "{0} 台机器 · {1} 个目录",
                  new Set(
                    target.project.directories.map(
                      (directory) => directory.deviceId,
                    ),
                  ).size,
                  target.project.directories.length,
                )}
              </span>
            )}
          </div>
          <span className="target-status">
            {target.issue ? (
              target.label
            ) : (
              <>
                <i className={presence.dot} />
                {presence.label}
              </>
            )}
          </span>
        </button>
      )}
      {!loading && target.issue && (
        <div className="welcome-actions">
          {missingExecutor && onLocalDevice && (
            <button className="primary" onClick={onLocalDevice}>
              <Monitor size={15} />
              {t(" 配置这台电脑")}
            </button>
          )}
          {target.issue === "project" && onCreateProject && (
            <button className="primary" onClick={onCreateProject}>
              <Plus size={15} />
              {t(" 创建项目")}
            </button>
          )}
          {canPair && target.issue !== "project" && (
            <button
              className={missingExecutor && !onLocalDevice ? "primary" : ""}
              onClick={onPair}
            >
              <Plus size={15} />
              {t(" 添加执行设备")}
            </button>
          )}
          {!missingExecutor && onLocalDevice && (
            <button onClick={onLocalDevice}>{t("管理本机设备")}</button>
          )}
        </div>
      )}
    </div>
  );
}
