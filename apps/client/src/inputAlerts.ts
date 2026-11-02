import type { Event } from "@mewdex/sdk";
export { inputScope as alertScope } from "./inputScope.ts";

export interface InputAlert {
  key: string;
  deviceId: string;
  sessionId: string;
  title: string;
  body: string;
}

export interface AlertStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

interface SavedAlerts {
  enabled: boolean;
  cursor: number;
  seen: string[];
}
const object = (value: unknown): Record<string, unknown> | null =>
  value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;

// Summaries never include model text, questions, commands, attachment paths, or
// secret answer fields. The same classifier serves desktop, mobile, and web.
export function inputAlert(event: Event): InputAlert | null {
  if (!event.sessionId || !event.deviceId) return null;
  const payload = object(event.payload);
  if (!payload) return null;
  let reference: [string, string], body: string;
  if (event.kind === "engine.turn/completed") {
    const turn = object(payload.turn);
    if (!turn || typeof turn.id !== "string" || !turn.id) return null;
    const status = turn.status;
    if (
      status !== "completed" &&
      status !== "failed" &&
      status !== "interrupted"
    )
      return null;
    reference = ["turn", turn.id];
    body =
      status === "completed"
        ? "一轮对话已完成。"
        : status === "failed"
          ? "一轮对话未能完成，请查看执行结果。"
          : "一轮对话已中断。";
  } else if (event.kind === "interaction.updated") {
    if (
      payload.state !== "pending" ||
      payload.sessionId !== event.sessionId ||
      typeof payload.id !== "string" ||
      !payload.id
    )
      return null;
    reference = ["request", payload.id];
    body = "一个对话有待处理请求。";
  } else return null;
  const key = JSON.stringify([event.deviceId, event.sessionId, ...reference]);
  if (key.length > 512) return null;
  return {
    key,
    deviceId: event.deviceId,
    sessionId: event.sessionId,
    title: "MewDex",
    body,
  };
}

// Checkpoints let an enabled input catch up after suspension. Semantic keys
// deduplicate native callbacks and freshly republished interaction snapshots.
export class InputAlertLedger {
  private value: SavedAlerts = { enabled: false, cursor: 0, seen: [] };
  private observed = 0;
  private dirty = false;
  private removed = false;
  private readonly key: string;
  constructor(
    private readonly storage: AlertStorage,
    scope: string,
  ) {
    this.key = `mewdex.alerts.v1:${scope}`;
    try {
      const raw = storage.getItem(this.key);
      if (raw && raw.length <= 512 * 1024) {
        const parsed = JSON.parse(raw) as SavedAlerts;
        if (
          typeof parsed.enabled === "boolean" &&
          Number.isSafeInteger(parsed.cursor) &&
          parsed.cursor >= 0 &&
          Array.isArray(parsed.seen) &&
          parsed.seen.length <= 512 &&
          parsed.seen.every(
            (item) => typeof item === "string" && item.length <= 512,
          )
        )
          this.value = parsed;
      }
    } catch {
      /* Storage availability does not affect conversations. */
    }
  }
  get enabled(): boolean {
    return this.value.enabled;
  }
  bootstrap(latest: number): number {
    if (!Number.isSafeInteger(latest) || latest < 0) return this.value.cursor;
    if (this.value.cursor > latest) {
      this.value.cursor = latest;
      this.value.seen = [];
    }
    this.observed = latest;
    if (!this.enabled) this.value.cursor = latest;
    this.dirty = true;
    this.flush();
    return this.value.cursor;
  }
  checkpoint(sequence: number) {
    if (!Number.isSafeInteger(sequence) || sequence < 0) return;
    this.observed = Math.max(this.observed, sequence);
    if (sequence > this.value.cursor) {
      this.value.cursor = sequence;
      this.dirty = true;
    }
  }
  setEnabled(enabled: boolean) {
    this.removed = false;
    this.value.enabled = enabled;
    this.value.cursor = this.observed;
    this.value.seen = [];
    this.dirty = true;
    this.flush();
  }
  accept(event: Event): InputAlert | null {
    if (
      !Number.isSafeInteger(event.sequence) ||
      event.sequence <= this.value.cursor
    )
      return null;
    this.checkpoint(event.sequence);
    if (!this.enabled) return null;
    const alert = inputAlert(event);
    if (!alert || this.value.seen.includes(alert.key)) return null;
    this.value.seen.push(alert.key);
    this.value.seen = this.value.seen.slice(-512);
    this.dirty = true;
    // Persist intent before invoking the system. A lost OS response does not
    // result in the same notification being resent after an input restart.
    this.flush();
    return alert;
  }
  clear() {
    this.value = { enabled: false, cursor: this.observed, seen: [] };
    this.removed = true;
    this.dirty = false;
    try {
      this.storage.removeItem(this.key);
    } catch {
      /* Continue without storage. */
    }
  }
  flush() {
    if (this.removed || !this.dirty) return;
    try {
      this.storage.setItem(this.key, JSON.stringify(this.value));
      this.dirty = false;
    } catch {
      /* Continue in memory. */
    }
  }
}
