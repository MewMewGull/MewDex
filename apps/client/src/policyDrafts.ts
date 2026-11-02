import type { Policy } from "@mewdex/sdk";
import { samePolicy } from "./policyValues.ts";

export interface PolicyDraft {
  policy: Policy;
  baseline: Policy;
  basis: string;
}

// Unapplied form values remain in this input window. Applied policy and its
// version continue to belong to the executor/Hub, never to this draft store.
export class PolicyDrafts {
  private entries = new Map<string, PolicyDraft>();
  private listeners = new Set<() => void>();

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): ReadonlyMap<string, PolicyDraft> => this.entries;

  change(key: string, initial: PolicyDraft, patch: Partial<Policy>) {
    const current = this.entries.get(key) ?? initial;
    const next = { ...current, policy: { ...current.policy, ...patch } };
    const entries = new Map(this.entries);
    if (samePolicy(next.policy, next.baseline)) entries.delete(key);
    else entries.set(key, next);
    this.publish(entries);
  }

  reset(key: string) {
    if (!this.entries.has(key)) return;
    const entries = new Map(this.entries);
    entries.delete(key);
    this.publish(entries);
  }

  complete(key: string, submitted: PolicyDraft) {
    if (this.entries.get(key) === submitted) this.reset(key);
  }

  clear() {
    this.publish(new Map());
  }

  private publish(entries: Map<string, PolicyDraft>) {
    this.entries = entries;
    for (const listener of this.listeners) listener();
  }
}
