import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { DeviceLogin, MewDexClient } from "@mewdex/sdk";

// The composer and settings observe one account result and one pending login.
// Reading state never starts a login or refreshes model credentials.
export function useModelAccount(
  client: MewDexClient,
  deviceId: string,
  enabled: boolean,
) {
  const cache = useQueryClient();
  const loginKey = ["account-login", deviceId];
  const pendingLogin = useQuery<DeviceLogin | null>({
    queryKey: loginKey,
    queryFn: async () => null,
    initialData: null,
    enabled: false,
    gcTime: Infinity,
  });
  const pending = pendingLogin.data;
  const account = useQuery({
    queryKey: ["account", deviceId],
    queryFn: () => client.modelAccount(deviceId),
    enabled: enabled && !!deviceId,
    staleTime: 30000,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: pending ? 3000 : 30000,
  });
  useEffect(() => {
    if (account.data?.signedIn) cache.setQueryData(loginKey, null);
  }, [account.data?.signedIn, deviceId]);
  return {
    account,
    pending,
    setPending(value: DeviceLogin | null) {
      cache.setQueryData(loginKey, value);
    },
  };
}
