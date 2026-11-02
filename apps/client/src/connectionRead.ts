import { MewDexError } from "@mewdex/sdk";

// Only connection reads use this policy. User commands keep their original
// receipt and must never be replayed by a connection retry.
export function retryableConnectionRead(cause: unknown): boolean {
  if (cause instanceof MewDexError)
    return [408, 425, 429].includes(cause.status) || cause.status >= 500;
  return (
    cause instanceof TypeError ||
    (cause instanceof DOMException && cause.name === "TimeoutError")
  );
}

export const connectionRetryDelay = (failures: number) =>
  Math.min(1000 * 2 ** Math.min(failures, 4), 15000);

export async function readConnection<T>(
  read: (signal: AbortSignal) => Promise<T>,
  signal: AbortSignal,
  timeoutMs = 10000,
): Promise<T> {
  signal.throwIfAborted();
  const request = new AbortController();
  const abort = () => request.abort(signal.reason);
  signal.addEventListener("abort", abort, { once: true });
  const timer = setTimeout(
    () =>
      request.abort(
        new DOMException("连接超时，请检查设备网络", "TimeoutError"),
      ),
    timeoutMs,
  );
  try {
    return await read(request.signal);
  } finally {
    clearTimeout(timer);
    signal.removeEventListener("abort", abort);
  }
}

export function sameNetworkAddress(first: string, second: string): boolean {
  const a = new URL(first),
    b = new URL(second);
  return (
    a.origin === b.origin &&
    a.pathname.replace(/\/$/, "") === b.pathname.replace(/\/$/, "")
  );
}
