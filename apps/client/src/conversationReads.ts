import type { ConversationUpdate, Event, NetworkSnapshot } from "@mewdex/sdk";

export interface ReadPositionStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

interface SavedReads {
  baseline: number | null;
  positions: Record<string, number>;
}
const empty = (): SavedReads => ({ baseline: null, positions: {} });
const positionKey = (deviceId: string, sessionId: string) =>
  JSON.stringify([deviceId, sessionId]);

function replyUpdate(event: Event): ConversationUpdate | null {
  if (!event.sessionId || !event.deviceId) return null;
  if (event.kind !== "conversation.changed") {
    if (event.kind !== "engine.item/completed") return null;
    const payload = event.payload as {
      item?: { type?: string; text?: string };
    } | null;
    if (
      payload?.item?.type !== "agentMessage" ||
      typeof payload.item.text !== "string" ||
      !payload.item.text
    )
      return null;
  }
  return {
    deviceId: event.deviceId,
    sessionId: event.sessionId,
    sequence: event.sequence,
  };
}

// Each input retains only read positions. Reply text, execution and system
// notification preferences remain in their existing, independent modules.
export class ConversationReadLedger {
  private saved: SavedReads = empty();
  private latest = new Map<string, ConversationUpdate>();
  private readonly key: string;
  private writable = true;

  constructor(
    private readonly storage: ReadPositionStorage,
    scope: string,
  ) {
    this.key = `mewdex.read-positions.v1:${scope}`;
    try {
      const raw = storage.getItem(this.key);
      if (raw) this.saved = JSON.parse(raw) as SavedReads;
    } catch {
      // An unread badge must not prevent opening a conversation. Preserve an
      // unreadable saved record while retaining positions for this window.
      this.writable = false;
    }
  }

  private persist() {
    if (!this.writable) return;
    try {
      this.storage.setItem(this.key, JSON.stringify(this.saved));
    } catch {
      // The current window can continue tracking reads without local storage.
    }
  }

  bootstrap(snapshot: NetworkSnapshot): boolean {
    // Older Hubs do not advertise reply positions. Wait for a supported
    // snapshot rather than marking all old conversations unread on upgrade.
    if (!snapshot.conversationUpdates) return false;
    if (this.saved.baseline === null || snapshot.cursor < this.saved.baseline) {
      this.saved = { baseline: snapshot.cursor, positions: {} };
      this.persist();
    }
    const visible = new Map(
      snapshot.sessions.map((session) => [session.id, session.deviceId]),
    );
    this.latest = new Map(
      snapshot.conversationUpdates
        .filter((update) => visible.get(update.sessionId) === update.deviceId)
        .map((update) => [update.sessionId, update]),
    );
    return true;
  }

  accept(event: Event): boolean {
    if (this.saved.baseline === null) return false;
    const update = replyUpdate(event);
    if (
      !update ||
      update.sequence <= (this.latest.get(update.sessionId)?.sequence ?? 0)
    )
      return false;
    this.latest.set(update.sessionId, update);
    return true;
  }

  get unread(): ReadonlySet<string> {
    return new Set(
      [...this.latest.values()]
        .filter(
          (update) =>
            update.sequence >
            (this.saved.positions[
              positionKey(update.deviceId, update.sessionId)
            ] ??
              this.saved.baseline ??
              0),
        )
        .map((update) => update.sessionId),
    );
  }

  read(deviceId: string, sessionId: string, renderedThrough: number): boolean {
    const update = this.latest.get(sessionId);
    const key = positionKey(deviceId, sessionId);
    if (
      !update ||
      update.deviceId !== deviceId ||
      update.sequence > renderedThrough ||
      update.sequence <= (this.saved.positions[key] ?? this.saved.baseline ?? 0)
    )
      return false;
    this.saved.positions[key] = update.sequence;
    this.persist();
    return true;
  }

  markAllRead(sessions: { id: string; deviceId: string }[]): boolean {
    let changed = false;
    for (const session of sessions) {
      const update = this.latest.get(session.id);
      if (update?.deviceId !== session.deviceId) continue;
      const key = positionKey(session.deviceId, session.id);
      if (
        update.sequence <=
        (this.saved.positions[key] ?? this.saved.baseline ?? 0)
      )
        continue;
      this.saved.positions[key] = update.sequence;
      changed = true;
    }
    if (changed) this.persist();
    return changed;
  }

  clear() {
    this.saved = empty();
    this.latest.clear();
    this.writable = true;
    try {
      this.storage.removeItem(this.key);
    } catch {
      // Clearing an input's in-memory view does not depend on local storage.
    }
  }
}
