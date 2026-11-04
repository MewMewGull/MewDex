import { useEffect, useMemo, useRef, useState } from "react";
import { MewDexError, type MewDexClient } from "@mewdex/sdk";
import { InputAlertLedger, alertScope } from "../inputAlerts";
import {
  notificationAdapter,
  type AlertPermission,
} from "../notificationAdapter";
import { subscribeResume } from "../platform";
import { sameSelection, selectionOf, useWorkspace } from "../state";

const storage = {
  getItem: (key: string) => window.localStorage.getItem(key),
  setItem: (key: string, value: string) =>
    window.localStorage.setItem(key, value),
  removeItem: (key: string) => window.localStorage.removeItem(key),
};
const kinds = ["engine.turn/completed", "interaction.updated"];

export function useInputAlerts(
  client: MewDexClient,
  baseURL: string,
  identityId: string,
  ready: boolean,
) {
  const scope = useMemo(
    () => alertScope(baseURL, identityId),
    [baseURL, identityId],
  );
  const ledger = useMemo(() => new InputAlertLedger(storage, scope), [scope]);
  const [enabled, setEnabled] = useState(ledger.enabled);
  const [permission, setPermission] = useState<AlertPermission | "loading">(
    "loading",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const alive = useRef(true);
  const generation = useRef(0);
  const permissionEpoch = useRef(0);
  const openRequest = useRef(0);
  const stream = useRef<AbortController | undefined>(undefined);
  const pendingOpen = useRef<
    { deviceId: string; sessionId: string } | undefined
  >(undefined);
  const networkReady = useRef(ready);
  networkReady.current = ready;

  async function open(deviceId: string, sessionId: string) {
    if (!alive.current) return;
    const request = ++openRequest.current;
    if (!networkReady.current) {
      // Native action delivery can precede the first network snapshot. Retain
      // only the latest navigation hint, then authorize it after reconnecting.
      pendingOpen.current = { deviceId, sessionId };
      return;
    }
    const revision = generation.current;
    const selection = selectionOf(useWorkspace.getState());
    try {
      // A notification is a hint, never an authorization. Old toasts must also
      // pass a fresh read of the currently authorized sessions.
      const sessions = await client.sessions();
      if (
        !alive.current ||
        revision !== generation.current ||
        request !== openRequest.current ||
        !sameSelection(selection, useWorkspace.getState()) ||
        !networkReady.current ||
        !sessions.some(
          (session) =>
            session.id === sessionId && session.deviceId === deviceId,
        )
      )
        return;
      const state = useWorkspace.getState();
      if (state.deviceId !== deviceId) state.selectDevice(deviceId);
      if (useWorkspace.getState().sessionId !== sessionId)
        useWorkspace.getState().selectSession(sessionId);
      await notificationAdapter.activate();
    } catch {
      if (
        alive.current &&
        revision === generation.current &&
        request === openRequest.current &&
        sameSelection(selection, useWorkspace.getState())
      )
        setError("暂时无法打开该提醒的对话，请刷新连接后查看。");
    }
  }

  useEffect(() => {
    alive.current = true;
    generation.current++;
    setPermission("loading");
    setEnabled(ledger.enabled);
    setError("");
    setBusy(false);
    let disposed = false;
    let off: (() => void) | undefined;
    const readPermission = async () => {
      const epoch = ++permissionEpoch.current;
      try {
        const value = await notificationAdapter.permission();
        if (!disposed && epoch === permissionEpoch.current)
          setPermission(value);
        return value;
      } catch {
        if (!disposed && epoch === permissionEpoch.current) {
          setPermission("unsupported");
          setError("当前输入端暂时无法使用系统提醒。");
        }
        return "unsupported" as const;
      }
    };
    void readPermission().then(async (value) => {
      if (disposed || value === "unsupported") return;
      try {
        const unlisten = await notificationAdapter.listen(
          scope,
          (deviceId, sessionId) => {
            if (!disposed) void open(deviceId, sessionId);
          },
        );
        if (disposed) unlisten();
        else off = unlisten;
      } catch {
        if (!disposed) setError("系统提醒的对话入口暂时不可用。");
      }
    });
    // OS settings can change while the app is suspended. Reading permission on
    // return never prompts; requesting it remains an explicit checkbox action.
    const resume = subscribeResume(() => {
      void readPermission();
    });
    const flush = () => ledger.flush();
    const timer = setInterval(flush, 1000);
    window.addEventListener("pagehide", flush);
    return () => {
      disposed = true;
      alive.current = false;
      generation.current++;
      pendingOpen.current = undefined;
      stream.current?.abort();
      off?.();
      resume();
      clearInterval(timer);
      window.removeEventListener("pagehide", flush);
      ledger.flush();
    };
  }, [client, scope, ledger]);

  useEffect(() => {
    if (!ready || !pendingOpen.current) return;
    const target = pendingOpen.current;
    pendingOpen.current = undefined;
    void open(target.deviceId, target.sessionId);
  }, [client, scope, ready]);

  function clear() {
    generation.current++;
    pendingOpen.current = undefined;
    stream.current?.abort();
    ledger.clear();
    setEnabled(false);
    setBusy(false);
    setError("");
  }

  useEffect(() => {
    if (!ready || !enabled || !ledger.enabled || permission !== "granted")
      return;
    const controller = new AbortController();
    stream.current = controller;
    const revision = generation.current;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const current = () =>
      alive.current &&
      !controller.signal.aborted &&
      revision === generation.current;
    const bootstrap = async () => {
      const snapshot = await client.bootstrap(controller.signal);
      return current() ? ledger.bootstrap(snapshot.cursor) : undefined;
    };
    void bootstrap()
      .then(async (after) => {
        if (after === undefined || !current()) return;
        setError("");
        await client.subscribe({
          after,
          kinds,
          signal: controller.signal,
          onReconnect: bootstrap,
          onCursor: (cursor) => {
            if (current()) ledger.checkpoint(cursor);
          },
          onEvent: (event) => {
            if (!current()) return;
            const alert = ledger.accept(event);
            if (!alert) return;
            const state = useWorkspace.getState();
            if (
              document.visibilityState === "visible" &&
              document.hasFocus() &&
              state.sessionId === alert.sessionId &&
              state.following
            )
              return;
            const failed = () => {
              if (current())
                setError("系统未能显示提醒，请检查此输入端的通知设置。");
            };
            void notificationAdapter
              .send(
                scope,
                alert,
                () => {
                  if (alive.current && revision === generation.current)
                    void open(alert.deviceId, alert.sessionId);
                },
                failed,
                controller.signal,
              )
              .catch(failed);
          },
        });
      })
      .catch((value: unknown) => {
        if (!current()) return;
        if (value instanceof MewDexError && [401, 403].includes(value.status)) {
          clear();
          return;
        }
        setError("提醒同步暂时不可用，正在重新连接。");
        timer = setTimeout(() => {
          if (current()) setRetry((value) => value + 1);
        }, 3000);
      });
    return () => {
      controller.abort();
      if (stream.current === controller) stream.current = undefined;
      if (timer !== undefined) clearTimeout(timer);
      ledger.flush();
    };
  }, [client, scope, ledger, ready, enabled, permission, retry]);

  async function toggle(next: boolean) {
    if (busy) return;
    const revision = ++generation.current;
    stream.current?.abort();
    setBusy(true);
    setError("");
    try {
      if (next) {
        permissionEpoch.current++;
        const value = await notificationAdapter.request();
        if (!alive.current || revision !== generation.current) return;
        permissionEpoch.current++;
        setPermission(value);
        if (value !== "granted") {
          setError("系统提醒未开启。允许通知后，可以再次开启工作台提醒。");
          return;
        }
        const snapshot = await client.bootstrap();
        if (!alive.current || revision !== generation.current) return;
        // Enabling starts here; intentionally disabled periods are not replayed.
        ledger.bootstrap(snapshot.cursor);
      }
      ledger.setEnabled(next);
      setEnabled(next);
    } catch {
      if (alive.current && revision === generation.current)
        setError("提醒设置未能保存，请重试。");
    } finally {
      if (alive.current && revision === generation.current) setBusy(false);
    }
  }

  return { enabled, permission, busy, error, toggle, clear };
}

export type InputAlerts = ReturnType<typeof useInputAlerts>;
