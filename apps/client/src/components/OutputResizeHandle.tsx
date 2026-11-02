import { useRef, type RefObject } from "react";

const defaultWidth = 38;

export function OutputResizeHandle({
  container,
  width,
  onCommit,
}: {
  container: RefObject<HTMLElement | null>;
  width: number;
  onCommit: (value: number) => void;
}) {
  const dragging = useRef<number | null>(null);
  function apply(value: number) {
    const panel = container.current!;
    const minimum = Math.max(
      30,
      (320 / panel.getBoundingClientRect().width) * 100,
    );
    const next = Math.round(Math.min(65, Math.max(minimum, value)) * 10) / 10;
    panel.style.setProperty("--mewdex-output-width", `${next}%`);
    return next;
  }
  function cancel() {
    if (dragging.current === null) return;
    dragging.current = null;
    container.current?.style.setProperty("--mewdex-output-width", `${width}%`);
  }
  return (
    <div
      className="output-resize-handle"
      role="separator"
      tabIndex={0}
      aria-label="调整输出面板宽度"
      aria-orientation="vertical"
      aria-valuemin={30}
      aria-valuemax={65}
      aria-valuenow={width}
      aria-valuetext={`${Math.round(width)}%`}
      title="拖动调整宽度 · 双击还原 · 方向键微调"
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.currentTarget.focus();
        event.currentTarget.setPointerCapture(event.pointerId);
        dragging.current = width;
      }}
      onPointerMove={(event) => {
        if (dragging.current === null) return;
        const rect = container.current!.getBoundingClientRect();
        dragging.current = apply(
          ((rect.right - event.clientX) / rect.width) * 100,
        );
      }}
      onPointerUp={() => {
        if (dragging.current === null) return;
        const value = dragging.current;
        dragging.current = null;
        onCommit(value);
      }}
      onPointerCancel={cancel}
      onLostPointerCapture={cancel}
      onDoubleClick={() => onCommit(apply(defaultWidth))}
      onKeyDown={(event) => {
        if (event.key === "Escape" && dragging.current !== null) {
          event.preventDefault();
          cancel();
        } else if (
          ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
        ) {
          event.preventDefault();
          onCommit(
            apply(
              event.key === "Home"
                ? 30
                : event.key === "End"
                  ? 65
                  : width + (event.key === "ArrowLeft" ? 2 : -2),
            ),
          );
        }
      }}
    />
  );
}
