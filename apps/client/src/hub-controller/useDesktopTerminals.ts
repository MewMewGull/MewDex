import { t } from "../localization.ts";
import { useEffect, useRef, useState } from "react";
import { prepareDesktopTerminals } from "./desktopTerminals";
import { deviceRequest, inputCapabilities } from "../localDevice";
import { MewDexClient } from "@mewdex/sdk";
import { isLocalHub } from "./hubConnection";

interface DesktopTerminals {
  available: boolean;
  pending: boolean;
  baseURL?: string;
  setupRequired?: boolean;
  started?: boolean;
  error?: string;
  hubDeviceId?: string;
  hostedLocally?: boolean;
}

interface ConnectionTarget {
  enabled: boolean;
  baseURL?: string;
  confirmed: boolean;
}

async function openDesktopHub(
  target: ConnectionTarget,
): Promise<DesktopTerminals> {
  let available = false;
  try {
    const capabilities = await inputCapabilities();
    available = capabilities.localService;
    if (!available) return { available, pending: false };
    if (!capabilities.helperPresent)
      throw new Error(
        t("当前安装缺少终端服务程序，请安装完整的 MewDex 应用。"),
      );
    let hubDeviceId: string | undefined;
    if (target.baseURL && target.confirmed) {
      const server = await new MewDexClient({ baseURL: target.baseURL })
        .checkServer(AbortSignal.timeout(10000))
        .catch(() => undefined);
      hubDeviceId = server?.hubDeviceId;
    }
    const result = await prepareDesktopTerminals(
      deviceRequest,
      target.baseURL
        ? { ...target, baseURL: target.baseURL, hubDeviceId }
        : undefined,
    );
    const { snapshot } = result;
    return {
      available,
      pending: false,
      baseURL:
        target.baseURL ??
        result.existingHub?.baseUrl ??
        (snapshot.hub ? snapshot.baseUrl : snapshot.hubUrl),
      setupRequired: ["unconfigured", "setup-pending"].includes(snapshot.state),
      started: result.started,
      hubDeviceId,
      hostedLocally:
        !!target.baseURL && isLocalHub(snapshot, target.baseURL, hubDeviceId),
    };
  } catch (cause) {
    return {
      available,
      pending: false,
      error: cause instanceof Error ? cause.message : String(cause),
    };
  }
}

export function useDesktopTerminals(
  onStarted: () => void,
  target: ConnectionTarget,
) {
  const [state, setState] = useState<DesktopTerminals>({
    available: false,
    pending: true,
  });
  const [attempt, setAttempt] = useState(0);
  const operation = useRef<{
    key: string;
    baseURL?: string;
    notified: boolean;
    result: Promise<DesktopTerminals>;
  } | null>(null);
  const started = useRef(onStarted);
  started.current = onStarted;
  useEffect(() => {
    let active = true;
    void inputCapabilities()
      .then((capabilities) => {
        if (active)
          setState((current) => ({
            ...current,
            available: capabilities.localService,
          }));
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    if (!target.enabled) {
      setState((current) =>
        current.pending ? { ...current, pending: false } : current,
      );
      return;
    }
    if (!target.baseURL && operation.current?.baseURL) {
      setState((current) => ({ ...current, pending: false }));
      return;
    }
    if (!operation.current)
      setState((current) => ({ ...current, pending: true }));
    let active = true;
    // Strict Mode and re-renders share one service start attempt per launch.
    const key = JSON.stringify([target.baseURL, target.confirmed, attempt]);
    if (operation.current?.key !== key) {
      const previous = operation.current?.result ?? Promise.resolve();
      operation.current = {
        key,
        baseURL: target.baseURL,
        notified: false,
        result: previous.then(() => openDesktopHub(target)),
      };
    }
    const pendingOperation = operation.current;
    void pendingOperation.result.then((result) => {
      if (!active) return;
      setState(result);
      if (result.started && !pendingOperation.notified) {
        pendingOperation.notified = true;
        started.current();
      }
    });
    return () => {
      active = false;
    };
  }, [attempt, target.enabled, target.baseURL, target.confirmed]);
  return {
    ...state,
    connected(baseURL: string) {
      setState((current) => ({
        ...current,
        baseURL,
        setupRequired: false,
        error: undefined,
      }));
    },
    retry() {
      setState((current) => ({ ...current, pending: true, error: undefined }));
      setAttempt((value) => value + 1);
    },
  };
}
