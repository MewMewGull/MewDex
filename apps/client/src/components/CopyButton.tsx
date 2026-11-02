import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";

export function CopyButton({
  text,
  label = "复制",
}: {
  text: string | (() => string);
  label?: string;
}) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(
        typeof text === "function" ? text() : text,
      );
      setStatus("copied");
    } catch {
      setStatus("failed");
    }
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setStatus("idle"), 1800);
  }
  return (
    <button
      className="copy-button"
      aria-label={label}
      onClick={() => void copy()}
    >
      {status === "copied" ? <Check size={13} /> : <Copy size={13} />}
      <span role="status">
        {status === "copied"
          ? "已复制"
          : status === "failed"
            ? "复制失败，重试"
            : label}
      </span>
    </button>
  );
}
