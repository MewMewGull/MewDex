import { useRef, type RefObject } from "react";

export function SidebarResizeHandle({
  sidebar,
  width,
  onCommit,
}: {
  sidebar: RefObject<HTMLElement | null>;
  width: number;
  onCommit: (width: number) => void;
}) {
  const dragging = useRef<number | null>(null);
  function apply(value: number) {
    const next = Math.round(Math.min(440, Math.max(220, value)));
    sidebar.current?.style.setProperty("--sidebar-width", `${next}px`);
    return next;
  }
  function cancel() {
    if (dragging.current === null) return;
    dragging.current = null;
    apply(width);
  }
  return (
    <div
      className="sidebar-resize-handle"
      role="separator"
      tabIndex={0}
      aria-label="调整侧栏宽度"
      aria-orientation="vertical"
      aria-valuemin={220}
      aria-valuemax={440}
      aria-valuenow={width}
      title="拖动调整侧栏 · 双击还原"
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        dragging.current = width;
      }}
      onPointerMove={(event) => {
        if (dragging.current === null || !sidebar.current) return;
        dragging.current = apply(
          event.clientX - sidebar.current.getBoundingClientRect().left,
        );
      }}
      onPointerUp={() => {
        if (dragging.current === null) return;
        const next = dragging.current;
        dragging.current = null;
        onCommit(next);
      }}
      onPointerCancel={cancel}
      onLostPointerCapture={cancel}
      onDoubleClick={() => onCommit(apply(280))}
      onKeyDown={(event) => {
        if (event.key === "Escape") cancel();
        else if (
          ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
        ) {
          event.preventDefault();
          onCommit(
            apply(
              event.key === "Home"
                ? 220
                : event.key === "End"
                  ? 440
                  : width + (event.key === "ArrowRight" ? 10 : -10),
            ),
          );
        }
      }}
    />
  );
}
