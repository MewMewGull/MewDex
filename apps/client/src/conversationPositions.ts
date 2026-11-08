export interface HistoryPosition {
  direction: "before" | "after";
  cursor: number;
  target?: { id: string; version: number };
}

export interface ConversationViewport {
  pinned: boolean;
  top: number;
  anchor?: { id: string; sequence: number; offset: number };
  expandedTools: string[];
}

interface ConversationPosition {
  history: HistoryPosition | null;
  viewport?: ConversationViewport;
  followLatest: boolean;
}

// Window-local reading context. Durable messages remain on the Hub; this stores
// only their cursors and layout references, never text or execution state.
export class ConversationPositions {
  private entries = new Map<string, ConversationPosition>();

  get(sessionId: string): ConversationPosition | undefined {
    return this.entries.get(sessionId);
  }

  remember(
    sessionId: string,
    history: HistoryPosition | null,
    viewport: ConversationViewport,
  ) {
    if (!sessionId) return;
    // A live event window can advance far beyond the message being read while
    // another conversation is open. Re-enter that message through its event.
    const position =
      history ??
      (!viewport.pinned && viewport.anchor
        ? { direction: "after" as const, cursor: viewport.anchor.sequence - 1 }
        : null);
    this.entries.set(sessionId, {
      history: position,
      viewport,
      followLatest: !position && viewport.pinned,
    });
  }

  navigate(
    sessionId: string,
    history: HistoryPosition | null,
    followLatest = history === null,
  ) {
    this.entries.set(sessionId, { history, followLatest });
  }

  clear() {
    this.entries.clear();
  }
}

export function captureConversationViewport(
  area: HTMLElement,
  live: boolean,
): ConversationViewport {
  const bounds = area.getBoundingClientRect();
  const visible = Array.from(
    area.querySelectorAll<HTMLElement>("[data-reading-id]"),
  ).find((item) => {
    const rect = item.getBoundingClientRect();
    return (
      rect.height > 0 && rect.bottom > bounds.top && rect.top < bounds.bottom
    );
  });
  return {
    pinned: live && area.scrollHeight - area.scrollTop - area.clientHeight < 64,
    top: area.scrollTop,
    anchor: visible
      ? {
          id: visible.dataset.readingId!,
          sequence: Number(visible.dataset.readingSequence),
          offset: visible.getBoundingClientRect().top - bounds.top,
        }
      : undefined,
    expandedTools: Array.from(
      area.querySelectorAll<HTMLDetailsElement>(
        "details[data-tool-group][open]",
      ),
      (item) => item.dataset.toolGroup!,
    ),
  };
}

export function restoreConversationViewport(
  area: HTMLElement,
  viewport: ConversationViewport,
) {
  const expanded = new Set(viewport.expandedTools);
  for (const item of area.querySelectorAll<HTMLDetailsElement>(
    "details[data-tool-group]",
  ))
    item.open =
      expanded.has(item.dataset.toolGroup!) ||
      (expanded.size > 0 &&
        Array.from(
          item.querySelectorAll<HTMLElement>("[data-message-id]"),
        ).some((message) => expanded.has(message.dataset.messageId!)));
  preserveConversationViewport(area, viewport, true);
}

// Paging keeps this semantic anchor through later image and Markdown reflows.
// Disclosure state is restored only when entering a view or committing a page.
export function preserveConversationViewport(
  area: HTMLElement,
  viewport: ConversationViewport,
  reveal = false,
) {
  if (viewport.pinned) {
    area.scrollTop = area.scrollHeight;
    return true;
  }
  const anchor = viewport.anchor;
  const items = Array.from(
    area.querySelectorAll<HTMLElement>("[data-reading-id]"),
  );
  let item = anchor
    ? items.find((item) => item.dataset.readingId === anchor.id)
    : undefined;
  // An older page can extend a tool group and replace its first-item key.
  if (!item && anchor?.id.startsWith("tool-group:")) {
    const member = items.find(
      (item) =>
        item.dataset.messageId === anchor.id.slice("tool-group:".length),
    );
    item =
      member
        ?.closest<HTMLDetailsElement>("details[data-tool-group]")
        ?.querySelector<HTMLElement>("summary") ?? undefined;
  }
  // Re-entering a live window at a tool event can give its group a different
  // first item. Open the anchor's actual group before measuring the message.
  if (reveal && item?.dataset.messageId) {
    const group = item.closest<HTMLDetailsElement>("details[data-tool-group]");
    if (group) group.open = true;
  }
  if (item && item.getBoundingClientRect().height > 0) {
    area.scrollTop +=
      item.getBoundingClientRect().top -
      area.getBoundingClientRect().top -
      anchor!.offset;
    return true;
  }
  if (reveal) area.scrollTop = viewport.top;
  return false;
}

export function conversationHistoryEdge(
  area: HTMLElement,
  hasOlder: boolean,
  hasNewer: boolean,
): "older" | "newer" | undefined {
  if (area.clientHeight <= 0) return;
  const margin = Math.max(320, area.clientHeight);
  if (hasOlder && area.scrollTop < margin) return "older";
  const bottom = area.scrollHeight - area.scrollTop - area.clientHeight;
  if (hasNewer && bottom < margin) return "newer";
}
