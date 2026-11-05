import { t } from "./localization.ts";
import { invoke, isTauri } from "@tauri-apps/api/core";
import type { MewDexClient, OutputFile, Session } from "@mewdex/sdk";
import { deviceRequest, inputCapabilities } from "./localDevice";

export interface ProjectOpener {
  id: string;
  name: string;
}
export async function projectOpeners(): Promise<ProjectOpener[]> {
  return isTauri() && (await inputCapabilities()).projectDirectory
    ? invoke<ProjectOpener[]>("project_openers")
    : [];
}
export async function isLocalProject(deviceId: string): Promise<boolean> {
  if (!isTauri() || !(await inputCapabilities()).projectDirectory) return false;
  return (
    (await deviceRequest({ operation: "status" })).snapshot.deviceId ===
    deviceId
  );
}
export async function openLocalProjectFile(
  session: Pick<Session, "deviceId" | "projectId">,
  relative: string,
  action = "default",
): Promise<void> {
  try {
    await invoke("open_project_file", {
      deviceId: session.deviceId,
      projectId: session.projectId,
      relative,
      action,
    });
  } catch (cause) {
    throw new Error(typeof cause === "string" ? cause : t("无法打开文件"));
  }
}
export async function openReceivedFile(
  client: MewDexClient,
  output: OutputFile,
  action = "default",
): Promise<string | null> {
  if (!isTauri() || !(await inputCapabilities()).attachmentSave) return null;
  const bytes = await client.attachmentContent(output.attachment.id);
  try {
    return await invoke<string>(
      "open_received_file",
      await bytes.arrayBuffer(),
      {
        headers: {
          "x-mewdex-file": new URLSearchParams({
            name: output.attachment.name,
            action,
          }).toString(),
        },
      },
    );
  } catch (cause) {
    throw new Error(typeof cause === "string" ? cause : t("无法打开本机副本"));
  }
}
const defaultKey = "mewdex.file-openers.v1";
export function defaultProjectOpener(
  deviceId: string,
  projectId: string,
): string {
  const values = JSON.parse(localStorage.getItem(defaultKey) || "{}") as Record<
    string,
    string
  >;
  return values[`${deviceId}:${projectId}`] || "default";
}
export function setDefaultProjectOpener(
  deviceId: string,
  projectId: string,
  opener: string,
): void {
  const values = JSON.parse(localStorage.getItem(defaultKey) || "{}") as Record<
    string,
    string
  >;
  values[`${deviceId}:${projectId}`] = opener;
  localStorage.setItem(defaultKey, JSON.stringify(values));
}
