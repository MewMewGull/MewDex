import { useEffect, useState, useSyncExternalStore } from "react";
import type { Policy } from "@mewdex/sdk";
import { PolicyDrafts, type PolicyDraft } from "./policyDrafts";
import { useComposerActivity } from "./useComposerActivity";
import {
  sameSelection,
  selectionOf,
  useWorkspace,
  type WorkspaceSelection,
} from "./state";

export function usePolicyEditor(
  key: string,
  policy: Policy,
  basis: string,
  selection: WorkspaceSelection,
) {
  const [drafts] = useState(() => new PolicyDrafts());
  const entries = useSyncExternalStore(
    drafts.subscribe,
    drafts.getSnapshot,
    drafts.getSnapshot,
  );
  const activity = useComposerActivity(key);
  useEffect(() => () => drafts.clear(), [drafts]);
  const entry = entries.get(key) ?? { policy, baseline: policy, basis };
  const requestedSelection = selectionOf(selection);
  return {
    key,
    entry,
    draft: entry.policy,
    basis: entry.basis,
    dirty: entries.has(key),
    stale: entries.has(key) && entry.basis !== basis,
    isCurrent: () => sameSelection(requestedSelection, useWorkspace.getState()),
    busy: activity.busy,
    error: activity.error,
    begin: activity.begin,
    finish: activity.finish,
    report: activity.setError,
    change: (patch: Partial<Policy>) => drafts.change(key, entry, patch),
    reset: () => {
      drafts.reset(key);
      activity.setError("");
    },
    complete: (target: string, submitted: PolicyDraft) =>
      drafts.complete(target, submitted),
    clear: () => {
      drafts.clear();
      activity.clear();
    },
  };
}

export type PolicyEditor = ReturnType<typeof usePolicyEditor>;
