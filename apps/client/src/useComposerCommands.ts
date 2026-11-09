import { useQuery } from "@tanstack/react-query";
import type { Device, MewDexClient, Session } from "@mewdex/sdk";
import { composerCommandEntries } from "./composerCommands";

export function useComposerCommands({
  client,
  device,
  projectId,
  session,
  connected,
  enabled,
}: {
  client: MewDexClient;
  device?: Device;
  projectId: string;
  session?: Session;
  connected: boolean;
  enabled: boolean;
}) {
  const available = enabled && connected && !!device?.online && !!projectId;
  const target = {
    deviceId: device?.id ?? "",
    projectId,
    sessionId: session?.id,
  };
  const runtimeSupported = !!device?.capabilities?.includes("skill-catalog");
  const projectSupported =
    !!device?.roles.includes("coordinator") &&
    !!device.capabilities?.includes("project-harness-v1");
  const runtime = useQuery({
    queryKey: [
      "composer-skills",
      target.deviceId,
      projectId,
      session?.id ?? "",
    ],
    queryFn: () => client.skills(target),
    enabled: available && runtimeSupported,
    staleTime: 30000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const project = useQuery({
    queryKey: ["composer-project-skills", target.deviceId, projectId],
    queryFn: ({ signal }) => client.projectHarness(projectId, 0, signal),
    enabled: available && projectSupported,
    staleTime: 30000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return {
    entries: composerCommandEntries(
      connected && device?.online && !runtime.isError
        ? (runtime.data?.skills ?? [])
        : [],
      connected && device?.online && !project.isError
        ? (project.data?.configuration.skills ?? [])
        : [],
    ),
    loading: runtime.isFetching || project.isFetching,
    error:
      runtime.error?.message ||
      project.error?.message ||
      runtime.data?.errors.join("\n") ||
      "",
    runtimeSupported,
    refresh() {
      if (!available) return;
      if (runtimeSupported) void runtime.refetch();
      if (projectSupported) void project.refetch();
    },
  };
}
