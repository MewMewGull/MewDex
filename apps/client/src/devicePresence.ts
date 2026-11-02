import type { Device } from "@mewdex/sdk";

// A device snapshot remains useful during reconnection, but cannot describe
// its current presence until the input has synchronized with the network.
export function devicePresence(
  online: boolean | undefined,
  connected: boolean,
) {
  return {
    label:
      online === undefined
        ? "等待同步"
        : connected
          ? online
            ? "在线"
            : "离线"
          : online
            ? "上次在线"
            : "上次离线",
    dot: connected && online ? "online" : "offline",
  };
}
export function devicePlatform(platform: string) {
  if (platform.startsWith("darwin")) return "macOS";
  if (platform.startsWith("windows")) return "Windows";
  if (platform.startsWith("linux")) return "Linux";
  if (platform.startsWith("android")) return "Android";
  if (platform.startsWith("ios")) return "iOS";
  return platform;
}

export function deviceDescription(device: Device, connected: boolean) {
  return [
    devicePresence(device.online, connected).label,
    devicePlatform(device.platform),
    !device.roles.includes("executor")
      ? "未开启执行能力"
      : device.projects.length
        ? `${device.projects.length} 个项目`
        : "暂无可用项目",
  ]
    .filter(Boolean)
    .join(" · ");
}
