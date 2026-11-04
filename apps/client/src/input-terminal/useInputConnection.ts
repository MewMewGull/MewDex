import { useCallback, useEffect, useRef, useState } from "react";
import { MewDexClient, MewDexError, PROTOCOL_VERSION } from "@mewdex/sdk";
import { credentialStore, subscribeResume, type Connection } from "../platform";
import {
  connectionRetryDelay,
  readConnection,
  retryableConnectionRead,
} from "../connectionRead";

export interface ConnectionIssue {
  kind: "storage" | "configuration" | "network" | "authorization";
  message: string;
  retrying: boolean;
}

// Restoring an input is separate from creating a pairing. Network recovery
// reads the existing identity; save and clear remain explicit user operations.
export function useInputConnection(onChanged: () => void) {
  const [connection, setConnection] = useState<Connection | null>(null);
  const [issue, setIssue] = useState<ConnectionIssue | null>(null);
  const [address, setAddress] = useState<string>();
  const [checking, setChecking] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const epoch = useRef(0);
  const failures = useRef(0);
  const current = useRef<AbortController | null>(null);
  const scheduled = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const changed = useRef(onChanged);
  changed.current = onChanged;

  const cancelRead = useCallback(() => {
    current.current?.abort();
    if (scheduled.current !== undefined) clearTimeout(scheduled.current);
    scheduled.current = undefined;
  }, []);
  const retry = useCallback(() => {
    cancelRead();
    setAttempt((value) => value + 1);
  }, [cancelRead]);

  useEffect(() => {
    const controller = new AbortController();
    current.current = controller;
    const requestEpoch = ++epoch.current;
    const active = () =>
      !controller.signal.aborted && epoch.current === requestEpoch;
    let stage: ConnectionIssue["kind"] = "storage";
    setChecking(true);
    void (async () => {
      const saved = await credentialStore.load();
      if (!active()) return;
      setAddress(saved?.baseURL);
      if (!saved) return null;
      stage = "configuration";
      const client = new MewDexClient({
        baseURL: saved.baseURL,
        token: saved.credentials.token,
      });
      stage = "network";
      const identity = await readConnection(
        (signal) => client.identity(signal),
        controller.signal,
      );
      return {
        baseURL: saved.baseURL,
        credentials: {
          token: saved.credentials.token,
          identity,
          protocolVersion: PROTOCOL_VERSION,
        },
      } satisfies Connection;
    })()
      .then((value) => {
        if (!active()) return;
        failures.current = 0;
        setConnection(value ?? null);
        setIssue(null);
      })
      .catch((cause: unknown) => {
        if (!active()) return;
        const unauthorized =
          cause instanceof MewDexError && [401, 403].includes(cause.status);
        const retrying = stage === "network" && retryableConnectionRead(cause);
        setIssue({
          kind: unauthorized ? "authorization" : stage,
          message: cause instanceof Error ? cause.message : String(cause),
          retrying,
        });
        if (retrying)
          scheduled.current = setTimeout(
            retry,
            connectionRetryDelay(failures.current++),
          );
      })
      .finally(() => {
        if (active()) setChecking(false);
      });
    return () => {
      controller.abort();
      cancelRead();
    };
  }, [attempt, cancelRead, retry]);

  const problem = useRef(issue);
  problem.current = issue;
  useEffect(
    () =>
      subscribeResume(() => {
        if (problem.current?.kind === "network") retry();
      }),
    [retry],
  );

  async function save(value: Connection) {
    await credentialStore.save(value);
    epoch.current++;
    cancelRead();
    changed.current();
    failures.current = 0;
    setAddress(value.baseURL);
    setConnection(value);
    setIssue(null);
    setChecking(false);
  }
  async function disconnect() {
    try {
      await credentialStore.clear();
      epoch.current++;
      cancelRead();
      changed.current();
      failures.current = 0;
      setAddress(undefined);
      setConnection(null);
      setIssue(null);
      setChecking(false);
    } catch (cause) {
      setIssue({
        kind: "storage",
        message: cause instanceof Error ? cause.message : String(cause),
        retrying: false,
      });
    }
  }
  function chooseNetwork() {
    epoch.current++;
    cancelRead();
    setConnection(null);
    setIssue(null);
    setChecking(false);
    setAddress(undefined);
  }
  return {
    connection,
    issue,
    address,
    checking,
    loading: checking && !issue,
    retry,
    save,
    disconnect,
    chooseNetwork,
  };
}
