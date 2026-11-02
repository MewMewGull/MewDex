import { useEffect, useMemo, useSyncExternalStore } from "react";
import { ComposerActivity, type ComposerSubmission } from "./composerActivity";

export function useComposerActivity(draftKey: string) {
  const activity = useMemo(() => new ComposerActivity(), []);
  const snapshot = useSyncExternalStore(
    activity.subscribe,
    activity.getSnapshot,
    activity.getSnapshot,
  );
  useEffect(() => () => activity.clear(), [activity]);
  return {
    busy: snapshot.sending.has(draftKey),
    error: snapshot.errors.get(draftKey) ?? "",
    setError: (error: string) =>
      activity.report(draftKey, error, snapshot.epoch),
    report: (key: string, error: string) =>
      activity.report(key, error, snapshot.epoch),
    begin: () => activity.begin(draftKey),
    finish: (submission: ComposerSubmission, error?: string) =>
      activity.finish(submission, error),
    move: (from: string, to: string) => activity.move(from, to),
    clear: () => activity.clear(),
  };
}
