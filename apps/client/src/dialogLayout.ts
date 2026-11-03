export interface DialogSize {
  width: number;
  height: number;
}

export interface DialogRect extends DialogSize {
  left: number;
  top: number;
}

export type ResizeDirection = "n" | "s" | "e" | "w" | "ne" | "nw" | "se" | "sw";

export interface DialogLayout {
  size?: DialogSize;
  maximized?: boolean;
}

function limits(viewport: DialogSize) {
  const margin = Math.min(
    viewport.width <= 640 ? 10 : 24,
    viewport.width / 2,
    viewport.height / 2,
  );
  return {
    margin,
    width: Math.max(0, viewport.width - margin * 2),
    height: Math.max(0, viewport.height - margin * 2),
  };
}

const clamp = (value: number, minimum: number, maximum: number) =>
  Math.min(maximum, Math.max(minimum, value));

export function fitDialog(
  layout: DialogLayout,
  viewport: DialogSize,
  position?: Pick<DialogRect, "left" | "top">,
): DialogRect {
  const available = limits(viewport);
  const requested = layout.size ?? {
    width: Math.min(1400, Math.max(960, viewport.width * 0.8)),
    height: Math.min(900, viewport.height * 0.86),
  };
  const width = layout.maximized
    ? available.width
    : clamp(requested.width, Math.min(560, available.width), available.width);
  const height = layout.maximized
    ? available.height
    : clamp(
        requested.height,
        Math.min(360, available.height),
        available.height,
      );
  return {
    width,
    height,
    left: clamp(
      layout.maximized
        ? available.margin
        : (position?.left ?? (viewport.width - width) / 2),
      available.margin,
      viewport.width - available.margin - width,
    ),
    top: clamp(
      layout.maximized
        ? available.margin
        : (position?.top ?? (viewport.height - height) / 2),
      available.margin,
      viewport.height - available.margin - height,
    ),
  };
}

export function resizeDialog(
  start: DialogRect,
  direction: ResizeDirection,
  delta: { x: number; y: number },
  viewport: DialogSize,
): DialogRect {
  const available = limits(viewport);
  const minimumWidth = Math.min(560, available.width);
  const minimumHeight = Math.min(360, available.height);
  const fitted = fitDialog({ size: start }, viewport, start);
  let { left, top } = fitted;
  let right = left + fitted.width;
  let bottom = top + fitted.height;
  if (direction.includes("w"))
    left = clamp(left + delta.x, available.margin, right - minimumWidth);
  if (direction.includes("e"))
    right = clamp(
      right + delta.x,
      left + minimumWidth,
      viewport.width - available.margin,
    );
  if (direction.includes("n"))
    top = clamp(top + delta.y, available.margin, bottom - minimumHeight);
  if (direction.includes("s"))
    bottom = clamp(
      bottom + delta.y,
      top + minimumHeight,
      viewport.height - available.margin,
    );
  return { left, top, width: right - left, height: bottom - top };
}

export function parseDialogLayout(raw: string | null): DialogLayout {
  try {
    const value = JSON.parse(raw ?? "{}");
    const size = value?.size;
    return {
      size:
        Number.isFinite(size?.width) &&
        size.width > 0 &&
        Number.isFinite(size?.height) &&
        size.height > 0
          ? { width: size.width, height: size.height }
          : undefined,
      maximized: value?.maximized === true,
    };
  } catch {
    return {};
  }
}
