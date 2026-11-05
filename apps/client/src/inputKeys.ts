import { t } from "./localization.ts";
export type SendKey = "enter" | "modifier-enter";

type KeyInput = Pick<
  KeyboardEvent,
  "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "isComposing"
>;

export function shouldSendMessage(
  event: KeyInput,
  mode: SendKey,
  touch: boolean,
) {
  if (
    event.key !== "Enter" ||
    event.shiftKey ||
    event.altKey ||
    event.isComposing
  )
    return false;
  return event.ctrlKey || event.metaKey || (!touch && mode === "enter");
}

export function isSettingsShortcut(
  event: KeyInput & Pick<KeyboardEvent, "code">,
) {
  return (
    (event.ctrlKey || event.metaKey) &&
    !event.altKey &&
    !event.shiftKey &&
    !event.isComposing &&
    (event.key === "," || event.key === "，" || event.code === "Comma")
  );
}

export function keyboardModifierLabel(platform = navigator.platform) {
  return /Mac|iPhone|iPad|iPod/.test(platform) ? "⌘" : "Ctrl";
}

export function sendKeyHint(
  mode: SendKey,
  touch: boolean,
  modifier = keyboardModifierLabel(),
  action = t("发送"),
) {
  if (touch) return t("点按{0} · Enter 换行", action);
  return mode === "modifier-enter"
    ? t("{0} + Enter {1} · Enter 换行", modifier, action)
    : t("Enter {0} · Shift + Enter 换行", action);
}
