import { invoke, isTauri } from "@tauri-apps/api/core";
import { inputCapabilities } from "./localDevice";

const previewImageTypes = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/bmp",
]);

export function canPreviewImage(mediaType: string): boolean {
  return previewImageTypes.has(mediaType.split(";")[0].trim().toLowerCase());
}

export async function canSaveAttachment(): Promise<boolean> {
  return !isTauri() || !!(await inputCapabilities()).attachmentSave;
}

export async function saveAttachmentFile(
  name: string,
  content: Blob,
): Promise<"saved" | "download-started" | "cancelled"> {
  if (isTauri())
    return (await invoke<boolean>(
      "save_attachment_file",
      await content.arrayBuffer(),
      {
        headers: { "x-mewdex-file": new URLSearchParams({ name }).toString() },
      },
    ))
      ? "saved"
      : "cancelled";
  const url = URL.createObjectURL(content);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  return "download-started";
}
