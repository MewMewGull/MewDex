import { useEffect, useSyncExternalStore } from "react";

export interface Appearance {
  theme: "system" | "dark" | "light";
  font: string;
  fontSize: number;
  messageSize: number;
  accent: string;
  motion: boolean;
}
const defaults: Appearance = {
  theme: "system",
  font: "system",
  fontSize: 14,
  messageSize: 16,
  accent: "#9ccba6",
  motion: true,
};
const key = "mewdex.appearance.v1";
let value: Appearance = defaults;
try {
  value = { ...defaults, ...JSON.parse(localStorage.getItem(key) || "{}") };
} catch {
  /* Defaults remain available when local storage is unavailable. */
}
let error = "";
const listeners = new Set<() => void>();
let snapshot = { value, error };
const fonts: Record<string, string> = {
  system: 'Inter, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  yahei: '"Microsoft YaHei", "PingFang SC", sans-serif',
  segoe: '"Segoe UI", "Microsoft YaHei", sans-serif',
  mono: '"Cascadia Code", Consolas, "Microsoft YaHei", monospace',
};
function apply(current: Appearance) {
  const root = document.documentElement;
  root.dataset.theme = current.theme;
  root.dataset.motion = current.motion ? "full" : "reduced";
  root.style.setProperty("--ui-scale", String(current.fontSize / 14));
  root.style.setProperty("--message-font-size", `${current.messageSize}px`);
  root.style.setProperty("--ui-font", fonts[current.font] ?? fonts.system);
  root.style.setProperty("--accent", current.accent);
  const red = parseInt(current.accent.slice(1, 3), 16),
    green = parseInt(current.accent.slice(3, 5), 16),
    blue = parseInt(current.accent.slice(5, 7), 16);
  root.style.setProperty(
    "--accent-contrast",
    red * 0.299 + green * 0.587 + blue * 0.114 > 155 ? "#142019" : "#ffffff",
  );
}
export function useAppearance() {
  const state = useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    () => snapshot,
  );
  useEffect(() => apply(state.value), [state.value]);
  return {
    ...state,
    update(patch: Partial<Appearance>) {
      value = { ...value, ...patch };
      try {
        localStorage.setItem(key, JSON.stringify(value));
        error = "";
      } catch {
        error = "外观设置未保存，关闭工作台后可能恢复原设置。";
      }
      snapshot = { value, error };
      apply(value);
      for (const listener of listeners) listener();
    },
  };
}
