import { getLanguage, t } from "../localization.ts";
import { useId, useState } from "react";
import type { Session } from "@mewdex/sdk";

export function ContextUsageIndicator({ session }: { session?: Session }) {
  const tokenCount = new Intl.NumberFormat(getLanguage(), {
    notation: "compact",
    maximumFractionDigits: 0,
  });
  const [open, setOpen] = useState(false);
  const tooltipId = useId();
  const usage = session?.contextUsage;
  const capacity = usage?.modelContextWindow;
  const used = usage?.totalTokens;
  const percent =
    used != null && capacity != null && capacity > 0
      ? Math.min(100, Math.max(0, (used / capacity) * 100))
      : null;
  const roundedPercent = percent == null ? null : Math.round(percent);

  return (
    <div
      className="composer-context-indicator"
      onPointerEnter={(event) => {
        if (event.pointerType !== "touch") setOpen(true);
      }}
      onPointerLeave={(event) => {
        if (event.pointerType !== "touch") setOpen(false);
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          setOpen(false);
          event.stopPropagation();
        }
      }}
    >
      <button
        type="button"
        className="composer-context-button"
        aria-label={
          roundedPercent == null
            ? t("上下文使用情况")
            : t(
                "上下文已使用 {0}%，剩余 {1}%",
                roundedPercent,
                100 - roundedPercent,
              )
        }
        aria-describedby={open ? tooltipId : undefined}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onClick={() => setOpen(true)}
      >
        <svg
          className="composer-context-ring"
          width="16"
          height="16"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          aria-hidden="true"
        >
          <circle className="composer-context-track" cx="10" cy="10" r="7.5" />
          {percent != null && percent > 0 && (
            <circle
              cx="10"
              cy="10"
              r="7.5"
              pathLength="100"
              strokeDasharray={`${percent} 100`}
              strokeLinecap="round"
              transform="rotate(-90 10 10)"
            />
          )}
        </svg>
      </button>
      {open && (
        <div className="composer-context-tooltip" id={tooltipId} role="tooltip">
          <span>{t("背景信息窗口：")}</span>
          {roundedPercent != null && used != null && capacity != null ? (
            <>
              <p>
                {t(
                  "{0}% 已用（剩余 {1}%）",
                  roundedPercent,
                  100 - roundedPercent,
                )}
              </p>
              <p
                aria-label={t(
                  "已用 {0} 标记，共 {1}",
                  used.toLocaleString(getLanguage()),
                  capacity.toLocaleString(getLanguage()),
                )}
              >
                {t(
                  "已用 {0} 标记，共 {1}",
                  tokenCount.format(used).toLowerCase(),
                  tokenCount.format(capacity).toLowerCase(),
                )}
              </p>
            </>
          ) : (
            <p>
              {session?.engineStarted
                ? t("引擎尚未返回上下文用量。")
                : t("发送后显示实际上下文用量。")}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
