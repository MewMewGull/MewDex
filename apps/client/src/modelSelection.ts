import { t } from "./localization.ts";
import type { ModelOption, Policy } from "@mewdex/sdk";

const effortNames: Record<string, string> = {
  get none() {
    return t("关闭推理");
  },
  get minimal() {
    return t("极低");
  },
  get low() {
    return t("低");
  },
  get medium() {
    return t("中");
  },
  get high() {
    return t("高");
  },
  get xhigh() {
    return t("很高");
  },
  get max() {
    return t("最高");
  },
  ultra: "Ultra",
};

export const reasoningLabel = (effort: string) => effortNames[effort] ?? effort;

export function selectedModel(
  models: ModelOption[],
  requested?: string,
  inherited?: string,
): ModelOption | undefined {
  const model = requested || inherited;
  return model ? models.find((item) => item.model === model) : undefined;
}

export function selectPolicyModel(
  policy: Policy,
  model: string,
  models: ModelOption[],
  inherited?: string,
): Partial<Policy> {
  const option = selectedModel(models, model, inherited);
  return {
    model: model || undefined,
    reasoningEffort: option?.supportedReasoningEfforts.some(
      (effort) => effort.reasoningEffort === policy.reasoningEffort,
    )
      ? policy.reasoningEffort
      : undefined,
  };
}
