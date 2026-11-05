import { t } from "./localization.ts";
export type ThemeType = "dark" | "light";

// Stable color IDs are also the public keys in shared theme files.
export const themeColorRegistry = {
  accent: {
    get label() {
      return t("强调色");
    },
    get group() {
      return t("基础");
    },
    dark: "#9ccba6",
    light: "#397548",
  },
  foreground: {
    get label() {
      return t("主要文字");
    },
    get group() {
      return t("基础");
    },
    dark: "#e0e7f2",
    light: "#263244",
  },
  descriptionForeground: {
    get label() {
      return t("次要文字");
    },
    get group() {
      return t("基础");
    },
    dark: "#91a5be",
    light: "#596b80",
  },
  disabledForeground: {
    get label() {
      return t("弱化文字");
    },
    get group() {
      return t("基础");
    },
    dark: "#788aa1",
    light: "#68788c",
  },
  "editor.background": {
    get label() {
      return t("工作区背景");
    },
    get group() {
      return t("基础");
    },
    dark: "#10131b",
    light: "#ffffff",
  },
  "sideBar.background": {
    get label() {
      return t("侧栏背景");
    },
    get group() {
      return t("基础");
    },
    dark: "#151923",
    light: "#f3f5f8",
  },
  "panel.background": {
    get label() {
      return t("弹窗与面板背景");
    },
    get group() {
      return t("基础");
    },
    dark: "#1b2330",
    light: "#f8fafc",
  },
  "surface.background": {
    get label() {
      return t("卡片背景");
    },
    get group() {
      return t("基础");
    },
    dark: "#19222d",
    light: "#edf1f6",
  },
  "surface.hoverBackground": {
    get label() {
      return t("悬停背景");
    },
    get group() {
      return t("基础");
    },
    dark: "#242d3c",
    light: "#e4eaf2",
  },
  "widget.border": {
    get label() {
      return t("界面边框");
    },
    get group() {
      return t("基础");
    },
    dark: "#354051",
    light: "#cbd4e0",
  },
  "input.background": {
    get label() {
      return t("输入控件背景");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#171c27",
    light: "#ffffff",
  },
  "input.foreground": {
    get label() {
      return t("输入控件文字");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#e0e7f2",
    light: "#263244",
  },
  "input.border": {
    get label() {
      return t("输入控件边框");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#354051",
    light: "#cbd4e0",
  },
  "chat.foreground": {
    get label() {
      return t("回复文字");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#adb9cf",
    light: "#34465d",
  },
  "chat.userForeground": {
    get label() {
      return t("用户消息文字");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#e0e5f0",
    light: "#263244",
  },
  "chat.inputBackground": {
    get label() {
      return t("对话输入区背景");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#192029",
    light: "#f3f5f8",
  },
  "textCodeBlock.background": {
    get label() {
      return t("代码块背景");
    },
    get group() {
      return t("输入与对话");
    },
    dark: "#0d1119",
    light: "#edf1f6",
  },
  "titleBar.activeBackground": {
    get label() {
      return t("标题栏背景");
    },
    get group() {
      return t("标题栏与终端");
    },
    dark: "#151923",
    light: "#f3f5f8",
  },
  "titleBar.activeForeground": {
    get label() {
      return t("标题栏文字");
    },
    get group() {
      return t("标题栏与终端");
    },
    dark: "#91a5be",
    light: "#596b80",
  },
  "terminal.background": {
    get label() {
      return t("终端背景");
    },
    get group() {
      return t("标题栏与终端");
    },
    dark: "#0d1119",
    light: "#f5f7fa",
  },
  "terminal.foreground": {
    get label() {
      return t("终端文字");
    },
    get group() {
      return t("标题栏与终端");
    },
    dark: "#a6b7cc",
    light: "#34465d",
  },
  successForeground: {
    get label() {
      return t("成功状态");
    },
    get group() {
      return t("状态");
    },
    dark: "#bfe3c5",
    light: "#27643a",
  },
  warningForeground: {
    get label() {
      return t("警告状态");
    },
    get group() {
      return t("状态");
    },
    dark: "#dcc89a",
    light: "#805900",
  },
  errorForeground: {
    get label() {
      return t("错误状态");
    },
    get group() {
      return t("状态");
    },
    dark: "#f1b7bc",
    light: "#ad3346",
  },
  infoForeground: {
    get label() {
      return t("提示状态");
    },
    get group() {
      return t("状态");
    },
    dark: "#a8c5ed",
    light: "#285e9e",
  },
} as const;

export type ThemeColorId = keyof typeof themeColorRegistry;
export type ThemeColors = Partial<Record<ThemeColorId, string>>;
export type ResolvedThemeColors = Record<ThemeColorId, string>;

export interface ColorTheme {
  id: string;
  name: string;
  type: ThemeType;
  colors: ThemeColors;
}

export const builtInThemes: readonly ColorTheme[] = [
  {
    id: "mewdex-dark",
    get name() {
      return t("MewDex 深色");
    },
    type: "dark",
    colors: {},
  },
  {
    id: "mewdex-light",
    get name() {
      return t("MewDex 浅色");
    },
    type: "light",
    colors: {},
  },
  {
    id: "midnight",
    get name() {
      return t("午夜蓝");
    },
    type: "dark",
    colors: {
      accent: "#88baff",
      foreground: "#dce8ff",
      descriptionForeground: "#9aafce",
      disabledForeground: "#7f95b5",
      "editor.background": "#0b1220",
      "sideBar.background": "#101b2d",
      "panel.background": "#152238",
      "surface.background": "#18283f",
      "surface.hoverBackground": "#233854",
      "widget.border": "#324968",
      "input.background": "#101b2d",
      "input.foreground": "#dce8ff",
      "input.border": "#324968",
      "chat.foreground": "#b9cbe6",
      "chat.userForeground": "#dce8ff",
      "chat.inputBackground": "#152238",
      "textCodeBlock.background": "#080f1b",
      "titleBar.activeBackground": "#101b2d",
      "titleBar.activeForeground": "#9aafce",
      "terminal.background": "#080f1b",
      "terminal.foreground": "#b9cbe6",
    },
  },
  {
    id: "warm-paper",
    get name() {
      return t("暖纸");
    },
    type: "light",
    colors: {
      accent: "#986337",
      foreground: "#3e352c",
      descriptionForeground: "#75685a",
      disabledForeground: "#817467",
      "editor.background": "#faf7f0",
      "sideBar.background": "#f1ebe0",
      "panel.background": "#fffcf5",
      "surface.background": "#eee7db",
      "surface.hoverBackground": "#e7dece",
      "widget.border": "#d2c5b3",
      "input.background": "#fffcf5",
      "input.foreground": "#3e352c",
      "input.border": "#d2c5b3",
      "chat.foreground": "#584a3d",
      "chat.userForeground": "#3e352c",
      "chat.inputBackground": "#f1ebe0",
      "textCodeBlock.background": "#eee7db",
      "titleBar.activeBackground": "#f1ebe0",
      "titleBar.activeForeground": "#75685a",
      "terminal.background": "#f1ebe0",
      "terminal.foreground": "#584a3d",
    },
  },
];

export const appearanceFonts = {
  system: 'Inter, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  yahei: '"Microsoft YaHei", "PingFang SC", sans-serif',
  segoe: '"Segoe UI", "Microsoft YaHei", sans-serif',
  mono: '"Cascadia Code", Consolas, "Microsoft YaHei", monospace',
};

export interface Appearance {
  theme: string;
  preferredDarkTheme: string;
  preferredLightTheme: string;
  customThemes: ColorTheme[];
  colorCustomizations: Record<string, ThemeColors>;
  font: keyof typeof appearanceFonts;
  fontSize: number;
  messageSize: number;
  motion: boolean;
}

export const defaultAppearance: Appearance = {
  theme: "system",
  preferredDarkTheme: "mewdex-dark",
  preferredLightTheme: "mewdex-light",
  customThemes: [],
  colorCustomizations: {},
  font: "system",
  fontSize: 14,
  messageSize: 16,
  motion: true,
};

export const maxThemeFileBytes = 256 * 1024;

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function normalizeThemeColor(value: unknown): string | undefined {
  if (
    typeof value !== "string" ||
    !/^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/i.test(value)
  )
    return;
  const hex = value.slice(1).toLowerCase();
  return `#${hex.length < 5 ? [...hex].map((digit) => digit + digit).join("") : hex}`;
}

function readColors(value: unknown, strict = false): ThemeColors {
  if (!isObject(value)) {
    if (strict) throw new Error(t("主题文件的 colors 必须是颜色对象。"));
    return {};
  }
  const colors: ThemeColors = {};
  for (const [id, input] of Object.entries(value)) {
    if (!Object.hasOwn(themeColorRegistry, id)) {
      if (strict)
        throw new Error(
          t("无法识别颜色项 {0}，请检查主题文件或升级 MewDex。", id),
        );
      continue;
    }
    const color = normalizeThemeColor(input);
    if (!color && strict)
      throw new Error(
        t("颜色项 {0} 需要十六进制颜色，例如 #88baff 或 #88baff80。", id),
      );
    if (color) colors[id as ThemeColorId] = color;
  }
  return colors;
}

function readTheme(value: unknown): ColorTheme | undefined {
  if (
    !isObject(value) ||
    typeof value.id !== "string" ||
    !/^user-[\w-]{1,100}$/.test(value.id) ||
    typeof value.name !== "string" ||
    !value.name.trim() ||
    value.name.length > 80 ||
    (value.type !== "dark" && value.type !== "light")
  )
    return;
  return {
    id: value.id,
    name: value.name.trim(),
    type: value.type,
    colors: readColors(value.colors),
  };
}

function boundedSize(
  value: unknown,
  fallback: number,
  min: number,
  max: number,
) {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(min, Math.min(max, Math.round(value)))
    : fallback;
}

export function parseAppearance(raw: string | null): Appearance {
  let input: unknown;
  try {
    input = JSON.parse(raw || "{}");
  } catch {
    input = {};
  }
  const source = isObject(input) ? input : {};
  const customThemes: ColorTheme[] = [];
  if (Array.isArray(source.customThemes)) {
    for (const item of source.customThemes) {
      const theme = readTheme(item);
      if (theme && !customThemes.some((existing) => existing.id === theme.id))
        customThemes.push(theme);
    }
  }
  const themes = [...builtInThemes, ...customThemes];
  const colorCustomizations: Record<string, ThemeColors> = {};
  if (isObject(source.colorCustomizations)) {
    for (const theme of themes) {
      if (Object.hasOwn(source.colorCustomizations, theme.id))
        colorCustomizations[theme.id] = readColors(
          source.colorCustomizations[theme.id],
        );
    }
  }
  // Version 1 stored one accent for every theme, including automatic system mode.
  const legacyAccent = normalizeThemeColor(source.accent);
  if (legacyAccent) {
    for (const theme of builtInThemes.slice(0, 2))
      colorCustomizations[theme.id] = {
        accent: legacyAccent,
        ...colorCustomizations[theme.id],
      };
  }
  const requested =
    source.theme === "dark"
      ? "mewdex-dark"
      : source.theme === "light"
        ? "mewdex-light"
        : source.theme;
  return {
    theme:
      requested === "system" || themes.some((theme) => theme.id === requested)
        ? (requested as string)
        : "system",
    preferredDarkTheme:
      themes.find(
        (theme) =>
          theme.type === "dark" && theme.id === source.preferredDarkTheme,
      )?.id ?? "mewdex-dark",
    preferredLightTheme:
      themes.find(
        (theme) =>
          theme.type === "light" && theme.id === source.preferredLightTheme,
      )?.id ?? "mewdex-light",
    customThemes,
    colorCustomizations,
    font:
      typeof source.font === "string" &&
      Object.hasOwn(appearanceFonts, source.font)
        ? (source.font as Appearance["font"])
        : "system",
    fontSize: boundedSize(source.fontSize, 14, 12, 20),
    messageSize: boundedSize(source.messageSize, 16, 13, 24),
    motion: typeof source.motion === "boolean" ? source.motion : true,
  };
}

export function activeColorTheme(
  appearance: Appearance,
  systemDark: boolean,
): ColorTheme {
  const selected =
    appearance.theme === "system"
      ? systemDark
        ? appearance.preferredDarkTheme
        : appearance.preferredLightTheme
      : appearance.theme;
  return (
    [...builtInThemes, ...appearance.customThemes].find(
      (theme) => theme.id === selected,
    ) ?? builtInThemes[0]
  );
}

export function resolveThemeColors(
  theme: ColorTheme,
  overrides: ThemeColors = {},
): ResolvedThemeColors {
  const colors = {} as ResolvedThemeColors;
  for (const id of Object.keys(themeColorRegistry) as ThemeColorId[])
    colors[id] =
      overrides[id] ?? theme.colors[id] ?? themeColorRegistry[id][theme.type];
  return colors;
}

export interface ThemeFile {
  format: "mewdex-color-theme";
  version: 1;
  name: string;
  type: ThemeType;
  colors: ThemeColors;
}

export function parseThemeFile(text: string): ThemeFile {
  if (new TextEncoder().encode(text).length > maxThemeFileBytes)
    throw new Error(t("主题文件不能超过 256 KiB。"));
  let input: unknown;
  try {
    input = JSON.parse(text.replace(/^\uFEFF/, ""));
  } catch {
    throw new Error(t("主题文件不是有效的 JSON。"));
  }
  if (!isObject(input) || input.format !== "mewdex-color-theme")
    throw new Error(t("请选择 MewDex 导出的 .mewdex-theme.json 主题文件。"));
  if (input.version !== 1)
    throw new Error(t("当前 MewDex 不支持此主题文件版本。"));
  if (
    typeof input.name !== "string" ||
    !input.name.trim() ||
    input.name.length > 80
  )
    throw new Error(t("主题名称需要 1–80 个字符。"));
  if (input.type !== "dark" && input.type !== "light")
    throw new Error(t("主题类型需要是 dark 或 light。"));
  return {
    format: "mewdex-color-theme",
    version: 1,
    name: input.name.trim(),
    type: input.type,
    colors: readColors(input.colors, true),
  };
}

export function exportThemeFile(
  name: string,
  theme: ColorTheme,
  overrides: ThemeColors,
): string {
  const file: ThemeFile = {
    format: "mewdex-color-theme",
    version: 1,
    name: name.trim(),
    type: theme.type,
    colors: resolveThemeColors(theme, overrides),
  };
  return JSON.stringify(parseThemeFile(JSON.stringify(file)), null, 2) + "\n";
}

export function themeFileName(name: string): string {
  const safe = name
    .trim()
    .replace(/[<>:"/\\|?*\u0000-\u001f]/g, "-")
    .replace(/[. ]+$/, "");
  // Leave room for the suffix within a 255-byte filename, including Unicode.
  const stem = [...safe].slice(0, 50).join("");
  return `mewdex-theme-${stem || "custom"}.mewdex-theme.json`;
}

function rgb(color: string): number[] {
  return [1, 3, 5].map((offset) =>
    parseInt(color.slice(offset, offset + 2), 16),
  );
}

function opaqueChannels(color: string, background = "#10131b"): number[] {
  const alpha = color.length === 9 ? parseInt(color.slice(7), 16) / 255 : 1;
  const backdrop = rgb(background);
  return rgb(color).map(
    (channel, index) => channel * alpha + backdrop[index] * (1 - alpha),
  );
}

function luminance(channels: number[]): number {
  const [red, green, blue] = channels.map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return red * 0.2126 + green * 0.7152 + blue * 0.0722;
}

function contrast(foreground: number[], background: number[]): number {
  const first = luminance(foreground);
  const second = luminance(background);
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
}

export function accentColors(colors: ResolvedThemeColors): {
  text: string;
  contrast: string;
} {
  const background = opaqueChannels(colors["panel.background"]);
  const chosen = opaqueChannels(colors.accent, colors["panel.background"]);
  const target = luminance(background) < 0.179 ? 255 : 0;
  let text = colors.accent;
  for (let step = 0; step <= 100; step++) {
    const mixed = chosen.map((channel) =>
      Math.round(channel + (target - channel) * (step / 100)),
    );
    if (contrast(mixed, background) >= 4.5) {
      text = `#${mixed.map((channel) => channel.toString(16).padStart(2, "0")).join("")}`;
      break;
    }
  }
  const button = opaqueChannels(colors.accent, colors["editor.background"]);
  return {
    text,
    contrast:
      contrast([0, 0, 0], button) >= contrast([255, 255, 255], button)
        ? "#000000"
        : "#ffffff",
  };
}
