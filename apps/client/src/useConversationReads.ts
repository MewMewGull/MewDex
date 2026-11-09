import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type RefObject,
} from "react";
import type { Event, NetworkSnapshot } from "@mewdex/sdk";
import { ConversationReadLedger } from "./conversationReads";

const storage = {
  getItem: (key: string) => localStorage.getItem(key),
  setItem: (key: string, value: string) => localStorage.setItem(key, value),
  removeItem: (key: string) => localStorage.removeItem(key),
};

export function useConversationReads(scope: string) {
  const ledger = useMemo(
    () => new ConversationReadLedger(storage, scope),
    [scope],
  );
  const [unread, setUnread] = useState<ReadonlySet<string>>(ledger.unread);
  const bootstrap = useCallback(
    (snapshot: NetworkSnapshot) => {
      if (ledger.bootstrap(snapshot)) setUnread(ledger.unread);
    },
    [ledger],
  );
  const event = useCallback(
    (value: Event) => {
      if (ledger.accept(value)) setUnread(ledger.unread);
    },
    [ledger],
  );
  const markRead = useCallback(
    (deviceId: string, sessionId: string, through: number) => {
      if (ledger.read(deviceId, sessionId, through)) setUnread(ledger.unread);
    },
    [ledger],
  );
  const clear = useCallback(() => {
    ledger.clear();
    setUnread(ledger.unread);
  }, [ledger]);
  const markAllRead = useCallback(
    (sessions: { id: string; deviceId: string }[]) => {
      if (ledger.markAllRead(sessions)) setUnread(ledger.unread);
    },
    [ledger],
  );
  return { unread, bootstrap, event, markRead, markAllRead, clear };
}

// A selected conversation is read only when its current content is rendered,
// the window has focus and the reader has reached the latest messages.
export function useReadVisibleConversation(
  {
    deviceId,
    sessionId,
    events,
    enabled,
    area,
  }: {
    deviceId: string;
    sessionId: string;
    events: Event[];
    enabled: boolean;
    area: RefObject<HTMLElement | null>;
  },
  markRead: (deviceId: string, sessionId: string, through: number) => void,
) {
  const through = events.reduce(
    (last, event) =>
      event.sessionId === sessionId ? Math.max(last, event.sequence) : last,
    0,
  );
  useEffect(() => {
    if (!enabled || !sessionId || !through) return;
    let frame = 0;
    const check = () => {
      const element = area.current;
      if (
        document.visibilityState === "visible" &&
        document.hasFocus() &&
        element &&
        !element.closest('[aria-hidden="true"], [inert]') &&
        element.scrollHeight - element.scrollTop - element.clientHeight < 80
      )
        markRead(deviceId, sessionId, through);
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(check);
    };
    schedule();
    window.addEventListener("focus", schedule);
    document.addEventListener("focusin", schedule);
    document.addEventListener("visibilitychange", schedule);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("focus", schedule);
      document.removeEventListener("focusin", schedule);
      document.removeEventListener("visibilitychange", schedule);
    };
  }, [deviceId, sessionId, through, enabled, area, markRead]);
}
