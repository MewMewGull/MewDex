import { t } from "../localization.ts";
import { useEffect, type ReactNode } from "react";
import {
  Check,
  ChevronDown,
  Code2,
  FlaskConical,
  Globe,
  MessageSquareText,
  ShieldCheck,
  Eye,
  SlidersHorizontal,
} from "lucide-react";
import type { Device, MewDexClient, Policy, Session } from "@mewdex/sdk";
import { useModelCatalog } from "../useModelCatalog";
import { ContextUsageIndicator } from "./ContextUsageIndicator";
import {
  reasoningLabel,
  selectedModel,
  selectPolicyModel,
} from "../modelSelection";

function closeMenu(button: HTMLElement) {
  const details = button.closest("details");
  if (details) {
    details.open = false;
    details.querySelector("summary")?.focus({ preventScroll: true });
  }
}
function ToolbarMenu({
  label,
  icon,
  children,
  disabled = false,
  className = "",
}: {
  label: string;
  icon: ReactNode;
  children: ReactNode;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <details className={`composer-menu ${className}`} data-composer-menu>
      <summary
        aria-label={label}
        title={label}
        aria-disabled={disabled}
        onClick={(event) => {
          if (disabled) event.preventDefault();
        }}
      >
        {icon}
      </summary>
      <div className="composer-menu-content">{children}</div>
    </details>
  );
}
export function ComposerControls({
  client,
  device,
  projectId,
  session,
  value,
  onChange,
  disabled,
  connected,
  dirty,
  onSettings,
}: {
  client: MewDexClient;
  device?: Device;
  projectId: string;
  session?: Session;
  value: Policy;
  onChange: (patch: Partial<Policy>) => void;
  disabled: boolean;
  connected: boolean;
  dirty: boolean;
  onSettings: () => void;
}) {
  const { catalog, models } = useModelCatalog(
    client,
    { deviceId: device?.id ?? "", projectId, sessionId: session?.id },
    connected &&
      !!device?.online &&
      !!device.capabilities?.includes("model-catalog"),
  );
  const inherited =
    session?.defaultModel || models.find((item) => item.isDefault)?.model;
  const model = selectedModel(models, value.model, inherited);
  const actualModel = value.model || inherited || session?.model;
  const modelLabel = model?.displayName || actualModel || t("默认模型");
  const effort = value.reasoningEffort || model?.defaultReasoningEffort;
  const effortLabel = effort ? reasoningLabel(effort) : t("默认强度");
  const access = value.accessMode || "workspace";
  const accessLabel =
    access === "read-only"
      ? t("只读访问")
      : access === "workspace-network"
        ? t("工作区与网络")
        : t("工作区权限");
  const strategy = value.noTests ? "no-tests" : value.mode;
  const strategies = [
    { id: "edit", label: t("直接修改") },
    { id: "focused", label: t("局部验证") },
    { id: "full", label: t("完整验证") },
    { id: "no-tests", label: t("不运行验证") },
  ];
  const strategyLabel =
    strategies.find((item) => item.id === strategy)?.label || t("任务策略");
  const running = session?.state === "running";
  const applicationHint =
    running && dirty ? t("；修改用于后续回合，当前执行继续使用原设置") : "";
  useEffect(() => {
    const pointer = (event: PointerEvent) => {
      for (const menu of document.querySelectorAll<HTMLDetailsElement>(
        "details[data-composer-menu][open]",
      ))
        if (!menu.contains(event.target as Node)) menu.open = false;
    };
    const keyboard = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      const menu = (event.target as Element)?.closest<HTMLDetailsElement>(
        "details[data-composer-menu][open]",
      );
      if (menu) {
        menu.open = false;
        menu.querySelector("summary")?.focus();
        event.stopPropagation();
      }
    };
    document.addEventListener("pointerdown", pointer);
    document.addEventListener("keydown", keyboard);
    return () => {
      document.removeEventListener("pointerdown", pointer);
      document.removeEventListener("keydown", keyboard);
    };
  }, []);
  return (
    <div className="composer-inline-controls">
      <ToolbarMenu
        label={t("访问权限：{0}{1}", accessLabel, applicationHint)}
        className="composer-labeled-menu composer-access-menu"
        disabled={disabled || !device?.capabilities?.includes("access-modes")}
        icon={
          <>
            {access === "read-only" ? (
              <Eye size={16} />
            ) : access === "workspace-network" ? (
              <Globe size={16} />
            ) : (
              <ShieldCheck size={16} />
            )}
            <span className="composer-control-label">{accessLabel}</span>
          </>
        }
      >
        <strong>{t("访问权限")}</strong>
        {[
          { id: "read-only", label: t("只读访问"), note: t("查看项目文件") },
          {
            id: "workspace",
            label: t("工作区权限"),
            note: t("可修改项目，额外访问需批准"),
          },
          {
            id: "workspace-network",
            label: t("工作区与网络"),
            note: t("允许项目读写及网络访问"),
          },
        ].map((item) => (
          <button
            key={item.id}
            aria-pressed={access === item.id}
            onClick={(event) => {
              onChange({
                accessMode: item.id === "workspace" ? undefined : item.id,
              });
              closeMenu(event.currentTarget);
            }}
          >
            <span>
              <b>{item.label}</b>
              <small>{item.note}</small>
            </span>
            {access === item.id && <Check size={14} />}
          </button>
        ))}
      </ToolbarMenu>
      <ToolbarMenu
        label={t(
          "任务策略：{0}{1}{2}",
          value.collaborationMode === "plan" ? t("方案讨论 · ") : "",
          strategyLabel,
          applicationHint,
        )}
        className="composer-labeled-menu"
        disabled={disabled}
        icon={
          <>
            {value.collaborationMode === "plan" ? (
              <MessageSquareText size={16} />
            ) : strategy === "edit" ? (
              <Code2 size={16} />
            ) : (
              <FlaskConical size={16} />
            )}
            <span className="composer-control-label">
              {value.collaborationMode === "plan"
                ? t("方案讨论")
                : strategyLabel}
            </span>
          </>
        }
      >
        <strong>{t("任务策略")}</strong>
        {strategies.map((item) => (
          <button
            key={item.id}
            aria-pressed={strategy === item.id}
            onClick={(event) => {
              onChange(
                item.id === "no-tests"
                  ? { noTests: true }
                  : { mode: item.id, noTests: false },
              );
              closeMenu(event.currentTarget);
            }}
          >
            <span>{item.label}</span>
            {strategy === item.id && <Check size={14} />}
          </button>
        ))}
        <button
          onClick={(event) => {
            closeMenu(event.currentTarget);
            onSettings();
          }}
        >
          <SlidersHorizontal size={14} />
          {t("更多任务设置…")}
        </button>
      </ToolbarMenu>
      <span className="composer-toolbar-space" />
      <ContextUsageIndicator session={session} />
      <ToolbarMenu
        label={t(
          "模型与思考强度：{0} · {1}{2}",
          modelLabel,
          effortLabel,
          applicationHint,
        )}
        disabled={disabled}
        className="composer-model-menu"
        icon={
          <>
            <span className="composer-model-name">{modelLabel}</span>
            <span className="composer-effort-label">{effortLabel}</span>
            <ChevronDown size={12} />
          </>
        }
      >
        <strong>{t("模型与思考强度")}</strong>
        <label>
          {t("模型")}
          <select
            aria-label={t("模型")}
            value={value.model ?? ""}
            disabled={disabled}
            onChange={(event) =>
              onChange(
                selectPolicyModel(value, event.target.value, models, inherited),
              )
            }
          >
            <option value="">{inherited || t("Hub 默认模型")}</option>
            {value.model &&
              !models.some((item) => item.model === value.model) && (
                <option value={value.model}>{value.model}</option>
              )}
            {models.map((item) => (
              <option key={item.id} value={item.model}>
                {item.displayName || item.model}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t("思考强度")}
          <select
            aria-label={t("思考强度")}
            value={value.reasoningEffort ?? ""}
            disabled={
              disabled || !device?.capabilities?.includes("reasoning-policy")
            }
            onChange={(event) =>
              onChange({ reasoningEffort: event.target.value || undefined })
            }
          >
            <option value="">
              {model?.defaultReasoningEffort
                ? t("{0}（默认）", reasoningLabel(model.defaultReasoningEffort))
                : t("Hub 默认强度")}
            </option>
            {value.reasoningEffort &&
              !model?.supportedReasoningEfforts.some(
                (item) => item.reasoningEffort === value.reasoningEffort,
              ) && (
                <option value={value.reasoningEffort}>
                  {reasoningLabel(value.reasoningEffort)}
                </option>
              )}
            {model?.supportedReasoningEfforts.map((item) => (
              <option key={item.reasoningEffort} value={item.reasoningEffort}>
                {reasoningLabel(item.reasoningEffort)}
              </option>
            ))}
          </select>
        </label>
        {catalog.hasNextPage && (
          <button
            disabled={catalog.isFetching}
            onClick={() => void catalog.fetchNextPage()}
          >
            {t("加载更多模型")}
          </button>
        )}
        {catalog.error && (
          <small>{t("模型目录暂不可读，可在任务设置中填写模型。")}</small>
        )}
      </ToolbarMenu>
    </div>
  );
}
