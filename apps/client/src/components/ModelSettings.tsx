import { useState } from "react";
import { useModelCatalog } from "../useModelCatalog";
import { RefreshCw } from "lucide-react";
import type { MewDexClient, Policy } from "@mewdex/sdk";
import {
  reasoningLabel,
  selectedModel,
  selectPolicyModel,
} from "../modelSelection";

export function ModelSettings({
  client,
  deviceId,
  projectId,
  sessionId,
  inheritedModel,
  catalogSupported,
  reasoningSupported,
  online,
  disabled,
  value,
  onChange,
}: {
  client: MewDexClient;
  deviceId: string;
  projectId: string;
  sessionId?: string;
  inheritedModel?: string;
  catalogSupported: boolean;
  reasoningSupported: boolean;
  online: boolean;
  disabled: boolean;
  value: Policy;
  onChange: (policy: Partial<Policy>) => void;
}) {
  const [manual, setManual] = useState(false);
  const { catalog, models } = useModelCatalog(
    client,
    { deviceId, projectId, sessionId },
    online && catalogSupported,
  );
  const model = selectedModel(models, value.model, inheritedModel);
  const custom =
    manual ||
    (!!value.model && !models.some((item) => item.model === value.model));
  const knownEffort = model?.supportedReasoningEfforts.find(
    (item) => item.reasoningEffort === value.reasoningEffort,
  );
  function choose(model: string) {
    onChange(selectPolicyModel(value, model, models, inheritedModel));
  }
  const defaultLabel = inheritedModel
    ? `继承 ${inheritedModel}`
    : "执行端默认模型";
  return (
    <div className="model-policy-controls">
      <div className="model-policy-heading">
        <span>模型与推理</span>
        {catalogSupported && (
          <button
            type="button"
            className="model-catalog-action"
            disabled={!online || catalog.isFetching}
            onClick={() => void catalog.refetch()}
            aria-label="刷新执行端模型目录"
          >
            <RefreshCw size={12} />
            {catalog.isFetching ? "读取中…" : "刷新目录"}
          </button>
        )}
      </div>
      <div className="model-policy-row">
        <label>
          模型
          {catalogSupported ? (
            <select
              aria-label="选择模型"
              disabled={disabled}
              value={custom ? ":custom" : (value.model ?? "")}
              onChange={(event) => {
                const selected = event.target.value;
                setManual(selected === ":custom");
                if (selected !== ":custom") choose(selected);
              }}
            >
              <option value="">{defaultLabel}</option>
              {models.map((item) => (
                <option key={item.id} value={item.model}>
                  {item.displayName || item.model}
                  {item.isDefault ? " · 推荐" : ""}
                </option>
              ))}
              <option value=":custom">自定义模型…</option>
            </select>
          ) : (
            <input
              aria-label="模型名称"
              disabled={disabled}
              value={value.model ?? ""}
              maxLength={200}
              onChange={(event) =>
                onChange({ model: event.target.value || undefined })
              }
              placeholder={defaultLabel}
            />
          )}
        </label>
        <label>
          推理强度
          {model && reasoningSupported ? (
            <select
              aria-label="选择推理强度"
              disabled={disabled}
              value={value.reasoningEffort ?? ""}
              onChange={(event) =>
                onChange({ reasoningEffort: event.target.value || undefined })
              }
            >
              <option value="">由执行端决定</option>
              {value.reasoningEffort && !knownEffort && (
                <option value={value.reasoningEffort}>
                  当前设置 · {reasoningLabel(value.reasoningEffort)}
                </option>
              )}
              {model.supportedReasoningEfforts.map((item) => (
                <option key={item.reasoningEffort} value={item.reasoningEffort}>
                  {reasoningLabel(item.reasoningEffort)}
                </option>
              ))}
            </select>
          ) : (
            <input
              aria-label="推理强度"
              disabled={
                disabled ||
                !reasoningSupported ||
                (!value.model && !inheritedModel && !value.reasoningEffort)
              }
              value={value.reasoningEffort ?? ""}
              maxLength={64}
              onChange={(event) =>
                onChange({ reasoningEffort: event.target.value || undefined })
              }
              placeholder={
                reasoningSupported
                  ? !value.model && !inheritedModel
                    ? "选择模型后查看支持的强度"
                    : "留空由执行端决定"
                  : "此设备暂不支持设置"
              }
            />
          )}
        </label>
      </div>
      {catalogSupported && custom && (
        <label>
          自定义模型名称
          <input
            aria-label="自定义模型名称"
            disabled={disabled}
            value={value.model ?? ""}
            maxLength={200}
            placeholder="输入执行端使用的模型标识"
            onChange={(event) => choose(event.target.value)}
          />
        </label>
      )}
      {model && (
        <p className="policy-note">
          {model.description}
          {model.defaultReasoningEffort
            ? ` · 建议强度：${reasoningLabel(model.defaultReasoningEffort)}`
            : ""}
          {knownEffort?.description ? ` · ${knownEffort.description}` : ""}
        </p>
      )}
      {!model && !value.model && reasoningSupported && (
        <p className="policy-note">
          继承模型时由执行端决定推理强度；选择具体模型后可查看其支持的选项。
        </p>
      )}
      {catalogSupported && (
        <div className="model-catalog-status">
          <p className="policy-note">
            {!online
              ? "设备离线，可先修改设置。"
              : catalog.isError
                ? "模型目录暂时不可读，可保留设置或填写自定义模型。"
                : "目录由当前执行端提供，实际可用性取决于该设备账号。"}
          </p>
          {catalog.hasNextPage && (
            <button
              type="button"
              className="model-catalog-action"
              disabled={!online || catalog.isFetching}
              onClick={() => void catalog.fetchNextPage()}
            >
              {catalog.isFetchingNextPage ? "读取中…" : "更多模型"}
            </button>
          )}
          {catalog.isError && (
            <details>
              <summary>读取详情</summary>
              <p>{catalog.error.message}</p>
            </details>
          )}
        </div>
      )}
    </div>
  );
}
