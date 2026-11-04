import {
  useLayoutEffect,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type PointerEvent,
  type ReactNode,
} from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Maximize2, Minimize2, MoveDiagonal2, RotateCcw } from "lucide-react";
import {
  fitDialog,
  parseDialogLayout,
  resizeDialog,
  type DialogLayout,
  type DialogRect,
  type ResizeDirection,
} from "../dialogLayout";

const directions: ResizeDirection[] = [
  "n",
  "s",
  "e",
  "w",
  "ne",
  "nw",
  "sw",
  "se",
];

function dialogViewport() {
  const top =
    parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--desktop-titlebar-height",
      ),
    ) || 0;
  return { width: window.innerWidth, height: window.innerHeight - top, top };
}

export function ResizableDialogContent({
  sizeKey,
  children,
  className = "",
  style,
  ...props
}: Omit<ComponentPropsWithoutRef<typeof Dialog.Content>, "children"> & {
  sizeKey: string;
  children: (sizeControls: ReactNode) => ReactNode;
}) {
  const storageKey = `mewdex.dialog-layout.${sizeKey}.v1`;
  const [layout, setLayout] = useState<DialogLayout>(() => {
    try {
      return parseDialogLayout(localStorage.getItem(storageKey));
    } catch {
      return {};
    }
  });
  const [viewport, setViewport] = useState(dialogViewport);
  const [position, setPosition] = useState<DialogRect>();
  const [resizing, setResizing] = useState<ResizeDirection>();
  const drag = useRef<{
    pointerId: number;
    x: number;
    y: number;
    start: DialogRect;
    latest: DialogRect;
  } | null>(null);
  const rect = fitDialog(layout, viewport, position);

  useLayoutEffect(() => {
    const update = () => setViewport(dialogViewport());
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-custom-titlebar"],
    });
    window.addEventListener("resize", update);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", update);
    };
  }, []);

  function remember(next: DialogLayout) {
    setLayout(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
    } catch {
      // Resizing remains available when local storage is unavailable.
    }
  }
  function reset() {
    drag.current = null;
    setResizing(undefined);
    setPosition(undefined);
    remember({});
  }
  function finishResize(event: PointerEvent<HTMLButtonElement>) {
    if (!drag.current || drag.current.pointerId !== event.pointerId) return;
    const { width, height } = drag.current.latest;
    drag.current = null;
    setResizing(undefined);
    remember({ size: { width, height } });
  }
  const sizeControls = (
    <>
      <button
        type="button"
        className="icon-button"
        onClick={reset}
        aria-label="还原弹窗默认尺寸"
        title="还原默认尺寸"
      >
        <RotateCcw size={16} />
      </button>
      <button
        type="button"
        className="icon-button"
        onClick={() => remember({ ...layout, maximized: !layout.maximized })}
        aria-label={layout.maximized ? "还原弹窗大小" : "最大化弹窗"}
        title={layout.maximized ? "还原大小" : "最大化"}
        aria-pressed={!!layout.maximized}
      >
        {layout.maximized ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
      </button>
    </>
  );
  return (
    <Dialog.Content
      {...props}
      className={`${className} resizable-dialog`}
      data-resizing={resizing}
      style={{
        ...style,
        ...rect,
        top: rect.top + viewport.top,
        transform: "none",
      }}
    >
      {children(sizeControls)}
      {!layout.maximized &&
        directions.map((direction) => (
          <button
            key={direction}
            type="button"
            className={`dialog-resize-handle dialog-resize-${direction}`}
            tabIndex={direction === "se" ? 0 : -1}
            aria-hidden={direction !== "se" ? true : undefined}
            aria-label="调整弹窗大小"
            title="拖动调整大小 · 双击还原 · 方向键微调 · Home 还原"
            onPointerDown={(event) => {
              if (event.button !== 0 || drag.current) return;
              event.preventDefault();
              event.currentTarget.setPointerCapture(event.pointerId);
              drag.current = {
                pointerId: event.pointerId,
                x: event.clientX,
                y: event.clientY,
                start: rect,
                latest: rect,
              };
              setResizing(direction);
            }}
            onPointerMove={(event) => {
              const current = drag.current;
              if (!current || current.pointerId !== event.pointerId) return;
              const next = resizeDialog(
                current.start,
                direction,
                {
                  x: event.clientX - current.x,
                  y: event.clientY - current.y,
                },
                viewport,
              );
              current.latest = next;
              setPosition(next);
              setLayout({ size: { width: next.width, height: next.height } });
            }}
            onPointerUp={finishResize}
            onPointerCancel={finishResize}
            onLostPointerCapture={finishResize}
            onDoubleClick={reset}
            onKeyDown={(event) => {
              if (event.key === "Home") {
                event.preventDefault();
                reset();
              } else if (
                ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(
                  event.key,
                )
              ) {
                event.preventDefault();
                const step = event.shiftKey ? 50 : 10;
                const next = fitDialog(
                  {
                    size: {
                      width:
                        rect.width +
                        (event.key === "ArrowRight"
                          ? step
                          : event.key === "ArrowLeft"
                            ? -step
                            : 0),
                      height:
                        rect.height +
                        (event.key === "ArrowDown"
                          ? step
                          : event.key === "ArrowUp"
                            ? -step
                            : 0),
                    },
                  },
                  viewport,
                );
                setPosition(undefined);
                remember({ size: { width: next.width, height: next.height } });
              }
            }}
          >
            {direction === "se" && <MoveDiagonal2 size={14} />}
          </button>
        ))}
    </Dialog.Content>
  );
}
