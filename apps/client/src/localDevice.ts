// Native device management is separate from the network SDK used by every input.
import { invoke, isTauri } from "@tauri-apps/api/core";
import type { Request, Result, Snapshot } from "./localProtocol";

export interface InputCapabilities {
  localService: boolean;
  helperPresent: boolean;
  enginePresent: boolean;
  notifications?: boolean;
  notificationActions?: boolean;
  desktopNotifications?: boolean;
  desktopPreferences?: boolean;
  attachmentSave?: boolean;
  projectDirectory?: boolean;
}

const inputOnly: InputCapabilities = {
  localService: false,
  helperPresent: false,
  enginePresent: false,
};

export const localServiceStates: Record<string, string> = {
  unconfigured: "尚未配置",
  "setup-pending": "配置尚未完成",
  stopped: "已停止",
  starting: "正在启动",
  running: "运行中",
  stopping: "正在停止",
  unknown: "状态待核实",
  unreadable: "配置无法读取",
};
let capabilities: Promise<InputCapabilities> | undefined;

export function inputCapabilities(): Promise<InputCapabilities> {
  return (capabilities ??= isTauri()
    ? invoke<InputCapabilities>("input_capabilities")
    : Promise.resolve(inputOnly));
}

export async function deviceRequest(request: Request): Promise<Result> {
  const support = await inputCapabilities();
  if (!support.localService || !support.helperPresent)
    throw new Error("当前输入端没有可用的本机设备服务");
  try {
    return await invoke<Result>("device_request", { request });
  } catch (error) {
    throw new Error(typeof error === "string" ? error : "本机设备操作失败");
  }
}

export async function chooseProject(): Promise<string | null> {
  if (!(await inputCapabilities()).localService)
    throw new Error("当前输入端不提供本机项目选择");
  try {
    return await invoke<string | null>("choose_project");
  } catch (error) {
    throw new Error(typeof error === "string" ? error : "无法选择项目目录");
  }
}

export async function openProjectDirectory(
  deviceId: string,
  projectId: string,
): Promise<void> {
  if (!(await inputCapabilities()).projectDirectory)
    throw new Error("此输入端暂未提供项目目录打开");
  try {
    await invoke("open_project_directory", { deviceId, projectId });
  } catch (cause) {
    throw new Error(typeof cause === "string" ? cause : "无法打开本机项目目录");
  }
}

export async function chooseBackup(
  mode: "open" | "save",
): Promise<string | null> {
  if (!(await inputCapabilities()).localService)
    throw new Error("当前输入端不提供本机数据恢复");
  try {
    return await invoke<string | null>("choose_backup", { mode });
  } catch (error) {
    throw new Error(typeof error === "string" ? error : "无法选择备份文件");
  }
}

export interface LocalInstallation {
  root: string;
  isDefault: boolean;
  developmentOverride: boolean;
}

export interface LocalInstallationPreview {
  id: string;
  root: string;
  isDefault: boolean;
  snapshot: Snapshot;
}

async function installationRequest<T>(
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  try {
    return await invoke<T>(command, args);
  } catch (cause) {
    throw new Error(typeof cause === "string" ? cause : "无法读取本机配置位置");
  }
}

export const localInstallation = () =>
  installationRequest<LocalInstallation>("local_installation_status");

export const prepareLocalInstallation = (mode: "choose" | "default") =>
  installationRequest<LocalInstallationPreview | null>(
    "local_installation_prepare",
    { mode },
  );

export const applyLocalInstallation = (id: string) =>
  installationRequest<LocalInstallation>("local_installation_apply", { id });
