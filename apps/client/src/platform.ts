import type { Credentials } from "@mewdex/sdk";
import { isTauri, invoke } from "@tauri-apps/api/core";

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
export interface CredentialStore {
  load(): Promise<SavedConnection | null>;
  save(value: Connection): Promise<void>;
  clear(): Promise<void>;
}

export interface SavedConnection {
  baseURL: string;
  credentials: Pick<Credentials, "token">;
}
const saved = (value: Connection): SavedConnection => ({
  baseURL: value.baseURL,
  credentials: { token: value.credentials.token },
});

// The web development adapter is deliberately session-scoped. Native vaults implement this interface.
const webCredentialStore: CredentialStore = {
  async load() {
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
    window.removeEventListener("online", visible);
    window.removeEventListener("pageshow", restored);
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
    throw new Error("登录地址不是官方 HTTPS 地址");
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
    throw new Error("只能打开不含内嵌凭据的 HTTPS 网页");
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
    throw new Error("网页版地址需要使用 HTTPS 或本机回环地址");
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
