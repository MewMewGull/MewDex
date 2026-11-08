import { useEffect } from "react";
import { invoke, isTauri } from "@tauri-apps/api/core";
import { inputCapabilities } from "../localDevice";
import type { Connection } from "../platform";
import { nextHostedChannelRequest } from "../inputChannelHost";

let epoch = 0;

export function useInputChannels(connection: Connection | null) {
  useEffect(() => {
    const current = ++epoch;
    const hostedRequest = isTauri() ? null : nextHostedChannelRequest();
    let active = true;
    const configure = async () => {
      if (!(await inputCapabilities()).messagingBridge || !active) return;
      const credentials = connection?.credentials;
      const options = {
        epoch: current,
        connection:
          connection && credentials?.identity.role === "admin"
            ? {
                baseURL: connection.baseURL,
                token: credentials.token,
                hubDeviceId: credentials.hubDeviceId,
                identityId: credentials.identity.id,
              }
            : null,
      };
      if (isTauri()) await invoke("channel_connection", options);
      else if (options.connection) {
        const response = await fetch("/__mewdex/channels", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${options.connection.token}`,
          },
          body: JSON.stringify({
            operation: "connect",
            ...hostedRequest,
            baseURL: options.connection.baseURL,
            hubDeviceId: options.connection.hubDeviceId,
            identityId: options.connection.identityId,
          }),
        });
        if (!response.ok) throw new Error(await response.text());
        const result = (await response.json()) as { settingsKey?: string };
        if (result.settingsKey)
          sessionStorage.setItem("mewdex.web-panel-key", result.settingsKey);
      }
    };
    // Each input owner deduplicates its connection and reconnects in the background.
    void configure().catch(() => {});
    const timer = setInterval(() => void configure().catch(() => {}), 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [connection]);
}
