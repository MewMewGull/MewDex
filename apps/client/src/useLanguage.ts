import { useSyncExternalStore } from "react";
import { isTauri, invoke } from "@tauri-apps/api/core";
import {
  detectLanguage,
  getLanguage,
  isLanguage,
  languages,
  selectLanguage,
  subscribeLanguage,
  type Language,
} from "./localization";
import { inputCapabilities } from "./localDevice";

const storageKey = "mewdex.language.v1";
const listeners = new Set<() => void>();
let persistenceError = "";
let nativeError = "";
let snapshot = { language: getLanguage(), error: "" };
let nativeSync = Promise.resolve();

function publish() {
  snapshot = {
    language: getLanguage(),
    error: persistenceError || nativeError,
  };
  for (const listener of listeners) listener();
}
function apply() {
  const language = getLanguage();
  document.documentElement.lang = languages.find(
    (item) => item.id === language,
  )!.lang;
  publish();
  if (isTauri()) {
    nativeSync = nativeSync
      .then(async () => {
        if ((await inputCapabilities()).desktopPreferences)
          await invoke("desktop_language", { language });
        if (getLanguage() === language && nativeError) {
          nativeError = "";
          publish();
        }
      })
      .catch(() => {
        if (getLanguage() === language) {
          nativeError = "托盘语言未能更新，请重新选择语言。";
          publish();
        }
      });
  }
}

export function initializeLanguage() {
  let initial = detectLanguage(navigator.languages);
  try {
    const saved = localStorage.getItem(storageKey);
    if (isLanguage(saved)) initial = saved;
  } catch {
    // Browsers with restricted storage can still switch languages in memory.
  }
  selectLanguage(initial);
  subscribeLanguage(apply);
  window.addEventListener("storage", (event) => {
    if (event.key !== storageKey && event.key !== null) return;
    persistenceError = "";
    nativeError = "";
    selectLanguage(
      isLanguage(event.newValue)
        ? event.newValue
        : detectLanguage(navigator.languages),
    );
  });
  apply();
}

export function setLanguage(next: Language) {
  if (!isLanguage(next)) return;
  persistenceError = "";
  nativeError = "";
  try {
    localStorage.setItem(storageKey, next);
  } catch {
    persistenceError = "语言设置未保存，关闭工作台后可能恢复原设置。";
  }
  if (next === getLanguage()) apply();
  else selectLanguage(next);
}
export function useLanguage() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    () => snapshot,
  );
}
