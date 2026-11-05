import { test } from "node:test";
import assert from "node:assert/strict";
import {
  detectLanguage,
  getLanguage,
  isLanguage,
  languages,
  selectLanguage,
  subscribeLanguage,
  t,
  translate,
} from "../src/localization.ts";
import { inputAlert } from "../src/inputAlerts.ts";
import { reasoningLabel } from "../src/modelSelection.ts";
import { localServiceStates } from "../src/localDevice.ts";
import { checkCatalogs } from "../../../scripts/check-locales.mjs";

test("display languages are limited to five and match supported system variants", () => {
  assert.deepEqual(
    languages.map((item) => item.id),
    ["zh-CN", "zh-TW", "en", "ja", "ko"],
  );
  for (const [preferences, expected] of [
    [["zh-Hant-HK"], "zh-TW"],
    [["zh_TW"], "zh-TW"],
    [["zh-Hans-SG"], "zh-CN"],
    [["en-GB"], "en"],
    [["ja-JP"], "ja"],
    [["ko-KR"], "ko"],
    [["de-DE", "ja-JP"], "ja"],
    [["fr-FR"], "en"],
    [[], "en"],
  ])
    assert.equal(detectLanguage(preferences), expected);
  assert.equal(isLanguage("fr"), false);
  assert.equal(isLanguage("zh-HK"), false);
  assert.equal(isLanguage("ko"), true);
});

test("every translated message keeps its arguments and application text is complete", () => {
  const result = checkCatalogs();
  assert.ok(result.count > 1500);
  assert.deepEqual(result.errors, []);
  for (const { id } of languages) {
    const value = translate("已用 {0} 标记，共 {1}", id, "7,000", "200,000");
    assert.ok(value.includes("7,000") && value.includes("200,000"));
    assert.ok(!value.includes("{0}"));
    const name = "自定义 <script>{0}</script> 日本語";
    assert.ok(translate("{0} 的对话操作", id, name).includes(name));
    assert.equal(translate("unknown diagnostic", id), "unknown diagnostic");
    assert.equal(translate("__proto__", id), "__proto__");
  }
});

test("switches update shared labels without freezing module-level names", () => {
  const previous = getLanguage();
  let updates = 0;
  const off = subscribeLanguage(() => updates++);
  try {
    selectLanguage("en");
    assert.equal(t("通用"), "General");
    assert.equal(t("最小化时隐藏至托盘"), "Minimize to tray");
    assert.equal(reasoningLabel("low"), "Low");
    assert.equal(localServiceStates.running, "Running");
    const afterFirst = updates;
    selectLanguage("en");
    assert.equal(updates, afterFirst);
    selectLanguage("ja");
    assert.equal(t("通知"), "通知");
    assert.equal(reasoningLabel("low"), "低");
    selectLanguage("ko");
    assert.equal(t("通用"), "일반");
    assert.equal(t("语言"), "언어");
  } finally {
    off();
    selectLanguage(previous);
  }
});

test("new system notifications follow the selected language without changing their route", () => {
  const previous = getLanguage();
  const event = {
    sessionId: "session",
    deviceId: "device",
    kind: "engine.turn/completed",
    payload: {
      turn: { id: "turn", status: "completed", text: "private reply" },
    },
  };
  try {
    selectLanguage("zh-CN");
    const original = inputAlert(event);
    for (const { id } of languages) {
      selectLanguage(id);
      const alert = inputAlert(event);
      assert.equal(alert.body, translate("一轮对话已完成。", id));
      assert.equal(alert.key, original.key);
      assert.equal(alert.sessionId, "session");
      assert.equal(alert.deviceId, "device");
      assert.ok(!JSON.stringify(alert).includes("private reply"));
    }
  } finally {
    selectLanguage(previous);
  }
});
