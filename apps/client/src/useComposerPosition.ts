import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import { useWorkspace } from "./state";

type ComposerPosition = Pick<
  HTMLTextAreaElement,
  | "selectionStart"
  | "selectionEnd"
  | "selectionDirection"
  | "scrollTop"
  | "scrollLeft"
>;

// Editing position belongs to the original draft. Only numeric offsets stay in
// window memory; draft text, persistence and submission remain elsewhere.
export function useComposerPosition(
  ref: RefObject<HTMLTextAreaElement | null>,
  key: string,
  enabled: boolean,
) {
  const positions = useRef(new Map<string, ComposerPosition>());

  function remember(input = ref.current) {
    const target = input?.dataset.draftKey;
    if (!input || !target) return;
    positions.current.set(target, {
      selectionStart: input.selectionStart,
      selectionEnd: input.selectionEnd,
      selectionDirection: input.selectionDirection,
      scrollTop: input.scrollTop,
      scrollLeft: input.scrollLeft,
    });
  }

  useEffect(() => {
    if (!enabled) return;
    return useWorkspace.subscribe((next, previous) => {
      if (next.selectionVersion === previous.selectionVersion) return;
      const input = ref.current;
      remember(input);
    });
  }, [ref, enabled]);

  // Called after the textarea sizing hook, so restored scrolling uses its new
  // layout. The DOM clamps saved ranges when a receipt has cleared old text.
  useLayoutEffect(() => {
    const input = ref.current;
    if (!enabled || !input) return;
    const saved = positions.current.get(key);
    input.setSelectionRange(
      saved?.selectionStart ?? input.value.length,
      saved?.selectionEnd ?? input.value.length,
      saved?.selectionDirection ?? "none",
    );
    input.scrollTop = saved?.scrollTop ?? input.scrollHeight;
    input.scrollLeft = saved?.scrollLeft ?? 0;
  }, [ref, key, enabled]);

  return {
    remember,
    focus() {
      const input = ref.current;
      if (!input) return;
      const { scrollTop, scrollLeft } = input;
      input.focus({ preventScroll: true });
      input.scrollTop = scrollTop;
      input.scrollLeft = scrollLeft;
    },
    move(from: string, to: string) {
      if (from === to) return;
      if (ref.current?.dataset.draftKey === from) remember();
      const saved = positions.current.get(from);
      if (saved) {
        positions.current.set(to, saved);
        positions.current.delete(from);
      }
    },
    clear() {
      positions.current.clear();
    },
  };
}
