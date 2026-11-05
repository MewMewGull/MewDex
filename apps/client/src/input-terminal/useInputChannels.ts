import { useEffect } from "react";
import { invoke } from "@tauri-apps/api/core";
import { inputCapabilities } from "../localDevice";
import type { Connection } from "../platform";

let epoch = 0;

export function useInputChannels(connection: Connection | null) {
  useEffect(() => {
    const current = ++epoch;
    let active = true;
    const configure = async () => {
      if (!(await inputCapabilities()).messagingBridge || !active) return;
      const credentials = connection?.credentials;
      await invoke("channel_connection", {
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
      });
    };
    // Native state deduplicates a live process and restarts an exited helper.
    void configure().catch(() => {});
    const timer = setInterval(() => void configure().catch(() => {}), 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [connection]);
}
