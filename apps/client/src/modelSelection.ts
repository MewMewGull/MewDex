import type { ModelOption, Policy } from "@mewdex/sdk";

const effortNames: Record<string, string> = {
  none: "关闭推理",
  minimal: "极低",
  low: "低",
  medium: "中",
  high: "高",
  xhigh: "很高",
  max: "最高",
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
