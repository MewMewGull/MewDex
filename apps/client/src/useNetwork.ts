import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  MewDexClient,
  MewDexError,
  type Identity,
  type NetworkSnapshot,
  type Event,
} from "@mewdex/sdk";
import { useWorkspace } from "./state";
import { orderedDevices } from "./workspaceNavigation";
import { subscribeResume } from "./platform";
import {
  connectionRetryDelay,
  readConnection,
  retryableConnectionRead,
} from "./connectionRead";

// Shared input synchronization. Platform storage and execution stay outside it.
export function useNetwork(
  client: MewDexClient,
  initialIdentity: Identity,
  clearDrafts: () => void,
  restoreSelection?: (snapshot: NetworkSnapshot) => void,
  conversations?: {
    bootstrap: (snapshot: NetworkSnapshot) => void;
    event: (event: Event) => void;
  },
) {
  const cache = useQueryClient();
  const sessionId = useWorkspace((state) => state.sessionId);
  const connectionState = useWorkspace((state) => state.connectionState);
  const [ready, setReady] = useState(false);
  const [unauthorized, setUnauthorized] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [reconnecting, setReconnecting] = useState(true);
  const [capabilities, setCapabilities] = useState<string[]>([]);
  const cursor = useRef(0);
  const identity = useRef(initialIdentity);
  const clear = useRef(clearDrafts);
  clear.current = clearDrafts;
  const restore = useRef(restoreSelection);
  restore.current = restoreSelection;
  const conversationObserver = useRef(conversations);
  conversationObserver.current = conversations;
  const initialSnapshotApplied = useRef(false);
  const failures = useRef(0);
  useEffect(() => subscribeResume(() => setAttempt((value) => value + 1)), []);

  function forgetVisibleData() {
    setCapabilities([]);
    cache.clear();
    useWorkspace.getState().reset();
    clear.current();
  }
  function apply(snapshot: NetworkSnapshot, initial: boolean) {
    const changed =
      identity.current.id !== snapshot.identity.id ||
      JSON.stringify(identity.current.access) !==
        JSON.stringify(snapshot.identity.access);
    if (changed || snapshot.cursor < cursor.current) forgetVisibleData();
    identity.current = snapshot.identity;
    setCapabilities(snapshot.capabilities ?? []);
    cache.setQueryData(["devices"], snapshot.devices);
    cache.setQueryData(["sessions"], snapshot.sessions);
    cache.setQueryData(["identity"], snapshot.identity);
    conversationObserver.current?.bootstrap(snapshot);
    if (initial && !initialSnapshotApplied.current) {
      initialSnapshotApplied.current = true;
      restore.current?.(snapshot);
    }
    const current = useWorkspace.getState();
    if (!snapshot.devices.some((device) => device.id === current.deviceId))
      current.selectDevice(orderedDevices(snapshot.devices)[0]?.id ?? "");
    if (
      current.sessionId &&
      !snapshot.sessions.some((session) => session.id === current.sessionId)
    )
      useWorkspace.getState().selectSession("");
    if (initial) cursor.current = snapshot.cursor;
  }
  function fail(value: unknown) {
    if (value instanceof MewDexError && [401, 403].includes(value.status)) {
      forgetVisibleData();
      setUnauthorized(true);
      setReady(false);
    }
    setError(value instanceof Error ? value.message : "连接暂时不可用");
  }
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    setReady(false);
    setUnauthorized(false);
    setError("");
    setReconnecting(true);
    useWorkspace.getState().setConnectionState("connecting");
    void readConnection((signal) => client.bootstrap(signal), controller.signal)
      .then((snapshot) => {
        if (controller.signal.aborted) return;
        failures.current = 0;
        apply(snapshot, true);
        if (attempt > 0) {
          useWorkspace.getState().clearEvents();
          for (const key of [
            "history",
            "continuous-history",
            "interactions",
            "queue",
            "policies",
            "account",
            "account-limits",
            "outputs",
            "project-files",
            "pending-commands",
            "task-plan",
          ])
            void cache.invalidateQueries({ queryKey: [key] });
        }
        setReady(true);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          fail(error);
          useWorkspace.getState().setConnectionState("disconnected");
          const retrying = retryableConnectionRead(error);
          setReconnecting(retrying);
          if (retrying)
            timer = setTimeout(
              () => setAttempt((value) => value + 1),
              connectionRetryDelay(failures.current++),
            );
        }
      });
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [client, attempt]);

  useEffect(() => {
    if (!ready) return;
    const controller = new AbortController();
    let initialState = true;
    const keepConnection =
      useWorkspace.getState().connectionState === "connected";
    void client
      .subscribe({
        after: cursor.current,
        sessionId,
        signal: controller.signal,
        onCursor: (value) => {
          if (!controller.signal.aborted) cursor.current = value;
        },
        onReconnect: async () => {
          const snapshot = await readConnection(
            (signal) => client.bootstrap(signal),
            controller.signal,
          );
          if (controller.signal.aborted) return undefined;
          apply(snapshot, false);
          useWorkspace.getState().clearEvents();
          for (const key of [
            "history",
            "continuous-history",
            "interactions",
            "queue",
            "policies",
            "account",
            "account-limits",
            "outputs",
            "project-files",
            "pending-commands",
            "task-plan",
          ])
            void cache.invalidateQueries({ queryKey: [key] });
          return snapshot.cursor;
        },
        onState: (state) => {
          if (controller.signal.aborted) return;
          const selectingConversation =
            initialState && keepConnection && state === "connecting";
          initialState = false;
          if (selectingConversation) return;
          if (state === "connecting") setReconnecting(true);
          useWorkspace.getState().setConnectionState(state);
          if (state === "unauthorized") {
            forgetVisibleData();
            setUnauthorized(true);
            setReady(false);
          }
        },
        onEvent: (event) => {
          if (controller.signal.aborted) return;
          useWorkspace.getState().appendEvent(event);
          conversationObserver.current?.event(event);
          if (
            [
              "activity.changed",
              "session.updated",
              "session.metadata.updated",
              "interaction.updated",
              "queue.item.updated",
            ].includes(event.kind)
          )
            void cache.invalidateQueries({ queryKey: ["activity"] });
          if (
            event.kind === "session.updated" ||
            event.kind === "session.metadata.updated"
          )
            void cache.invalidateQueries({ queryKey: ["sessions"] });
          if (event.kind === "command.result")
            void cache.invalidateQueries({ queryKey: ["pending-commands"] });
          if (event.kind === "workspace.catalog.updated")
            void cache.invalidateQueries({ queryKey: ["devices"] });
          if (event.kind === "engine.account/rateLimits/updated")
            void cache.invalidateQueries({
              queryKey: ["account-limits", event.deviceId],
            });
          if (event.kind === "file.exported")
            void cache.invalidateQueries({
              queryKey: ["outputs", event.sessionId],
            });
          if (
            [
              "engine.turn/plan/updated",
              "engine.turn/started",
              "engine.turn/completed",
            ].includes(event.kind)
          )
            void cache.invalidateQueries({
              queryKey: ["task-plan", event.sessionId, 0],
              exact: true,
            });
          if (event.kind.startsWith("engine.account/"))
            void cache.invalidateQueries({
              queryKey: ["account", event.deviceId],
            });
          if (event.kind === "interaction.updated")
            void cache.invalidateQueries({
              queryKey: ["interactions", event.sessionId],
            });
          if (event.kind === "policy.snapshot")
            void cache.invalidateQueries({
              queryKey: ["policies", event.sessionId],
            });
          if (event.kind === "queue.item.updated")
            void cache.invalidateQueries({
              queryKey: ["queue", event.sessionId],
            });
        },
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) fail(error);
      });
    return () => controller.abort();
  }, [client, ready, sessionId]);
  return {
    ready,
    connected: ready && connectionState === "connected",
    reconnecting,
    capabilities,
    unauthorized,
    error,
    retry: () => setAttempt((value) => value + 1),
  };
}
