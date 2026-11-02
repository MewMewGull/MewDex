// Android exposes this small, origin-scoped bridge before loading the input UI.
// Browser and desktop inputs use the same appearance code without a native bar.
export function attachMobileSystemBars(): () => void {
  const bridge = (window as Window & {
    mewdexSystemBars?: { postMessage(theme: string): void };
  }).mewdexSystemBars;
  if (!bridge) return () => {};
  const root = document.documentElement;
  const system = window.matchMedia("(prefers-color-scheme: light)");
  const update = () => {
    bridge.postMessage(getComputedStyle(root).colorScheme === "light" ? "light" : "dark");
  };
  const observer = new MutationObserver(update);
  observer.observe(root, { attributes: true, attributeFilter: ["data-theme"] });
  system.addEventListener("change", update);
  update();
  return () => {
    observer.disconnect();
    system.removeEventListener("change", update);
  };
}
