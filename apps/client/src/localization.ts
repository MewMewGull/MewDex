import en from "./locales/en.json" with { type: "json" };
import zhTW from "./locales/zh-TW.json" with { type: "json" };
import ja from "./locales/ja.json" with { type: "json" };
import ko from "./locales/ko.json" with { type: "json" };

export const languages = [
  { id: "zh-CN", name: "简体中文", lang: "zh-Hans" },
  { id: "zh-TW", name: "繁體中文", lang: "zh-Hant" },
  { id: "en", name: "English", lang: "en" },
  { id: "ja", name: "日本語", lang: "ja" },
  { id: "ko", name: "한국어", lang: "ko" },
] as const;
export type Language = (typeof languages)[number]["id"];
const catalogs: Record<Exclude<Language, "zh-CN">, Record<string, string>> = {
  "zh-TW": zhTW,
  en,
  ja,
  ko,
};
let language: Language = "zh-CN";
const listeners = new Set<() => void>();

export function isLanguage(value: unknown): value is Language {
  return languages.some((item) => item.id === value);
}

export function detectLanguage(preferences: readonly string[]): Language {
  for (const preference of preferences) {
    const tag = preference.replaceAll("_", "-").toLowerCase();
    if (tag === "zh" || tag.startsWith("zh-"))
      return /(?:hant|tw|hk|mo)(?:-|$)/.test(tag) ? "zh-TW" : "zh-CN";
    for (const locale of ["en", "ja", "ko"] as const)
      if (tag === locale || tag.startsWith(`${locale}-`)) return locale;
  }
  return "en";
}

export const getLanguage = () => language;
export function selectLanguage(next: Language) {
  if (!isLanguage(next) || next === language) return;
  language = next;
  for (const listener of listeners) listener();
}
export function subscribeLanguage(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// Only application-owned copy goes through this function. Messages, terminal
// output, file contents, names, and other user data stay in their original form.
export function translate(
  source: string,
  locale: Language,
  ...values: unknown[]
): string {
  const catalog = locale === "zh-CN" ? null : catalogs[locale];
  const message =
    catalog && Object.hasOwn(catalog, source) ? catalog[source] : source;
  return message.replace(/\{(\d+)\}/g, (placeholder, index: string) =>
    Number(index) < values.length
      ? values[Number(index)] == null ||
        typeof values[Number(index)] === "boolean"
        ? ""
        : String(values[Number(index)])
      : placeholder,
  );
}
export const t = (source: string, ...values: unknown[]): string =>
  translate(source, language, ...values);
