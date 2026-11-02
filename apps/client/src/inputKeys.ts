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
  action = "发送",
) {
  if (touch) return `点按${action} · Enter 换行`;
  return mode === "modifier-enter"
    ? `${modifier} + Enter ${action} · Enter 换行`
    : `Enter ${action} · Shift + Enter 换行`;
}
