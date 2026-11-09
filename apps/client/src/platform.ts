import { t } from "./localization.ts";
import type { Credentials } from "@mewdex/sdk";
import { isTauri, invoke } from "@tauri-apps/api/core";
import { nextHostedChannelRequest } from "./inputChannelHost.ts";

export interface Connection {
  baseURL: string;
  credentials: Credentials;
}

export function hostedHubURL(): string | undefined {
  if (
    !isTauri() &&
    document
      .querySelector('meta[name="mewdex-hub"]')
      ?.getAttribute("content") === "same-origin"
  )
    return window.location.origin;
}

export function hubDisplayURL(baseURL: string): string {
  const hosted = hostedHubURL();
  if (!hosted) return baseURL;
  try {
    if (new URL(baseURL).href.replace(/\/+$/, "") !== hosted) return baseURL;
  } catch {
    return baseURL;
  }
  return (
    document
      .querySelector('meta[name="mewdex-hub-url"]')
      ?.getAttribute("content") || baseURL
  );
}

export interface CredentialStore {
  load(): Promise<SavedConnection | null>;
  save(value: Connection): Promise<void>;
  clear(): Promise<void>;
}

export interface SavedConnection {
  baseURL: string;
  credentials: Pick<Credentials, "token" | "hubDeviceId" | "inputProxy"> & {
    identityId?: string;
  };
}
const saved = (value: Connection): SavedConnection => ({
  baseURL: value.baseURL,
  credentials: {
    token: value.credentials.token,
    hubDeviceId: value.credentials.hubDeviceId,
    inputProxy: value.credentials.inputProxy,
    identityId: value.credentials.identity.id,
  },
});

// Standalone web inputs can restore their background connection from a tray-authorized tab.
const webCredentialStore: CredentialStore = {
  async load() {
    if (
      document
        .querySelector('meta[name="mewdex-input-channels"]')
        ?.getAttribute("content") === "hosted"
    ) {
      const hash = window.location.hash;
      if (hash?.startsWith("#mewdex-input=")) {
        sessionStorage.setItem(
          "mewdex.web-panel-key",
          hash.slice("#mewdex-input=".length),
        );
        history.replaceState(
          null,
          "",
          window.location.pathname + window.location.search,
        );
      }
      const key = sessionStorage.getItem("mewdex.web-panel-key");
      if (key) {
        const response = await fetch("/__mewdex/session", {
          headers: { Authorization: `Bearer ${key}` },
        });
        if (response.ok) {
          const connection = (await response.json()) as SavedConnection | null;
          if (connection) {
            sessionStorage.setItem(
              "mewdex.connection",
              JSON.stringify(connection),
            );
            return connection;
          }
        }
      }
    }
    const value = sessionStorage.getItem("mewdex.connection");
    if (!value) return null;
    try {
      return JSON.parse(value) as SavedConnection;
    } catch {
      return null;
    }
  },
  async save(value) {
    sessionStorage.setItem("mewdex.connection", JSON.stringify(saved(value)));
  },
  async clear() {
    const raw = sessionStorage.getItem("mewdex.connection");
    if (
      raw &&
      document
        .querySelector('meta[name="mewdex-input-channels"]')
        ?.getAttribute("content") === "hosted"
    ) {
      const connection = JSON.parse(raw) as SavedConnection;
      const response = await fetch("/__mewdex/channels", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${connection.credentials.token}`,
        },
        body: JSON.stringify({
          operation: "disconnect",
          ...nextHostedChannelRequest(),
        }),
      });
      if (!response.ok) throw new Error(await response.text());
    }
    sessionStorage.removeItem("mewdex.connection");
  },
};

const nativeCredentialStore: CredentialStore = {
  load: () => invoke<SavedConnection | null>("vault_load"),
  save: (connection) =>
    invoke<void>("vault_save", { connection: saved(connection) }),
  clear: () => invoke<void>("vault_clear"),
};

export const credentialStore: CredentialStore = isTauri()
  ? nativeCredentialStore
  : webCredentialStore;

// The input refreshes projections on return; execution stays on its target.
export function subscribeResume(callback: () => void): () => void {
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let unlisten: (() => void) | undefined;
  const notify = () => {
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => {
      if (!disposed && document.visibilityState === "visible") callback();
    }, 150);
  };
  const visible = () => {
    if (document.visibilityState === "visible") notify();
  };
  const restored = (event: PageTransitionEvent) => {
    if (event.persisted) notify();
  };
  document.addEventListener("visibilitychange", visible);
  window.addEventListener("focus", visible);
  window.addEventListener("online", visible);
  window.addEventListener("pageshow", restored);
  if (isTauri()) {
    void import("@tauri-apps/api/event")
      .then(({ listen }) => listen("mewdex://resume", notify))
      .then((off) => {
        if (disposed) off();
        else unlisten = off;
      })
      .catch(() => {
        /* DOM lifecycle events remain available. */
      });
  }
  return () => {
    disposed = true;
    if (timer !== undefined) clearTimeout(timer);
    unlisten?.();
    document.removeEventListener("visibilitychange", visible);
    window.removeEventListener("focus", visible);
    window.removeEventListener("online", visible);
    window.removeEventListener("pageshow", restored);
  };
}

// Visibility alone stays true when a desktop window is behind another app.
// Query polling follows the focused input window, with native resume as a fallback.
export function subscribeForeground(
  callback: (foreground: boolean) => void,
): () => void {
  let previous: boolean | undefined;
  const notify = () => {
    const foreground =
      document.visibilityState === "visible" && document.hasFocus();
    if (foreground === previous) return;
    previous = foreground;
    callback(foreground);
  };
  document.addEventListener("visibilitychange", notify);
  window.addEventListener("focus", notify);
  window.addEventListener("blur", notify);
  const resume = subscribeResume(notify);
  notify();
  return () => {
    resume();
    document.removeEventListener("visibilitychange", notify);
    window.removeEventListener("focus", notify);
    window.removeEventListener("blur", notify);
  };
}

export async function openOfficialLogin(raw: string): Promise<void> {
  const url = new URL(raw);
  if (
    url.protocol !== "https:" ||
    (url.hostname !== "chatgpt.com" &&
      url.hostname !== "openai.com" &&
      !url.hostname.endsWith(".openai.com"))
  )
    throw new Error(t("登录地址不是官方 HTTPS 地址"));
  await openExternalLink(raw);
}

export async function openExternalLink(raw: string): Promise<void> {
  const url = new URL(raw);
  if (
    url.protocol !== "https:" ||
    !url.hostname ||
    url.username ||
    url.password
  )
    throw new Error(t("只能打开不含内嵌凭据的 HTTPS 网页"));
  await openLink(url);
}

export async function openWebWorkbench(raw: string): Promise<void> {
  const url = new URL(raw);
  const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  if (
    url.username ||
    url.password ||
    (url.protocol !== "https:" && !(url.protocol === "http:" && loopback))
  )
    throw new Error(t("网页版地址需要使用 HTTPS 或本机回环地址"));
  await openLink(url);
}

async function openLink(url: URL): Promise<void> {
  if (isTauri()) {
    const { openUrl } = await import("@tauri-apps/plugin-opener");
    await openUrl(url.toString());
  } else {
    window.open(url.toString(), "_blank", "noopener,noreferrer");
  }
}
