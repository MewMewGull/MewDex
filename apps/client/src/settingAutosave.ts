export type SavePhase =
  | "idle"
  | "pending"
  | "saving"
  | "saved"
  | "invalid"
  | "error";
export interface SaveState {
  phase: SavePhase;
  pending: boolean;
  error: string;
}

// Coalesce typing, serialize writes and retain edits made during an older save.
// A failed write is observed by the caller and is never retried automatically.
export class SettingAutosave<T> {
  private desired: T;
  private confirmed: string;
  private valid = true;
  private failed?: string;
  private phase: SavePhase = "idle";
  private error = "";
  private active = false;
  private timer?: ReturnType<typeof setTimeout>;
  private running?: Promise<void>;
  private listeners = new Set<(state: SaveState) => void>();

  constructor(
    initial: T,
    private save: (value: T) => Promise<unknown>,
    private delay = 350,
  ) {
    this.desired = initial;
    this.confirmed = JSON.stringify(initial);
  }
  get state(): SaveState {
    return {
      phase: this.phase,
      pending: this.active || this.timer !== undefined,
      error: this.error,
    };
  }
  get dirty() {
    return JSON.stringify(this.desired) !== this.confirmed;
  }
  subscribe(listener: (state: SaveState) => void) {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }
  private publish() {
    for (const listener of this.listeners) listener(this.state);
  }
  private clearTimer() {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }

  change(value: T, valid = true) {
    this.desired = value;
    this.valid = valid;
    this.failed = undefined;
    this.error = "";
    this.clearTimer();
    if (!valid) this.phase = "invalid";
    else if (this.active) this.phase = "saving";
    else if (!this.dirty) this.phase = "saved";
    else {
      this.phase = "pending";
      this.timer = setTimeout(() => {
        this.timer = undefined;
        void this.flush();
      }, this.delay);
    }
    this.publish();
  }

  // Polling and older acknowledgments must not replace an unfinished edit.
  adopt(value: T): boolean {
    if (
      this.state.pending ||
      this.phase === "invalid" ||
      this.phase === "error"
    )
      return false;
    this.desired = value;
    this.confirmed = JSON.stringify(value);
    return true;
  }

  flush(): Promise<void> {
    this.clearTimer();
    if (this.running) return this.running;
    if (
      !this.valid ||
      !this.dirty ||
      this.failed === JSON.stringify(this.desired)
    ) {
      this.publish();
      return Promise.resolve();
    }
    this.active = true;
    this.phase = "saving";
    this.publish();
    this.running = this.drain().finally(() => {
      this.active = false;
      this.running = undefined;
      this.publish();
    });
    return this.running;
  }
  private async drain() {
    while (this.valid && this.dirty) {
      const value = this.desired,
        key = JSON.stringify(value);
      try {
        await this.save(value);
      } catch (cause) {
        if (JSON.stringify(this.desired) !== key) {
          this.confirmed = "\u0000unconfirmed";
          continue;
        }
        this.failed = key;
        this.phase = "error";
        this.error = cause instanceof Error ? cause.message : String(cause);
        return;
      }
      this.confirmed = key;
      this.failed = undefined;
      this.error = "";
    }
    this.phase = this.valid ? "saved" : "invalid";
  }
}
