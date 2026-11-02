import { useEffect, useMemo, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type {
  CommandResult,
  Interaction,
  MewDexClient,
  Session,
} from "@mewdex/sdk";
import {
  InteractionResponses,
  initialInteractionResponse,
  interactionResponseKey,
  type QuestionValue,
} from "./interactionResponses";

export function useInteractionResponses(
  client: MewDexClient,
  session: Session | undefined,
  requests: Interaction[] | undefined,
) {
  const cache = useQueryClient();
  const responses = useMemo(
    () =>
      new InteractionResponses(
        {
          respond: (target, id, reply) => client.respond(target, id, reply),
          result: (id) => client.result(id),
        },
        (sessionId) => {
          void cache.invalidateQueries({
            queryKey: ["interactions", sessionId],
            exact: true,
          });
        },
      ),
    [client, cache],
  );
  const snapshot = useSyncExternalStore(
    responses.subscribe,
    responses.getSnapshot,
    responses.getSnapshot,
  );
  useEffect(() => () => responses.clear(), [responses]);
  useEffect(() => {
    if (session && requests)
      responses.retain(session, requests, snapshot.epoch);
  }, [responses, session?.id, session?.deviceId, requests, snapshot.epoch]);
  return {
    view: (target: Session, request: Interaction) =>
      snapshot.entries.get(interactionResponseKey(target, request)) ??
      initialInteractionResponse(request),
    change: (
      target: Session,
      request: Interaction,
      field: string,
      value: QuestionValue | undefined,
    ) => responses.change(target, request, field, value),
    reply: (target: Session, request: Interaction, action: string) =>
      responses.reply(target, request, action),
    observe: (target: Session, request: Interaction) =>
      responses.observe(target, request),
    resolve: (commandId: string, result: CommandResult) =>
      responses.resolve(commandId, result, snapshot.epoch),
    report: (target: Session, request: Interaction, error: string) =>
      responses.report(target, request, error, snapshot.epoch),
    clear: () => responses.clear(),
  };
}
