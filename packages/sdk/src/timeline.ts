import type { Event } from "./protocol.ts";

/** A bounded event window shared by input clients; durable history stays on the Hub. */
export class EventWindow {
  private entries = new Map<string, { event: Event; bytes: number }>();
  private bytes = 0;
  private clipped = false;
  private lastSequence = 0;
  constructor(
    private readonly limits = { maxEvents: 1500, maxBytes: 8 << 20 },
  ) {
    if (limits.maxEvents < 1 || limits.maxBytes < 1)
      throw new Error("Invalid event window limits");
  }
  clear(): void {
    this.entries.clear();
    this.bytes = 0;
    this.clipped = false;
    this.lastSequence = 0;
  }
  append(event: Event): boolean {
    if (this.entries.has(event.id)) return false;
    if (event.sequence < this.lastSequence) {
      this.merge([event]);
      return true;
    }
    const bytes = JSON.stringify(event).length * 2;
    this.entries.set(event.id, { event, bytes });
    this.bytes += bytes;
    this.lastSequence = event.sequence;
    this.trim();
    return true;
  }
  merge(events: Event[]): void {
    for (const event of events) {
      if (!this.entries.has(event.id)) {
        const bytes = JSON.stringify(event).length * 2;
        this.entries.set(event.id, { event, bytes });
        this.bytes += bytes;
      }
    }
    this.entries = new Map(
      [...this.entries].sort(
        (a, b) => a[1].event.sequence - b[1].event.sequence,
      ),
    );
    this.lastSequence = Math.max(
      0,
      ...[...this.entries.values()].map((entry) => entry.event.sequence),
    );
    this.trim();
  }
  private trim(): void {
    // One oversized event is still readable; every page remains navigable.
    while (
      this.entries.size > 1 &&
      (this.entries.size > this.limits.maxEvents ||
        this.bytes > this.limits.maxBytes)
    ) {
      const first = this.entries.entries().next().value;
      if (!first) break;
      this.entries.delete(first[0]);
      this.bytes -= first[1].bytes;
      this.clipped = true;
    }
  }
  get events(): Event[] {
    return [...this.entries.values()].map((entry) => entry.event);
  }
  get truncated(): boolean {
    return this.clipped;
  }
}
