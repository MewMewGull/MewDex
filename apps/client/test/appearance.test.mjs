import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  accentColors,
  activeColorTheme,
  builtInThemes,
  defaultAppearance,
  exportThemeFile,
  maxThemeFileBytes,
  normalizeThemeColor,
  parseAppearance,
  parseThemeFile,
  resolveThemeColors,
  themeColorRegistry,
  themeFileName,
} from "../src/appearance.ts";

test("legacy appearance retains theme, fonts, sizes, motion and accent", () => {
  const migrated = parseAppearance(
    JSON.stringify({
      theme: "light",
      font: "mono",
      fontSize: 18,
      messageSize: 22,
      accent: "#74A7ED",
      motion: false,
    }),
  );
  assert.equal(migrated.theme, "mewdex-light");
  assert.equal(migrated.font, "mono");
  assert.equal(migrated.fontSize, 18);
  assert.equal(migrated.messageSize, 22);
  assert.equal(migrated.motion, false);
  assert.deepEqual(migrated.colorCustomizations, {
    "mewdex-dark": { accent: "#74a7ed" },
    "mewdex-light": { accent: "#74a7ed" },
  });
  assert.equal(parseAppearance('{"theme":"dark"}').theme, "mewdex-dark");
  assert.equal(parseAppearance('{"theme":"system"}').theme, "system");
});

test("corrupt and unsupported stored settings fall back field by field", () => {
  for (const raw of [null, "{", "null", "[]", '"bad"'])
    assert.deepEqual(parseAppearance(raw), defaultAppearance);
  const result = parseAppearance(
    JSON.stringify({
      theme: "missing",
      font: "toString",
      motion: "yes",
      fontSize: 999,
      messageSize: -1,
      customThemes: [
        null,
        { id: "mewdex-dark", name: "collision", type: "dark" },
      ],
      colorCustomizations: {
        "mewdex-dark": {
          foreground: "url(evil)",
          accent: "#f80",
          missing: "#fff",
        },
      },
    }),
  );
  assert.equal(result.theme, "system");
  assert.equal(result.font, "system");
  assert.equal(result.fontSize, 20);
  assert.equal(result.messageSize, 13);
  assert.equal(result.motion, true);
  assert.deepEqual(result.customThemes, []);
  assert.deepEqual(result.colorCustomizations["mewdex-dark"], {
    accent: "#ff8800",
  });
});

test("system mode selects preferred themes and keeps their overrides isolated", () => {
  const settings = parseAppearance(
    JSON.stringify({
      preferredDarkTheme: "midnight",
      preferredLightTheme: "warm-paper",
      colorCustomizations: {
        midnight: { accent: "#123456" },
        "warm-paper": { accent: "#654321" },
      },
    }),
  );
  const dark = activeColorTheme(settings, true);
  const light = activeColorTheme(settings, false);
  assert.equal(dark.id, "midnight");
  assert.equal(light.id, "warm-paper");
  assert.equal(
    resolveThemeColors(dark, settings.colorCustomizations[dark.id]).accent,
    "#123456",
  );
  assert.equal(
    resolveThemeColors(light, settings.colorCustomizations[light.id]).accent,
    "#654321",
  );
  settings.theme = "warm-paper";
  assert.equal(activeColorTheme(settings, true).id, "warm-paper");
});

test("invalid system preferences and removed custom themes fall back to valid built-ins", () => {
  const result = parseAppearance(
    JSON.stringify({
      theme: "user-removed",
      preferredDarkTheme: "mewdex-light",
      preferredLightTheme: "missing",
      colorCustomizations: { "user-removed": { accent: "#123456" } },
    }),
  );
  assert.equal(activeColorTheme(result, true).id, "mewdex-dark");
  assert.equal(activeColorTheme(result, false).id, "mewdex-light");
  assert.deepEqual(result.colorCustomizations, {});
});

test("color resolution layers defaults, theme colors and user colors", () => {
  const theme = {
    id: "user-test",
    name: "Test",
    type: "light",
    colors: { accent: "#123456" },
  };
  const colors = resolveThemeColors(theme, { accent: "#abcdef" });
  assert.equal(colors.accent, "#abcdef");
  assert.equal(
    colors["editor.background"],
    themeColorRegistry["editor.background"].light,
  );
  assert.deepEqual(Object.keys(colors), Object.keys(themeColorRegistry));
  assert.equal(resolveThemeColors(theme).accent, "#123456");
});

test("shared themes round-trip every resolved color independently of the source theme", () => {
  for (const theme of builtInThemes) {
    const overrides = { accent: "#11223380", "widget.border": "#123456" };
    const text = exportThemeFile("  我的主题  ", theme, overrides);
    const file = parseThemeFile(text);
    assert.equal(file.name, "我的主题");
    assert.equal(file.type, theme.type);
    assert.deepEqual(
      resolveThemeColors({ ...file, id: "user-imported" }),
      resolveThemeColors(theme, overrides),
    );
    assert.equal(
      Object.keys(file.colors).length,
      Object.keys(themeColorRegistry).length,
    );
    assert.equal(text.includes("fontSize"), false);
    assert.equal(text.includes("customThemes"), false);
  }
});

test("import accepts a BOM and all four hexadecimal formats", () => {
  assert.equal(normalizeThemeColor("#AbC"), "#aabbcc");
  assert.equal(normalizeThemeColor("#AbC8"), "#aabbcc88");
  assert.equal(normalizeThemeColor("#AABBCC"), "#aabbcc");
  assert.equal(normalizeThemeColor("#AABBCC88"), "#aabbcc88");
  const file = parseThemeFile(
    "\uFEFF" +
      JSON.stringify({
        format: "mewdex-color-theme",
        version: 1,
        name: "Short",
        type: "dark",
        colors: { accent: "#abc" },
      }),
  );
  assert.deepEqual(file.colors, { accent: "#aabbcc" });
  for (const value of [
    "red",
    "var(--x)",
    "#12345",
    "#1234567",
    "#ggg",
    "#fff; color:red",
    null,
    1,
  ])
    assert.equal(normalizeThemeColor(value), undefined);
});

test("import rejects invalid versions, names, color IDs, values and oversized files", () => {
  const valid = {
    format: "mewdex-color-theme",
    version: 1,
    name: "Test",
    type: "dark",
    colors: {},
  };
  for (const patch of [
    { format: "vscode" },
    { version: 2 },
    { name: " " },
    { name: "a".repeat(81) },
    { type: "system" },
    { colors: [] },
    { colors: null },
    { colors: { "unknown.color": "#fff" } },
    { colors: { accent: "url(evil)" } },
    { colors: JSON.parse('{"__proto__":"#fff"}') },
  ])
    assert.throws(() => parseThemeFile(JSON.stringify({ ...valid, ...patch })));
  for (const text of ["{", "[]", "null", "a".repeat(maxThemeFileBytes + 1)])
    assert.throws(() => parseThemeFile(text));
  assert.throws(() => exportThemeFile("", builtInThemes[0], {}));
});

test("custom themes and overrides survive reload without replacing built-ins", () => {
  const custom = {
    id: "user-123",
    name: "Shared",
    type: "light",
    colors: { accent: "#aabbcc" },
  };
  const first = parseAppearance(
    JSON.stringify({
      theme: custom.id,
      customThemes: [custom, custom],
      preferredLightTheme: custom.id,
      colorCustomizations: { [custom.id]: { foreground: "#112233" } },
    }),
  );
  const reloaded = parseAppearance(JSON.stringify(first));
  assert.deepEqual(reloaded, first);
  assert.equal(reloaded.customThemes.length, 1);
  assert.equal(activeColorTheme(reloaded, true).id, custom.id);
  assert.equal(
    resolveThemeColors(
      reloaded.customThemes[0],
      reloaded.colorCustomizations[custom.id],
    ).foreground,
    "#112233",
  );
  assert.equal(builtInThemes[0].id, "mewdex-dark");
});

test("theme file names are portable across Windows and Unix", () => {
  assert.equal(
    themeFileName("CON:/a\\b?. "),
    "mewdex-theme-CON--a-b-.mewdex-theme.json",
  );
  assert.equal(themeFileName(" "), "mewdex-theme-custom.mewdex-theme.json");
  assert.equal(
    themeFileName("我的配色"),
    "mewdex-theme-我的配色.mewdex-theme.json",
  );
  assert.ok(
    new TextEncoder().encode(themeFileName("配".repeat(80))).length < 255,
  );
});

test("each registered color reaches the stylesheet and legacy literal tokens are removed", () => {
  const files = [
    "style.css",
    "desktop-workspace.css",
    "desktop-titlebar.css",
    "appearance.css",
    "theme-palette.css",
    "messaging.css",
    "components/app-updates.css",
  ];
  const css = files
    .map((file) =>
      readFileSync(new URL(`../src/${file}`, import.meta.url), "utf8"),
    )
    .join("\n");
  for (const id of Object.keys(themeColorRegistry))
    assert.ok(css.includes(`var(--mewdex-${id.replaceAll(".", "-")})`), id);
  assert.equal(css.includes("--tone-"), false);
});

test("accent text and button text remain defined for very dark, bright and transparent choices", () => {
  assert.equal(
    accentColors(resolveThemeColors(builtInThemes[0], { accent: "#777777" }))
      .contrast,
    "#000000",
  );
  for (const theme of builtInThemes) {
    for (const accent of [
      "#000000",
      "#ffffff",
      "#00ff00",
      "#00000000",
      "#fff00080",
    ]) {
      const resolved = accentColors(resolveThemeColors(theme, { accent }));
      assert.match(resolved.text, /^#[\da-f]{6}$/);
      assert.match(resolved.contrast, /^#[\da-f]{6}$/);
    }
  }
});
