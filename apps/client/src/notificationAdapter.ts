import { invoke, isTauri } from "@tauri-apps/api/core";
import { inputCapabilities } from "./localDevice";
import type { InputAlert } from "./inputAlerts";
import { createNativeNotificationAdapter } from "./nativeNotificationAdapter";
import {
  listenNativeNotificationHints,
  type NativeNotificationHint,
} from "./nativeNotificationActions";
import { subscribeResume } from "./platform";

export type AlertPermission = "unsupported" | "default" | "denied" | "granted";
export interface NotificationAdapter {
  permission(): Promise<AlertPermission>;
  request(): Promise<AlertPermission>;
  activate(): Promise<void>;
  listen(
    scope: string,
    open: (deviceId: string, sessionId: string) => void,
  ): Promise<() => void>;
  send(
    scope: string,
    alert: InputAlert,
    open: () => void,
    failed: () => void,
    signal: AbortSignal,
  ): Promise<void>;
}

const nativeAdapter = createNativeNotificationAdapter({
  supported: async () => !!(await inputCapabilities()).notifications,
  // The plugin's JS convenience method caches Notification.permission. Read
  // its native command so a revocation in OS settings is visible on resume.
  permission: async () =>
    invoke<boolean | null>(
      (await inputCapabilities()).desktopNotifications
        ? "desktop_notification_permission"
        : "plugin:notification|is_permission_granted",
    ),
  async request() {
    if ((await inputCapabilities()).desktopNotifications)
      return invoke<string>("desktop_notification_enable");
    const plugin = await import("@tauri-apps/plugin-notification");
    return plugin.requestPermission();
  },
  async activate() {
    // A mobile notification's foreground action is handled by the OS. Desktop
    // windows may still be minimized when the authorized conversation opens.
    if (!(await inputCapabilities()).localService) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    const window = getCurrentWindow();
    await window.show();
    await window.unminimize();
    await window.setFocus();
  },
  async registerActions(types) {
    const plugin = await import("@tauri-apps/plugin-notification");
    await plugin.registerActionTypes(types);
  },
  async onAction(handler) {
    if ((await inputCapabilities()).notificationActions) {
      const { listen } = await import("@tauri-apps/api/event");
      return listenNativeNotificationHints(
        {
          subscribe: (wake) => listen("mewdex://notification-action", wake),
          resume: subscribeResume,
          take: () =>
            invoke<NativeNotificationHint | null>("notification_take_action"),
        },
        handler,
      );
    }
    const plugin = await import("@tauri-apps/plugin-notification");
    return plugin.onAction(handler);
  },
  // Await the native result; the void JS convenience API cannot expose a
  // rejected dispatch command to the input's synchronization/error handling.
  notify: async (options) =>
    invoke<void>(
      (await inputCapabilities()).desktopNotifications
        ? "desktop_notification_send"
        : "plugin:notification|notify",
      { options },
    ),
});

const webAdapter: NotificationAdapter = {
  async permission() {
    return typeof window.Notification === "undefined"
      ? "unsupported"
      : window.Notification.permission;
  },
  async request() {
    return typeof window.Notification === "undefined"
      ? "unsupported"
      : window.Notification.requestPermission();
  },
  async activate() {
    window.focus();
  },
  async listen() {
    return () => {};
  },
  async send(_scope, alert, open, failed, signal) {
    if (signal.aborted) return;
    if (window.Notification.permission !== "granted")
      throw new Error("浏览器提醒权限尚未开启");
    const notification = new window.Notification(alert.title, {
      body: alert.body,
      tag: alert.key,
    });
    notification.onclick = () => {
      notification.close();
      open();
    };
    notification.onerror = failed;
  },
};

export const notificationAdapter: NotificationAdapter = isTauri()
  ? nativeAdapter
  : webAdapter;
