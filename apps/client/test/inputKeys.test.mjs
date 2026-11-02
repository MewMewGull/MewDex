import { test } from "node:test";
import assert from "node:assert/strict";
import {
  isSettingsShortcut,
  keyboardModifierLabel,
  sendKeyHint,
  shouldSendMessage,
} from "../src/inputKeys.ts";

const key = (value = {}) => ({
  key: "Enter",
  code: "Enter",
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  isComposing: false,
  ...value,
});

test("send preferences distinguish multiline editing, modifier submission and touch input without submitting composition", () => {
  for (const mode of ["enter", "modifier-enter"])
    for (const touch of [false, true]) {
      assert.equal(
        shouldSendMessage(key(), mode, touch),
        mode === "enter" && !touch,
      );
      for (const modifier of ["ctrlKey", "metaKey"]) {
        const submit = key({ [modifier]: true });
        assert.equal(shouldSendMessage(submit, mode, touch), true);
        for (const field of ["shiftKey", "altKey", "isComposing"])
          assert.equal(
            shouldSendMessage({ ...submit, [field]: true }, mode, touch),
            false,
          );
      }
      assert.equal(
        shouldSendMessage(key({ shiftKey: true }), mode, touch),
        false,
      );
      assert.equal(shouldSendMessage(key({ key: "a" }), mode, touch), false);
    }
});

test("settings recognizes logical and physical comma while preserving unrelated combinations and composition", () => {
  for (const modifier of ["ctrlKey", "metaKey"])
    for (const value of [
      key({ key: ",", code: "", [modifier]: true }),
      key({ key: "，", code: "Comma", [modifier]: true }),
      key({ key: "Process", code: "Comma", [modifier]: true }),
    ]) {
      assert.equal(isSettingsShortcut(value), true);
      for (const field of ["shiftKey", "altKey", "isComposing"])
        assert.equal(isSettingsShortcut({ ...value, [field]: true }), false);
    }
  assert.equal(isSettingsShortcut(key({ key: ",", code: "Comma" })), false);
  assert.equal(
    isSettingsShortcut(key({ key: ".", code: "Period", ctrlKey: true })),
    false,
  );
});

test("keyboard hints follow the input platform, selected send mode and touch behavior", () => {
  assert.equal(keyboardModifierLabel("Win32"), "Ctrl");
  assert.equal(keyboardModifierLabel("MacIntel"), "⌘");
  assert.equal(
    sendKeyHint("enter", false, "Ctrl"),
    "Enter 发送 · Shift + Enter 换行",
  );
  assert.equal(
    sendKeyHint("modifier-enter", false, "Ctrl"),
    "Ctrl + Enter 发送 · Enter 换行",
  );
  assert.equal(
    sendKeyHint("modifier-enter", false, "⌘"),
    "⌘ + Enter 发送 · Enter 换行",
  );
  assert.equal(sendKeyHint("enter", true, "Ctrl"), "点按发送 · Enter 换行");
  assert.equal(
    sendKeyHint("enter", false, "Ctrl", "追加"),
    "Enter 追加 · Shift + Enter 换行",
  );
  assert.equal(
    sendKeyHint("modifier-enter", false, "⌘", "追加"),
    "⌘ + Enter 追加 · Enter 换行",
  );
  assert.equal(
    sendKeyHint("enter", true, "Ctrl", "追加"),
    "点按追加 · Enter 换行",
  );
});
