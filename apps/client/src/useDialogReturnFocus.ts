import { useRef } from "react";

const returnTargets = new WeakMap<Element, HTMLElement>();

// Controlled dialogs may be opened without a Radix Trigger. Keep the original
// target plus its outer entry point when one dialog hands off to another.
export function useDialogReturnFocus() {
  const origin = useRef<{
    target: HTMLElement | null;
    fallback?: HTMLElement;
  } | null>(null);
  return {
    capture(event: Event) {
      const active = document.activeElement;
      const target = active instanceof HTMLElement ? active : null;
      const parent = target?.closest('[role="dialog"]');
      const fallback = parent ? returnTargets.get(parent) : undefined;
      origin.current = { target, fallback };
      if (event.target instanceof Element && (fallback || target))
        returnTargets.set(event.target, fallback ?? target!);
    },
    restore(event: Event, preserveCurrent = false) {
      event.preventDefault();
      const saved = origin.current;
      origin.current = null;
      if (preserveCurrent) return;
      const current = document.activeElement;
      if (
        current instanceof HTMLElement &&
        current !== document.body &&
        current !== document.documentElement &&
        current.isConnected
      )
        return;
      const target = saved?.target?.isConnected
        ? saved.target
        : saved?.fallback;
      if (target?.isConnected) target.focus({ preventScroll: true });
    },
  };
}
