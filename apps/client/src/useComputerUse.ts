import {
  useIsMutating,
  useMutation,
  useMutationState,
  useQueries,
  useQueryClient,
} from "@tanstack/react-query";
import {
  commandID,
  type ComputerSession,
  type Device,
  type MewDexClient,
  type Project,
  type Session,
} from "@mewdex/sdk";
import {
  computerPresentation,
  computerTargets,
  type ComputerTarget,
} from "./computerUse";

export function useComputerUse({
  scope,
  client,
  project,
  session,
  devices,
  connected,
}: {
  scope: string;
  client: MewDexClient;
  project?: Project;
  session?: Session;
  devices: Device[];
  connected: boolean;
}) {
  const cache = useQueryClient();
  const targets = computerTargets(project, devices);
  // Capabilities belong to the target executor, not the coordinating Hub.
  // A new conversation can show guidance without creating or requesting a grant.
  const queries = useQueries({
    queries: targets.map((target) => ({
      queryKey: ["computer", scope, project?.id, session?.id, target.deviceId],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        client.computerStatus(
          project!.id,
          session!.id,
          target.directory,
          signal,
        ),
      enabled:
        connected &&
        !!target.device?.online &&
        target.supported &&
        !!session &&
        !!project,
      staleTime: 1500,
      retry: false,
      refetchInterval:
        session?.state === "running" || session?.state === "waiting"
          ? 2000
          : 5000,
    })),
  });
  const mutationKey = ["computer-stop", scope, project?.id, session?.id];
  const stopping = useIsMutating({ mutationKey }) > 0;
  type Stop = {
    target: ComputerTarget;
    control: ComputerSession;
    projectId: string;
    conversationId: string;
    queryKey: unknown[];
  };
  const mutation = useMutation({
    mutationKey,
    mutationFn: (input: Stop) =>
      client.stopComputer(
        input.projectId,
        input.conversationId,
        input.target.directory,
        { id: "cu_ui_" + commandID(), sessionId: input.control.id },
      ),
    onSuccess: (status, input) => {
      cache.setQueryData(input.queryKey, status);
      void cache.invalidateQueries({ queryKey: input.queryKey });
    },
    onError: (_error, input) => {
      // A lost stop reply is not evidence that the window is still being driven.
      void cache.invalidateQueries({ queryKey: input.queryKey });
    },
  });
  const latestStop = useMutationState({
    filters: { mutationKey },
    select: (item) => ({
      error: item.state.error,
      input: item.state.variables as Stop | undefined,
    }),
  }).at(-1);
  const entries = targets.map((target, index) => {
    const query = queries[index];
    const view = computerPresentation({
      target,
      connected,
      conversationId: session?.id,
      status: query.data,
      loading: !!session && query.isFetching && !query.data,
      error: !!query.error,
    });
    return {
      ...target,
      view,
      query,
      stopError:
        latestStop?.error &&
        latestStop.input?.target.deviceId === target.deviceId &&
        latestStop.input.control.id === view.current?.id &&
        !["stopped", "expired"].includes(view.state)
          ? latestStop.error.message
          : "",
    };
  });
  return {
    entries,
    stopping,
    stop(entry: (typeof entries)[number]) {
      if (
        !project ||
        !session ||
        !entry.view.canStop ||
        !entry.view.current ||
        stopping
      )
        return;
      mutation.mutate({
        target: entry,
        control: entry.view.current,
        projectId: project.id,
        conversationId: session.id,
        queryKey: ["computer", scope, project.id, session.id, entry.deviceId],
      });
    },
  };
}
