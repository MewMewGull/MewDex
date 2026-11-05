import { t } from "./localization.ts";
import { useRef, useState, type SetStateAction } from "react";
import type { Attachment } from "@mewdex/sdk";
import {
  composerKey,
  completeComposerDraft,
  type ComposerDraft,
  type ComposerDrafts,
} from "./composerDrafts";

type Draft = ComposerDraft;
const empty = (): Draft => ({ text: "", uploads: [] });
type Drafts = ComposerDrafts;
function loadDrafts(key: string): { drafts: Drafts; error: string } {
  try {
    const value = localStorage.getItem(key);
    return { drafts: value ? (JSON.parse(value) as Drafts) : {}, error: "" };
  } catch {
    return {
      drafts: {},
      error: t(
        "无法恢复本机草稿。原记录已保留，本次输入暂存在当前窗口；可在设置中清空草稿后重新保存。",
      ),
    };
  }
}
// Store unsent input only. A restored draft never creates or repeats a command.
export function useComposerDrafts(
  scope: string,
  deviceId: string,
  sessionId: string,
  projectId: string,
) {
  const storageKey = `mewdex.drafts.v1:${scope}`;
  const [initial] = useState(() => loadDrafts(storageKey));
  const [drafts, setDrafts] = useState<Drafts>(initial.drafts);
  const [persistenceError, setPersistenceError] = useState(initial.error);
  const current = useRef(drafts);
  const writable = useRef(!initial.error);
  const key = composerKey(deviceId, sessionId, projectId);
  const draft = drafts[key] ?? empty();
  function persist(next: Drafts) {
    if (!writable.current) return;
    try {
      if (Object.keys(next).length)
        localStorage.setItem(storageKey, JSON.stringify(next));
      else localStorage.removeItem(storageKey);
      setPersistenceError("");
    } catch {
      setPersistenceError(t("草稿尚未保存到本机，关闭应用前请保留输入内容。"));
    }
  }
  function commit(next: Drafts) {
    const nonempty = Object.fromEntries(
      Object.entries(next).filter(
        ([, item]) => item.text.length || item.uploads.length,
      ),
    );
    current.current = nonempty;
    setDrafts(nonempty);
    persist(nonempty);
  }
  function update(change: (value: Draft) => Draft) {
    commit({
      ...current.current,
      [key]: change(current.current[key] ?? empty()),
    });
  }
  return {
    key,
    ...draft,
    count: Object.keys(drafts).length,
    all: drafts as Readonly<ComposerDrafts>,
    keys: new Set(Object.keys(drafts)),
    sessionIds: new Set(
      Object.keys(drafts)
        .map((target) => (JSON.parse(target) as string[])[1])
        .filter(Boolean),
    ),
    persistenceError,
    setText(value: SetStateAction<string>) {
      update((old) => ({
        ...old,
        text: typeof value === "function" ? value(old.text) : value,
      }));
    },
    setUploads(value: SetStateAction<Attachment[]>) {
      update((old) => ({
        ...old,
        uploads: typeof value === "function" ? value(old.uploads) : value,
      }));
    },
    appendUpload(target: string, attachment: Attachment) {
      const value = current.current[target] ?? empty();
      if (value.uploads.some((item) => item.id === attachment.id)) return;
      commit({
        ...current.current,
        [target]: { ...value, uploads: [...value.uploads, attachment] },
      });
    },
    reset() {
      writable.current = true;
      commit({});
    },
    move(from: string, to: string) {
      const next = {
        ...current.current,
        [to]: current.current[from] ?? empty(),
      };
      delete next[from];
      commit(next);
    },
    complete(target: string, text: string, attachmentIds: string[]) {
      const value = current.current[target];
      if (!value) return;
      commit({
        ...current.current,
        [target]: completeComposerDraft(value, text, attachmentIds),
      });
    },
  };
}
