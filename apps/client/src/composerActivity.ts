export interface ComposerSubmission {
  key: string;
}

type ActivitySnapshot = {
  epoch: number;
  sending: ReadonlySet<string>;
  errors: ReadonlyMap<string, string>;
};

// Input-side activity belongs to a draft, not to the whole workspace. This is
// transient UI state; durable command IDs and unknown results stay with the Hub.
export class ComposerActivity {
  private active = new Map<string, ComposerSubmission>();
  private errors = new Map<string, string>();
  private listeners = new Set<() => void>();
  private snapshot: ActivitySnapshot = {
    epoch: 0,
    sending: new Set(),
    errors: new Map(),
  };

  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  begin(key: string): ComposerSubmission | undefined {
    if (this.active.has(key)) return undefined;
    const submission = { key };
    this.active.set(key, submission);
    this.errors.delete(key);
    this.publish();
    return submission;
  }

  move(from: string, to: string) {
    if (from === to) return;
    const submission = this.active.get(from);
    const error = this.errors.get(from);
    if (!submission && error === undefined) return;
    this.active.delete(from);
    this.errors.delete(from);
    if (submission) {
      submission.key = to;
      this.active.set(to, submission);
    }
    if (error !== undefined) this.errors.set(to, error);
    this.publish();
  }

  finish(submission: ComposerSubmission, error?: string) {
    if (this.active.get(submission.key) !== submission) return false;
    this.active.delete(submission.key);
    if (error !== undefined) {
      if (error) this.errors.set(submission.key, error);
      else this.errors.delete(submission.key);
    }
    this.publish();
    return true;
  }

  report(key: string, error: string, epoch = this.snapshot.epoch) {
    if (epoch !== this.snapshot.epoch || (this.errors.get(key) ?? "") === error)
      return;
    if (error) this.errors.set(key, error);
    else this.errors.delete(key);
    this.publish();
  }

  clear() {
    this.active.clear();
    this.errors.clear();
    this.publish(this.snapshot.epoch + 1);
  }

  private publish(epoch = this.snapshot.epoch) {
    this.snapshot = {
      epoch,
      sending: new Set(this.active.keys()),
      errors: new Map(this.errors),
    };
    for (const listener of this.listeners) listener();
  }
}
