import { useEffect, useLayoutEffect, type RefObject } from "react";

function resize(input: HTMLTextAreaElement) {
  const scroll = input.scrollTop;
  input.style.height = "auto";
  input.style.height = `${input.scrollHeight}px`;
  input.scrollTop = scroll;
}

// CSS supplies minimum and maximum heights; content and width drive the size.
export function useAutosizeTextarea(
  ref: RefObject<HTMLTextAreaElement | null>,
  value: string,
  enabled = true,
) {
  useLayoutEffect(() => {
    if (enabled && ref.current) resize(ref.current);
  }, [ref, value, enabled]);
  useEffect(() => {
    const input = ref.current;
    if (!enabled || !input) return;
    let width = input.getBoundingClientRect().width;
    const observer = new ResizeObserver(() => {
      const next = input.getBoundingClientRect().width;
      if (next !== width) {
        width = next;
        resize(input);
      }
    });
    observer.observe(input);
    return () => observer.disconnect();
  }, [ref, enabled]);
}
