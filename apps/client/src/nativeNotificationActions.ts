import type { NativeNotificationRuntime } from "./nativeNotificationAdapter";

export interface NativeNotificationHint {
  actionId: string;
  notificationId: number;
  scope: string;
  deviceId: string;
  sessionId: string;
}
interface HintSource {
  subscribe(wake: () => void): Promise<() => void>;
  resume(wake: () => void): () => void;
  take(): Promise<NativeNotificationHint | null>;
}

// Subscribe before draining startup state. Wakeups coalesce while an IPC read is
// pending, and a suspended input also drains its native inbox on return.
export async function listenNativeNotificationHints(
  source: HintSource,
  handler: Parameters<NativeNotificationRuntime["onAction"]>[0],
): Promise<{ unregister(): Promise<void> }> {
  let disposed = false,
    requested = false;
  let running: Promise<void> | undefined;
  const drain = () => {
    requested = true;
    if (!running) {
      running = (async () => {
        do {
          requested = false;
          const hint = await source.take();
          if (!disposed && hint)
            handler({
              actionId: hint.actionId,
              notification: {
                id: hint.notificationId,
                extra: {
                  scope: hint.scope,
                  deviceId: hint.deviceId,
                  sessionId: hint.sessionId,
                },
              },
            });
        } while (requested && !disposed);
      })().finally(() => {
        running = undefined;
      });
    }
    return running;
  };
  const wake = () => {
    if (!disposed) void drain().catch(() => {});
  };
  const off = await source.subscribe(wake);
  const offResume = source.resume(wake);
  const close = () => {
    disposed = true;
    off();
    offResume();
  };
  try {
    await drain();
  } catch (error) {
    close();
    throw error;
  }
  return {
    async unregister() {
      close();
    },
  };
}
