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
import { useWorkspace } from "./state.ts";
import {
  ConversationPositions,
  captureConversationViewport,
  conversationHistoryEdge,
  preserveConversationViewport,
  restoreConversationViewport,
  type ConversationViewport,
  type HistoryPosition,
} from "./conversationPositions.ts";

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
    | {
        key: string;
        viewport: ConversationViewport;
        pages: number;
        pending: boolean;
      }
    | undefined
  >(undefined);
  const request = useRef<object | undefined>(undefined);
  const scrollTop = useRef(0);
  const checkEdges = useRef(() => {});
  const scheduleEdges = useRef(() => {});
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
        captureConversationViewport(area.current, live && pinned.current),
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
      pendingAnchor.current = undefined;
      request.current = undefined;
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
      const anchor = pendingAnchor.current;
      if (anchor.pages !== pages.data.pages.length) {
        restoreConversationViewport(element, anchor.viewport);
        anchor.pages = pages.data.pages.length;
        anchor.pending = false;
      } else {
        if (!preserveConversationViewport(element, anchor.viewport))
          pendingAnchor.current = undefined;
        if (!pages.isFetching && !request.current) anchor.pending = false;
      }
    } else if (live && pinned.current) element.scrollTop = element.scrollHeight;
    scrollTop.current = element.scrollTop;
    element.dataset.followingLatest = String(live && pinned.current);
    element.style.overflowAnchor =
      pinned.current || pendingAnchor.current?.key === viewKey ? "none" : "";
    scheduleEdges.current();
  });
  checkEdges.current = () => {
    const element = area.current;
    if (
      !enabled ||
      !element ||
      applied.current !== viewKey ||
      !pages.data ||
      pages.isFetching ||
      pages.isError ||
      request.current
    )
      return;
    const edge = conversationHistoryEdge(
      element,
      pages.hasPreviousPage,
      pages.hasNextPage,
    );
    if (edge) load(edge);
  };
  useEffect(() => {
    let frame = 0;
    scheduleEdges.current = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => checkEdges.current());
    };
    scheduleEdges.current();
    return () => {
      cancelAnimationFrame(frame);
      scheduleEdges.current = () => {};
    };
  }, []);
  useEffect(() => {
    const element = area.current;
    if (!element || !sessionId || !pages.data) return;
    const observer = new ResizeObserver(() => {
      if (applied.current !== viewKey) return;
      const anchor = pendingAnchor.current;
      if (anchor?.key === viewKey) {
        if (!preserveConversationViewport(element, anchor.viewport))
          pendingAnchor.current = undefined;
      } else if (live && pinned.current)
        element.scrollTop = element.scrollHeight;
      scrollTop.current = element.scrollTop;
      element.style.overflowAnchor =
        pinned.current || pendingAnchor.current?.key === viewKey ? "none" : "";
      scheduleEdges.current();
    });
    const onToggle = () => {
      const anchor = pendingAnchor.current;
      if (anchor?.key === viewKey)
        anchor.viewport = captureConversationViewport(
          element,
          live && pinned.current,
        );
    };
    observer.observe(element);
    if (element.firstElementChild) observer.observe(element.firstElementChild);
    element.addEventListener("toggle", onToggle, true);
    return () => {
      observer.disconnect();
      element.removeEventListener("toggle", onToggle, true);
    };
  }, [area, sessionId, live, viewKey, !!pages.data]);
  function load(direction: "older" | "newer") {
    const element = area.current;
    if (
      !element ||
      !enabled ||
      applied.current !== viewKey ||
      request.current ||
      pages.isFetching ||
      !(direction === "older" ? pages.hasPreviousPage : pages.hasNextPage)
    )
      return;
    pendingAnchor.current = {
      key: viewKey,
      viewport: captureConversationViewport(element, live && pinned.current),
      pages: pages.data?.pages.length ?? 0,
      pending: true,
    };
    element.style.overflowAnchor = "none";
    const active = {};
    request.current = active;
    void (
      direction === "older"
        ? pages.fetchPreviousPage({ cancelRefetch: false })
        : pages.fetchNextPage({ cancelRefetch: false })
    ).then(() => {
      if (request.current !== active) return;
      request.current = undefined;
      scheduleEdges.current();
    });
  }
  function navigate(
    history: HistoryPosition | null,
    followLatest = !history,
    targetSessionId = sessionId,
  ) {
    positions.navigate(targetSessionId, history, followLatest);
    pinned.current = followLatest;
    pendingAnchor.current = undefined;
    request.current = undefined;
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
      if (!element || applied.current !== viewKey) return;
      const previousTop = scrollTop.current;
      const moved = element.scrollTop !== previousTop;
      scrollTop.current = element.scrollTop;
      const bottom =
        element.scrollHeight - element.scrollTop - element.clientHeight;
      if (moved) {
        pinned.current =
          live &&
          (bottom <= 1 || (bottom < 64 && element.scrollTop > previousTop));
        const anchor = pendingAnchor.current;
        if (anchor?.key === viewKey && anchor.pending)
          anchor.viewport = captureConversationViewport(
            element,
            live && pinned.current,
          );
        else pendingAnchor.current = undefined;
      }
      element.dataset.followingLatest = String(live && pinned.current);
      element.style.overflowAnchor =
        pinned.current || pendingAnchor.current?.key === viewKey ? "none" : "";
      setReadingEarlier(!live || !pinned.current);
      scheduleEdges.current();
    },
    earlier: (_before?: number) => load("older"),
    newer: (_after?: number) => load("newer"),
    retry: () => {
      if (pages.isFetchPreviousPageError) load("older");
      else if (pages.isFetchNextPageError) load("newer");
      else void pages.refetch();
    },
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
      request.current = undefined;
      pinned.current = true;
      setVersion((value) => value + 1);
    },
  };
}
