import type { Device } from "@mewdex/sdk";

export function executionTarget(device: Device | undefined, projectId: string) {
  const project = device?.projects.find((item) => item.id === projectId);
  const executor = !!device?.roles.includes("executor");
  const issue = !device
    ? ("device" as const)
    : !executor
      ? ("execution" as const)
      : !project
        ? ("project" as const)
        : null;
  const label =
    issue === "device"
      ? "待选择设备"
      : issue === "execution"
        ? "执行能力未开启"
        : issue === "project"
          ? projectId
            ? "项目不可用"
            : "工作目录未就绪"
          : "";
  const message =
    issue === "device"
      ? "请先选择可以执行任务的设备。"
      : issue === "execution"
        ? "此设备当前没有开启执行能力，历史记录仍可查阅。"
        : issue === "project"
          ? projectId
            ? "原项目已移除或不在当前访问范围内，原输入保留供复制。"
            : "默认工作区尚未就绪，请确认执行端已更新并启动。已有项目不可见时，请确认项目访问权限。"
          : "";
  return {
    project,
    issue,
    label,
    message,
    canEdit: !!project,
    canExecute: executor && !!project,
  };
}

export type ExecutionTarget = ReturnType<typeof executionTarget>;
