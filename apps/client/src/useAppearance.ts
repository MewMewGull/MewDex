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
function luminance(channels: number[]) {
  const [red, green, blue] = channels.map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return red * 0.2126 + green * 0.7152 + blue * 0.0722;
}
function readableAccent(
  channels: number[],
  background: number[],
  target: number,
) {
  const backgroundLuminance = luminance(background);
  // Preserve the chosen color until mixing is needed for readable UI text.
  for (let step = 0; step <= 100; step++) {
    const mixed = channels.map((channel) =>
      Math.round(channel + (target - channel) * (step / 100)),
    );
    const foregroundLuminance = luminance(mixed);
    const contrast =
      (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) /
      (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
    if (contrast >= 4.5)
      return `#${mixed.map((channel) => channel.toString(16).padStart(2, "0")).join("")}`;
  }
  return target === 0 ? "#000000" : "#ffffff";
}
function apply(current: Appearance) {
  const root = document.documentElement;
  root.dataset.theme = current.theme;
  root.dataset.motion = current.motion ? "full" : "reduced";
  root.style.setProperty("--ui-scale", String(current.fontSize / 14));
  root.style.setProperty("--message-font-size", `${current.messageSize}px`);
  root.style.setProperty("--ui-font", fonts[current.font] ?? fonts.system);
  const accent = /^#[\da-f]{6}$/i.test(current.accent)
    ? current.accent
    : defaults.accent;
  const channels = [1, 3, 5].map((offset) =>
    parseInt(accent.slice(offset, offset + 2), 16),
  );
  const accentLuminance = luminance(channels);
  const darkContrast =
    (accentLuminance + 0.05) / (luminance([16, 19, 27]) + 0.05);
  const lightContrast = 1.05 / (accentLuminance + 0.05);
  root.style.setProperty("--accent", accent);
  root.style.setProperty(
    "--accent-text-dark",
    readableAccent(channels, [37, 45, 60], 255),
  );
  root.style.setProperty(
    "--accent-text-light",
    readableAccent(channels, [228, 233, 241], 0),
  );
  root.style.setProperty(
    "--accent-contrast",
    darkContrast >= lightContrast ? "#10131b" : "#ffffff",
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
