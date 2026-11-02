import { Laptop, Monitor, Server, Smartphone } from "lucide-react";

export function DeviceIcon({
  platform,
  size = 18,
}: {
  platform: string;
  size?: number;
}) {
  const Icon = platform.startsWith("darwin")
    ? Laptop
    : platform.startsWith("linux")
      ? Server
      : platform.startsWith("android") || platform.startsWith("ios")
        ? Smartphone
        : Monitor;
  return <Icon size={size} aria-hidden="true" />;
}
