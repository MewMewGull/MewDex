import { t } from "../localization.ts";
import { useEffect, useRef } from "react";
import { ArrowDown, ArrowUp, Search, X } from "lucide-react";

export function OutputFindBar({
  query,
  onQuery,
  status,
  hasResults,
  onPrevious,
  onNext,
  onClose,
  label,
  closeLabel = t("关闭输出查找"),
  autoFocus = true,
}: {
  query: string;
  onQuery: (query: string) => void;
  status: string;
  hasResults: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onClose: () => void;
  label: string;
  closeLabel?: string;
  autoFocus?: boolean;
}) {
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!autoFocus) return;
    field.current?.focus();
    field.current?.select();
  }, [autoFocus]);
  return (
    <div className="output-find-bar">
      <Search size={14} />
      <input
        data-output-find-input
        ref={field}
        aria-label={label}
        placeholder={label}
        value={query}
        onChange={(event) => onQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "Enter") {
            event.preventDefault();
            event.shiftKey ? onPrevious() : onNext();
          }
          if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            onClose();
          }
        }}
      />
      <small role="status">{status}</small>
      <button
        className="icon-button"
        aria-label={t("上一个匹配")}
        disabled={!hasResults}
        onClick={onPrevious}
      >
        <ArrowUp size={14} />
      </button>
      <button
        className="icon-button"
        aria-label={t("下一个匹配")}
        disabled={!hasResults}
        onClick={onNext}
      >
        <ArrowDown size={14} />
      </button>
      <button className="icon-button" aria-label={closeLabel} onClick={onClose}>
        <X size={14} />
      </button>
    </div>
  );
}
