import { t } from "../localization.ts";
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
  const runtime = catalog.data?.pages[0]?.runtime;
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
    ? t("继承 {0}", inheritedModel)
    : t("Hub 默认模型");
  return (
    <div className="model-policy-controls">
      <div className="model-policy-heading">
        <span>{t("模型与推理")}</span>
        {catalogSupported && (
          <button
            type="button"
            className="model-catalog-action"
            disabled={!online || catalog.isFetching}
            onClick={() => void catalog.refetch()}
            aria-label={t("刷新 Hub 模型目录")}
          >
            <RefreshCw size={12} />
            {catalog.isFetching ? t("读取中…") : t("刷新目录")}
          </button>
        )}
      </div>
      <div className="model-policy-row">
        <label>
          {t("模型")}
          {catalogSupported ? (
            <select
              aria-label={t("选择模型")}
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
                  {item.isDefault ? t(" · 推荐") : ""}
                </option>
              ))}
              <option value=":custom">{t("自定义模型…")}</option>
            </select>
          ) : (
            <input
              aria-label={t("模型名称")}
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
          {t("推理强度")}
          {model && reasoningSupported ? (
            <select
              aria-label={t("选择推理强度")}
              disabled={disabled}
              value={value.reasoningEffort ?? ""}
              onChange={(event) =>
                onChange({ reasoningEffort: event.target.value || undefined })
              }
            >
              <option value="">{t("留空由 Hub 决定")}</option>
              {value.reasoningEffort && !knownEffort && (
                <option value={value.reasoningEffort}>
                  {t("当前设置 · {0}", reasoningLabel(value.reasoningEffort))}
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
              aria-label={t("推理强度")}
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
                    ? t("选择模型后查看支持的强度")
                    : t("留空由 Hub 决定")
                  : t("此设备暂不支持设置")
              }
            />
          )}
        </label>
      </div>
      {catalogSupported && custom && (
        <label>
          {t("自定义模型名称")}
          <input
            aria-label={t("自定义模型名称")}
            disabled={disabled}
            value={value.model ?? ""}
            maxLength={200}
            placeholder={t("输入 Hub 使用的模型标识")}
            onChange={(event) => choose(event.target.value)}
          />
        </label>
      )}
      {model && (
        <p className="policy-note">
          {model.description}
          {model.defaultReasoningEffort
            ? t(
                " · 建议强度：{0}",
                reasoningLabel(model.defaultReasoningEffort),
              )
            : ""}
          {knownEffort?.description ? ` · ${knownEffort.description}` : ""}
        </p>
      )}
      {!model && !value.model && reasoningSupported && (
        <p className="policy-note">
          {t(
            "继承模型时使用 Hub 的默认思考强度。选择具体模型后可查看其支持的选项。",
          )}
        </p>
      )}
      {catalogSupported && (
        <div className="model-catalog-status">
          <p className="policy-note">
            {!online
              ? t("设备离线，可先修改设置。")
              : catalog.isError
                ? t("模型目录暂时不可读，可保留设置或填写自定义模型。")
                : t(
                    "模型目录由 Hub 提供，实际可用性由对应账号的模型回合验证。",
                  )}
          </p>
          {runtime && (
            <p className="policy-note">
              {t("Hub 运行时")} · {runtime.adapter} {runtime.version}
            </p>
          )}
          {catalog.hasNextPage && (
            <button
              type="button"
              className="model-catalog-action"
              disabled={!online || catalog.isFetching}
              onClick={() => void catalog.fetchNextPage()}
            >
              {catalog.isFetchingNextPage ? t("读取中…") : t("更多模型")}
            </button>
          )}
          {catalog.isError && (
            <details>
              <summary>{t("读取详情")}</summary>
              <p>{catalog.error.message}</p>
            </details>
          )}
        </div>
      )}
    </div>
  );
}
