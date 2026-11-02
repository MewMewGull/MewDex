import { useEffect, useState, type RefObject } from "react";
import {
  sameSelection,
  selectionOf,
  useWorkspace,
  type WorkspaceSelection,
} from "./state";

// Both the conversation hint and activity overview explicitly request this jump.
// Incoming request updates never move focus or select an approval action.
export function useInteractionFocus({
  selection,
  live,
  ready,
  area,
  onLatest,
}: {
  selection: WorkspaceSelection;
  live: boolean;
  ready: boolean;
  area: RefObject<HTMLDivElement | null>;
  onLatest: () => void;
}) {
  const [target, setTarget] = useState<WorkspaceSelection | null>(null);
  useEffect(() => {
    if (!target) return;
    if (!live || !sameSelection(target, selection)) {
      setTarget(null);
      return;
    }
    if (!ready) return;
    if (!area.current) {
      setTarget(null);
      return;
    }
    const frame = requestAnimationFrame(() => {
      if (!sameSelection(target, useWorkspace.getState())) return;
      area.current?.focus({ preventScroll: true });
      area.current?.scrollIntoView({ block: "start", behavior: "smooth" });
      setTarget(null);
    });
    return () => cancelAnimationFrame(frame);
  }, [
    target,
    selection.deviceId,
    selection.sessionId,
    selection.selectionVersion,
    live,
    ready,
    area,
  ]);
  return {
    show: () => {
      const current = selectionOf(useWorkspace.getState());
      if (!current.sessionId) return;
      onLatest();
      setTarget(current);
    },
    clear: () => setTarget(null),
  };
}
