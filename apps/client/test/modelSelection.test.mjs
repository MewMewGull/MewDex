import { test } from "node:test";
import assert from "node:assert/strict";
import {
  selectedModel,
  selectPolicyModel,
  reasoningLabel,
} from "../src/modelSelection.ts";

test("selection preserves engine and custom defaults and accepts catalog-provided effort names", () => {
  const model = (name, efforts, isDefault = false) => ({
    id: `picker-${name}`,
    model: name,
    isDefault,
    supportedReasoningEfforts: efforts.map((reasoningEffort) => ({
      reasoningEffort,
    })),
  });
  const models = [
    model("recommended", ["low", "high"], true),
    model("custom-provider", ["high", "future-effort"]),
  ];
  assert.equal(selectedModel(models), undefined);
  assert.equal(
    selectedModel(models, "", "custom-provider")?.model,
    "custom-provider",
  );
  assert.equal(selectedModel(models, "unlisted-custom"), undefined);
  assert.equal(selectedModel(models, "", "unlisted-configured"), undefined);
  const policy = { model: "recommended", reasoningEffort: "high" };
  assert.equal(
    selectPolicyModel(policy, "custom-provider", models).reasoningEffort,
    "high",
  );
  assert.equal(
    selectPolicyModel(
      { ...policy, reasoningEffort: "low" },
      "custom-provider",
      models,
    ).reasoningEffort,
    undefined,
  );
  assert.equal(
    selectPolicyModel(policy, "unlisted-custom", models).model,
    "unlisted-custom",
  );
  assert.equal(reasoningLabel("future-effort"), "future-effort");
});
