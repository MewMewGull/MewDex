import { useRef, useState } from "react";
import type { Device, NetworkSnapshot } from "@mewdex/sdk";
import { useWorkspace } from "./state";
import type { SendKey } from "./inputKeys";

interface WorkspaceView {
  deviceId: string;
  sessionId: string;
  projects: Record<string, string>;
  navigationVisible: boolean;
  restoreLastLocation: boolean;
  outputWidth: number;
  sidebarWidth: number;
  sendKey: SendKey;
}

const emptyView = (): WorkspaceView => ({
  deviceId: "",
  sessionId: "",
  projects: {},
  navigationVisible: true,
  restoreLastLocation: true,
  outputWidth: 38,
  sidebarWidth: 280,
  sendKey: "enter",
});

function loadView(key: string): { value: WorkspaceView; error: string } {
  try {
    const saved = localStorage.getItem(key);
    return {
      value: { ...emptyView(), ...(saved ? JSON.parse(saved) : {}) },
      error: "",
    };
  } catch {
    return {
      value: emptyView(),
      error: "暂时无法读取本机工作台设置，将从默认设置打开。",
    };
  }
}

// Only input view state is retained. Network access and execution stay elsewhere.
export function useWorkspaceView(scope: string) {
  const storageKey = `mewdex.workspace.v1:${scope}`;
  const [initial] = useState(() => loadView(storageKey));
  const [value, setValue] = useState(initial.value);
  const [error, setError] = useState(initial.error);
  const current = useRef(value);
  const startupSelection = useRef(useWorkspace.getState().selectionVersion);

  function commit(next: WorkspaceView) {
    current.current = next;
    setValue(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
      setError("");
    } catch {
      setError("工作台设置尚未保存，重开时可能回到原设置。");
    }
  }

  return {
    navigationVisible: value.navigationVisible,
    restoreLastLocation: value.restoreLastLocation,
    outputWidth: value.outputWidth,
    sidebarWidth: value.sidebarWidth,
    sendKey: value.sendKey,
    error,
    projectFor(device?: Device) {
      if (!device) return "";
      return (
        device.projects.find(
          (project) => project.id === value.projects[device.id],
        )?.id ??
        device.projects[0]?.id ??
        ""
      );
    },
    selectProject(deviceId: string, projectId: string) {
      commit({
        ...current.current,
        projects: { ...current.current.projects, [deviceId]: projectId },
      });
    },
    toggleNavigation() {
      commit({
        ...current.current,
        navigationVisible: !current.current.navigationVisible,
      });
    },
    setRestoreLastLocation(enabled: boolean) {
      commit({ ...current.current, restoreLastLocation: enabled });
    },
    setOutputWidth(width: number) {
      commit({ ...current.current, outputWidth: width });
    },
    setSidebarWidth(width: number) {
      commit({ ...current.current, sidebarWidth: width });
    },
    setSendKey(sendKey: SendKey) {
      commit({ ...current.current, sendKey });
    },
    rememberSelection(deviceId: string, sessionId: string) {
      if (
        current.current.deviceId !== deviceId ||
        current.current.sessionId !== sessionId
      )
        commit({ ...current.current, deviceId, sessionId });
    },
    restore(snapshot: NetworkSnapshot) {
      const state = useWorkspace.getState();
      // User navigation and notification activation take precedence over a saved view.
      if (
        !current.current.restoreLastLocation ||
        state.deviceId ||
        state.selectionVersion !== startupSelection.current
      )
        return;
      const device = snapshot.devices.find(
        (item) => item.id === current.current.deviceId,
      );
      if (!device) return;
      state.selectDevice(device.id);
      const session = snapshot.sessions.find(
        (item) =>
          item.id === current.current.sessionId && item.deviceId === device.id,
      );
      if (session) useWorkspace.getState().selectSession(session.id);
    },
    reset() {
      const next = emptyView();
      current.current = next;
      setValue(next);
      try {
        localStorage.removeItem(storageKey);
        setError("");
      } catch {
        setError("暂时无法清除本机工作台设置。");
      }
    },
  };
}
