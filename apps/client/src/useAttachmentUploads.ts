import { useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import type { Attachment, MewDexClient } from "@mewdex/sdk";
import { useQueryClient } from "@tanstack/react-query";
import { AttachmentUploads, type UploadTarget } from "./attachmentUploads";
import { canPreviewImage } from "./attachmentFiles";

export function useAttachmentUploads(
  client: MewDexClient,
  draftKey: string,
  onUploaded: (key: string, attachment: Attachment) => void,
) {
  const cache = useQueryClient();
  const completed = useRef(onUploaded);
  completed.current = onUploaded;
  const queue = useMemo(
    () =>
      new AttachmentUploads(
        (file, target, signal) =>
          client.upload(file, file.name, target, signal),
        (target, attachment, file) => {
          if (canPreviewImage(attachment.mediaType))
            cache.setQueryData(["attachment-content", attachment.id], file);
          completed.current(target.draftKey, attachment);
        },
      ),
    [client, cache],
  );
  const items = useSyncExternalStore(
    queue.subscribe,
    queue.getSnapshot,
    queue.getSnapshot,
  );
  useEffect(() => () => queue.clear(), [queue]);
  const current = items.filter((item) => item.target.draftKey === draftKey);
  return {
    all: items,
    items: current,
    pending: current.filter(
      (item) => item.state === "queued" || item.state === "uploading",
    ).length,
    blocked: current.length > 0,
    draftKeys: new Set(items.map((item) => item.target.draftKey)),
    sessionIds: new Set(
      items.map((item) => item.target.sessionId).filter(Boolean),
    ),
    add: (files: Iterable<File>, target: UploadTarget) =>
      queue.add(files, target),
    remove: (id: string) => queue.remove(id),
    retry: (id: string) => queue.retry(id),
    move: (from: string, to: string, sessionId: string) =>
      queue.move(from, to, sessionId),
    clear: () => queue.clear(),
  };
}
