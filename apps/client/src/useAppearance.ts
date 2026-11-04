import { useSyncExternalStore } from "react";
import {
  accentColors,
  activeColorTheme,
  appearanceFonts,
  defaultAppearance,
  normalizeThemeColor,
  parseAppearance,
  parseThemeFile,
  resolveThemeColors,
  type Appearance,
  type ThemeColorId,
} from "./appearance";

const key = "mewdex.appearance.v2";
const legacyKey = "mewdex.appearance.v1";
const system = window.matchMedia("(prefers-color-scheme: dark)");
let value = defaultAppearance;
let error = "";
try {
  value = parseAppearance(
    localStorage.getItem(key) ?? localStorage.getItem(legacyKey),
  );
} catch {
  // Settings remain usable when the WebView denies local storage access.
}
const listeners = new Set<() => void>();
let snapshot = {
  value,
  error,
  activeTheme: activeColorTheme(value, system.matches),
};

function apply() {
  const theme = activeColorTheme(value, system.matches);
  const colors = resolveThemeColors(theme, value.colorCustomizations[theme.id]);
  const accent = accentColors(colors);
  const root = document.documentElement;
  for (const [id, color] of Object.entries(colors))
    root.style.setProperty(`--mewdex-${id.replaceAll(".", "-")}`, color);
  root.style.setProperty("--accent-text", accent.text);
  root.style.setProperty("--accent-contrast", accent.contrast);
  root.style.setProperty("--ui-scale", String(value.fontSize / 14));
  root.style.setProperty("--message-font-size", `${value.messageSize}px`);
  root.style.setProperty("--ui-font", appearanceFonts[value.font]);
  root.dataset.theme = theme.type;
  root.dataset.colorTheme = theme.id;
  root.dataset.motion = value.motion ? "full" : "reduced";
  snapshot = { value, error, activeTheme: theme };
}

export function initializeAppearance() {
  apply();
}

function publish() {
  apply();
  for (const listener of listeners) listener();
}

function save(next: Appearance) {
  value = next;
  try {
    localStorage.setItem(key, JSON.stringify(value));
    error = "";
  } catch {
    error = "外观设置未保存，关闭工作台后可能恢复原设置。";
  }
  publish();
}

function onStorage(event: StorageEvent) {
  if (
    event.storageArea !== localStorage ||
    (event.key !== key && event.key !== null)
  )
    return;
  value = parseAppearance(event.newValue);
  error = "";
  publish();
}

function subscribe(listener: () => void) {
  if (listeners.size === 0) {
    system.addEventListener("change", publish);
    window.addEventListener("storage", onStorage);
    apply();
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      system.removeEventListener("change", publish);
      window.removeEventListener("storage", onStorage);
    }
  };
}

export function useAppearance() {
  const state = useSyncExternalStore(subscribe, () => snapshot);
  return {
    ...state,
    update(patch: Partial<Appearance>) {
      save(parseAppearance(JSON.stringify({ ...value, ...patch })));
    },
    setColor(id: ThemeColorId, input?: string) {
      const color =
        input === undefined ? undefined : normalizeThemeColor(input);
      if (input !== undefined && !color) return;
      const theme = activeColorTheme(value, system.matches);
      const colors = { ...value.colorCustomizations[theme.id] };
      if (color) colors[id] = color;
      else delete colors[id];
      save({
        ...value,
        colorCustomizations: {
          ...value.colorCustomizations,
          [theme.id]: colors,
        },
      });
    },
    resetColors() {
      const theme = activeColorTheme(value, system.matches);
      const colorCustomizations = { ...value.colorCustomizations };
      delete colorCustomizations[theme.id];
      save({ ...value, colorCustomizations });
    },
    importTheme(text: string) {
      const file = parseThemeFile(text);
      const theme = {
        id: `user-${crypto.randomUUID()}`,
        name: file.name,
        type: file.type,
        colors: file.colors,
      };
      save({
        ...value,
        theme: theme.id,
        customThemes: [...value.customThemes, theme],
      });
      return theme;
    },
    removeTheme(id: string) {
      const removed = value.customThemes.find((theme) => theme.id === id);
      if (!removed) return;
      const colorCustomizations = { ...value.colorCustomizations };
      delete colorCustomizations[id];
      save(
        parseAppearance(
          JSON.stringify({
            ...value,
            customThemes: value.customThemes.filter((theme) => theme.id !== id),
            theme: value.theme === id ? `mewdex-${removed.type}` : value.theme,
            colorCustomizations,
          }),
        ),
      );
    },
  };
}
