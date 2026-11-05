import { t } from "./localization.ts";
import { Channel, invoke, isTauri } from "@tauri-apps/api/core";
import { create } from "zustand";
import { subscribeResume } from "./platform";
import {
  readUpdatePreferences,
  updateCheckDue,
  defaultUpdatePreferences as defaults,
  updateStorageKey as storageKey,
  type UpdatePreferences,
} from "./appUpdatePreferences";
export type { UpdateChannel } from "./appUpdatePreferences";
export interface UpdateInfo {
  currentVersion: string;
  platform: string;
  arch: string;
  canDownload: boolean;
}
export interface UpdatePackage {
  name: string;
  bytes: number;
  sha256: string;
  updateMode: string;
  installation: string;
}
export interface AppRelease {
  tag: string;
  version: string;
  notes: string;
  url: string;
  publishedAt: string | null;
  prerelease: boolean;
  package: UpdatePackage | null;
}
interface Progress {
  downloaded: number;
  total: number;
}
type Phase =
  | "idle"
  | "checking"
  | "available"
  | "current"
  | "downloading"
  | "ready"
  | "installing";

function initialPreferences() {
  try {
    return readUpdatePreferences(localStorage.getItem(storageKey));
  } catch {
    return { ...defaults };
  }
}

function message(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

interface Updates {
  info: UpdateInfo | null;
  preferences: UpdatePreferences;
  release: AppRelease | null;
  phase: Phase;
  progress: Progress | null;
  error: string;
  open: boolean;
  dismissedTag: string;
  initialize: () => Promise<void>;
  configure: (
    patch: Partial<
      Pick<UpdatePreferences, "automatic" | "channel" | "ignoredTag">
    >,
  ) => void;
  check: (manual?: boolean) => Promise<void>;
  download: () => Promise<void>;
  cancel: () => Promise<void>;
  install: () => Promise<void>;
  show: () => void;
  dismiss: () => void;
}

export const useAppUpdates = create<Updates>((set, get) => ({
  info: null,
  preferences: initialPreferences(),
  release: null,
  phase: "idle",
  progress: null,
  error: "",
  open: false,
  dismissedTag: "",
  async initialize() {
    if (!isTauri() || get().info) return;
    try {
      set({ info: await invoke<UpdateInfo>("app_update_info") });
    } catch (error) {
      set({ error: message(error) });
    }
  },
  configure(patch) {
    if (["checking", "downloading", "installing"].includes(get().phase)) return;
    const previous = get().preferences;
    const changed = patch.channel && patch.channel !== previous.channel;
    const enabled = patch.automatic === true && !previous.automatic;
    const preferences = {
      ...previous,
      ...patch,
      ...(changed ? { lastAttempt: 0 } : {}),
    };
    try {
      localStorage.setItem(storageKey, JSON.stringify(preferences));
      set({
        preferences,
        error: "",
        ...(changed ? { release: null, phase: "idle", progress: null } : {}),
      });
      if (preferences.automatic && (changed || enabled)) void get().check();
    } catch {
      set({ error: t("无法保存更新设置，请检查本机存储。") });
    }
  },
  async check(manual = false) {
    if (
      !isTauri() ||
      ["checking", "downloading", "installing"].includes(get().phase)
    )
      return;
    if (
      !manual &&
      !updateCheckDue(get().preferences, Date.now(), !!get().error)
    )
      return;
    set({ phase: "checking", error: "" });
    const preferences = { ...get().preferences, lastAttempt: Date.now() };
    set({ preferences });
    try {
      localStorage.setItem(storageKey, JSON.stringify(preferences));
    } catch {
      // The current run still throttles checks when webview storage is unavailable.
    }
    try {
      const release = await invoke<AppRelease | null>("app_update_check", {
        channel: preferences.channel,
      });
      set({
        release,
        phase: release ? "available" : "current",
        progress: null,
        ...(manual ? { dismissedTag: "" } : {}),
      });
    } catch (error) {
      set({
        phase: get().release ? "available" : "idle",
        error: message(error),
      });
    }
  },
  async download() {
    const { release, info, phase } = get();
    if (
      !info?.canDownload ||
      !release?.package ||
      !["available", "ready"].includes(phase)
    )
      return;
    set({
      phase: "downloading",
      error: "",
      progress: { downloaded: 0, total: release.package.bytes },
    });
    const onProgress = new Channel<Progress>();
    onProgress.onmessage = (progress) => set({ progress });
    try {
      await invoke("app_update_download", { tag: release.tag, onProgress });
      set({ phase: "ready" });
    } catch (error) {
      set({ phase: "available", error: message(error), progress: null });
    }
  },
  async cancel() {
    if (get().phase !== "downloading") return;
    try {
      await invoke("app_update_cancel");
    } catch (error) {
      set({ error: message(error) });
    }
  },
  async install() {
    const { release, phase } = get();
    if (!release || phase !== "ready") return;
    set({ phase: "installing", error: "" });
    try {
      await invoke("app_update_install", { tag: release.tag });
      set({ phase: "ready" });
    } catch (error) {
      set({ phase: "ready", error: message(error) });
    }
  },
  show: () => set({ open: true }),
  dismiss: () => set({ dismissedTag: get().release?.tag ?? "" }),
}));

// Kept outside the network workspace so checks and the update dialog also work
// before pairing a device and while its Hub is offline.
export function startAppUpdateChecks() {
  if (!isTauri()) return () => {};
  let stopped = false;
  void useAppUpdates
    .getState()
    .initialize()
    .then(() => {
      if (!stopped) void useAppUpdates.getState().check();
    });
  const timer = setInterval(
    () => void useAppUpdates.getState().check(),
    60 * 60 * 1000,
  );
  const unsubscribe = subscribeResume(
    () => void useAppUpdates.getState().check(),
  );
  return () => {
    stopped = true;
    clearInterval(timer);
    unsubscribe();
  };
}
