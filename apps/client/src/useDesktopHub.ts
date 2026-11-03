import { useEffect, useRef, useState } from "react";
import { prepareDesktopHub } from "./desktopHubStartup";
import { deviceRequest, inputCapabilities } from "./localDevice";

interface DesktopHub {
  available: boolean;
  pending: boolean;
  baseURL?: string;
  setupRequired?: boolean;
  started?: boolean;
  error?: string;
}

async function openDesktopHub(): Promise<DesktopHub> {
  let available = false;
  try {
    const capabilities = await inputCapabilities();
    available = capabilities.localService;
    if (!available) return { available, pending: false };
    if (!capabilities.helperPresent)
      throw new Error("当前安装缺少中央后端程序，请安装完整的 MewDex 应用。");
    const result = await prepareDesktopHub(deviceRequest);
    const { snapshot } = result;
    return {
      available,
      pending: false,
      baseURL:
        result.existingHub?.baseUrl ??
        (snapshot.hub ? snapshot.baseUrl : undefined),
      setupRequired: ["unconfigured", "setup-pending"].includes(snapshot.state),
      started: result.started,
    };
  } catch (cause) {
    return {
      available,
      pending: false,
      error: cause instanceof Error ? cause.message : String(cause),
    };
  }
}

export function useDesktopHub(onStarted: () => void) {
  const [state, setState] = useState<DesktopHub>({
    available: false,
    pending: true,
  });
  const [attempt, setAttempt] = useState(0);
  const operation = useRef<Promise<DesktopHub> | null>(null);
  const started = useRef(onStarted);
  started.current = onStarted;
  useEffect(() => {
    let active = true;
    // Strict Mode and re-renders share one service start attempt per launch.
    operation.current ??= openDesktopHub();
    void operation.current.then((result) => {
      if (!active) return;
      setState(result);
      if (result.started) started.current();
    });
    return () => {
      active = false;
    };
  }, [attempt]);
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
      operation.current = null;
      setState((current) => ({ ...current, pending: true, error: undefined }));
      setAttempt((value) => value + 1);
    },
  };
}
