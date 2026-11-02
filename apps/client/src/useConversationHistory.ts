import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Event, MessageMatch, MewDexClient } from "@mewdex/sdk";
import { useWorkspace } from "./state";
import {
  ConversationPositions,
  captureConversationViewport,
  restoreConversationViewport,
  type ConversationViewport,
  type HistoryPosition,
} from "./conversationPositions";

// Reader pages are independent of the bounded live event window.
export function useConversationHistory(
  client: MewDexClient,
  sessionId: string,
  enabled: boolean,
  area: RefObject<HTMLDivElement | null>,
  liveEvents: Event[],
) {
  const [positions] = useState(() => new ConversationPositions());
  const [version, setVersion] = useState(0);
  const [laidOut, setLaidOut] = useState("");
  const [readingEarlier, setReadingEarlier] = useState(false);
  const pinned = useRef(true);
  const applied = useRef("");
  const focusVersion = useRef(0);
  const pendingAnchor = useRef<
    { key: string; viewport: ConversationViewport; pages: number } | undefined
  >(undefined);
  const saved = positions.get(sessionId);
  const current = saved?.history ?? null;
  const direction = current?.direction ?? "before";
  const cursor = current?.cursor ?? 0;
  const viewKey = JSON.stringify([sessionId, version, direction, cursor]);
  const pages = useInfiniteQuery({
    queryKey: ["continuous-history", sessionId, version, direction, cursor],
    initialPageParam: { direction, cursor },
    queryFn: ({ pageParam, signal }) =>
      pageParam.direction === "after"
        ? client.historyAfter(sessionId, pageParam.cursor, 200, signal)
        : client.historyPage(sessionId, pageParam.cursor, 200, signal),
    getPreviousPageParam: (first) =>
      first.hasMore && first.before
        ? { direction: "before" as const, cursor: first.before }
        : undefined,
    getNextPageParam: (last) =>
      last.hasNewer && last.after
        ? { direction: "after" as const, cursor: last.after }
        : undefined,
    enabled: enabled && !!sessionId,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  const lastPage = pages.data?.pages.at(-1);
  const live = !!lastPage && !lastPage.hasNewer;
  const readWindow = useRef<{ key: string; entries: Map<string, Event> }>({
    key: "",
    entries: new Map(),
  });
  const events = useMemo(() => {
    if (readWindow.current.key !== viewKey)
      readWindow.current = { key: viewKey, entries: new Map() };
    const combined = readWindow.current.entries;
    for (const page of pages.data?.pages ?? [])
      for (const event of page.events) combined.set(event.id, event);
    if (live)
      for (const event of liveEvents)
        if (event.sessionId === sessionId) combined.set(event.id, event);
    return [...combined.values()].sort((a, b) => a.sequence - b.sequence);
  }, [pages.data, liveEvents, live, sessionId, viewKey]);
  useLayoutEffect(() => {
    useWorkspace.getState().followEvents(live);
  }, [sessionId, live]);
  useEffect(() => {
    if (live && lastPage)
      useWorkspace.getState().loadEvents(sessionId, lastPage.events, true);
  }, [lastPage, sessionId, live]);
  const remember = useRef((_id: string) => {});
  remember.current = (id) => {
    if (id === sessionId && applied.current === viewKey && area.current)
      positions.remember(
        id,
        current,
        captureConversationViewport(area.current, live),
      );
  };
  useEffect(
    () =>
      useWorkspace.subscribe((next, previous) => {
        if (
          next.sessionId !== previous.sessionId ||
          next.deviceId !== previous.deviceId
        )
          remember.current(previous.sessionId);
      }),
    [],
  );
  useLayoutEffect(() => {
    const element = area.current;
    if (!sessionId || !element || !pages.data) return;
    if (applied.current !== viewKey) {
      pinned.current = saved?.followLatest ?? !current;
      if (saved?.viewport) restoreConversationViewport(element, saved.viewport);
      else if (current?.target) {
        const message = element.querySelector<HTMLElement>(
          `[data-message-id="${CSS.escape(current.target.id)}"]`,
        );
        message?.focus({ preventScroll: true });
        message?.scrollIntoView({ block: "center" });
      } else element.scrollTop = pinned.current ? element.scrollHeight : 0;
      applied.current = viewKey;
      setLaidOut(viewKey);
      setReadingEarlier(!live || !pinned.current);
    } else if (pendingAnchor.current?.key === viewKey) {
      if (pendingAnchor.current.pages !== pages.data.pages.length) {
        restoreConversationViewport(element, pendingAnchor.current.viewport);
        pendingAnchor.current = undefined;
      } else if (!pages.isFetching) pendingAnchor.current = undefined;
    } else if (live && pinned.current) element.scrollTop = element.scrollHeight;
  });
  useEffect(() => {
    const element = area.current;
    if (!element || !sessionId) return;
    const observer = new ResizeObserver(() => {
      if (
        live &&
        pinned.current &&
        applied.current === viewKey &&
        !pendingAnchor.current
      )
        element.scrollTop = element.scrollHeight;
    });
    observer.observe(element);
    if (element.firstElementChild) observer.observe(element.firstElementChild);
    return () => observer.disconnect();
  }, [area, sessionId, live, viewKey]);
  function load(direction: "older" | "newer") {
    const element = area.current;
    if (
      !element ||
      pages.isFetching ||
      !(direction === "older" ? pages.hasPreviousPage : pages.hasNextPage)
    )
      return;
    pinned.current = false;
    pendingAnchor.current = {
      key: viewKey,
      viewport: captureConversationViewport(element, false),
      pages: pages.data?.pages.length ?? 0,
    };
    void (direction === "older"
      ? pages.fetchPreviousPage()
      : pages.fetchNextPage());
  }
  function navigate(
    history: HistoryPosition | null,
    followLatest = !history,
    targetSessionId = sessionId,
  ) {
    positions.navigate(targetSessionId, history, followLatest);
    pinned.current = followLatest;
    pendingAnchor.current = undefined;
    setVersion((value) => value + 1);
  }
  return {
    events,
    page: { ...pages, data: lastPage },
    live,
    contextKey: JSON.stringify([sessionId, version, direction, cursor]),
    target: current?.target,
    restoring: !!sessionId && laidOut !== viewKey,
    readingEarlier,
    hasOlder: pages.hasPreviousPage,
    hasNewer: pages.hasNextPage,
    loadingOlder: pages.isFetchingPreviousPage,
    loadingNewer: pages.isFetchingNextPage,
    onScroll: () => {
      const element = area.current;
      if (!element || applied.current !== viewKey || pendingAnchor.current)
        return;
      const bottom =
        element.scrollHeight - element.scrollTop - element.clientHeight;
      pinned.current = live && bottom < 64;
      setReadingEarlier(!live || !pinned.current);
      if (element.scrollTop < 160) load("older");
      else if (bottom < 160) load("newer");
    },
    earlier: (_before?: number) => load("older"),
    newer: (_after?: number) => load("newer"),
    latest: (followLatest = true) =>
      navigate(null, followLatest, useWorkspace.getState().sessionId),
    locate: (match: MessageMatch) =>
      navigate({
        direction: "before",
        cursor: match.sequence + 1,
        target: { id: match.id, version: ++focusVersion.current },
      }),
    clear: () => {
      positions.clear();
      applied.current = "";
      pendingAnchor.current = undefined;
      pinned.current = true;
      setVersion((value) => value + 1);
    },
  };
}
