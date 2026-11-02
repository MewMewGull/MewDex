// DOM layout belongs to the shared input UI, never to the network SDK or an
// executor. Mobile keyboards resize and pan the visible part of a WebView.
export function attachInputViewport(): () => void {
  const viewport = window.visualViewport;
  if (!viewport) return () => {};
  const style = document.documentElement.style;
  const update = () => {
    // Keep manual pinch zoom independent of the application's layout size.
    if (Math.abs(viewport.scale - 1) > 0.01) return;
    style.setProperty("--mewdex-input-height", `${viewport.height}px`);
    style.setProperty("--mewdex-input-top", `${viewport.offsetTop}px`);
  };
  viewport.addEventListener("resize", update);
  viewport.addEventListener("scroll", update);
  update();
  return () => {
    viewport.removeEventListener("resize", update);
    viewport.removeEventListener("scroll", update);
    style.removeProperty("--mewdex-input-height");
    style.removeProperty("--mewdex-input-top");
  };
}

export function hasTouchInput(): boolean {
  return window.matchMedia("(pointer: coarse)").matches;
}
