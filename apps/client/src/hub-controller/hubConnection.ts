import type { Snapshot } from "./localProtocol";

export function sameHubAddress(first: string, second: string): boolean {
  try {
    const a = new URL(first);
    const b = new URL(second);
    const host = (value: string) =>
      ["localhost", "127.0.0.1", "[::1]"].includes(value) ? "loopback" : value;
    return (
      a.protocol === b.protocol &&
      host(a.hostname) === host(b.hostname) &&
      a.port === b.port &&
      a.pathname.replace(/\/$/, "") === b.pathname.replace(/\/$/, "")
    );
  } catch {
    return false;
  }
}

export function isLocalHub(
  snapshot: Pick<Snapshot, "hub" | "deviceId" | "baseUrl"> | null,
  address: string,
  hubDeviceId?: string,
): boolean {
  if (!snapshot?.hub) return false;
  if (hubDeviceId) return hubDeviceId === snapshot.deviceId;
  return !!snapshot.baseUrl && sameHubAddress(address, snapshot.baseUrl);
}
