import type { ActionType, Options } from "@tauri-apps/plugin-notification";
import type { NotificationAdapter } from "./notificationAdapter";

export interface NativeNotificationRuntime {
  supported(): Promise<boolean>;
  permission(): Promise<boolean | null>;
  request(): Promise<string>;
  activate(): Promise<void>;
  registerActions(types: ActionType[]): Promise<void>;
  onAction(
    handler: (action: NativeNotificationAction) => void,
  ): Promise<{ unregister(): Promise<void> }>;
  notify(options: Options): Promise<void>;
}

function notificationId(key: string): number {
  let hash = 2166136261;
  for (let i = 0; i < key.length; i++)
    hash = Math.imul(hash ^ key.charCodeAt(i), 16777619);
  return hash | 0;
}

// Platform calls are injected so the same behavior can be checked without
// requesting an OS permission or sending real notifications during unit checks.
export function createNativeNotificationAdapter(
  runtime: NativeNotificationRuntime,
): NotificationAdapter {
  const permission: NotificationAdapter["permission"] = async () => {
    if (!(await runtime.supported())) return "unsupported";
    const value = await runtime.permission();
    return value === true ? "granted" : value === false ? "denied" : "default";
  };
  return {
    permission,
    async request() {
      const current = await permission();
      if (current === "unsupported" || current === "granted") return current;
      const value = await runtime.request();
      return value === "granted"
        ? "granted"
        : value === "denied"
          ? "denied"
          : "default";
    },
    activate: () => runtime.activate(),
    async listen(scope, open) {
      if (!(await runtime.supported())) return () => {};
      await runtime.registerActions([
        {
          id: "mewdex-conversation",
          actions: [
            {
              id: "open",
              title: "打开对话",
              foreground: true,
              requiresAuthentication: true,
            },
          ],
        },
      ]);
      const listener = await runtime.onAction((event) => {
        const extra = event.notification?.extra;
        if (
          (event.actionId === "open" || event.actionId === "tap") &&
          extra?.scope === scope &&
          typeof extra.deviceId === "string" &&
          typeof extra.sessionId === "string"
        )
          open(extra.deviceId, extra.sessionId);
      });
      return () => {
        void listener.unregister().catch(() => {});
      };
    },
    async send(scope, alert, _open, _failed, signal) {
      if (signal.aborted) return;
      if ((await permission()) !== "granted")
        throw new Error("系统提醒权限尚未开启");
      if (signal.aborted) return;
      await runtime.notify({
        id: notificationId(scope + ":" + alert.key),
        title: alert.title,
        body: alert.body,
        actionTypeId: "mewdex-conversation",
        visibility: 0,
        extra: { scope, deviceId: alert.deviceId, sessionId: alert.sessionId },
      });
    },
  };
}
export interface NativeNotificationAction {
  actionId: string;
  notification?: { id?: number; extra?: Record<string, unknown> | null } | null;
}
