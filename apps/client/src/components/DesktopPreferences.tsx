import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PanelBottom } from "lucide-react";
import { inputCapabilities } from "../localDevice";

interface Preferences {
  closeToTray: boolean;
}
const key = ["desktop-preferences"];

export function DesktopPreferences() {
  const cache = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const preferences = useQuery({
    queryKey: key,
    queryFn: async () =>
      (await inputCapabilities()).desktopPreferences
        ? invoke<Preferences>("desktop_preferences")
        : null,
    staleTime: Infinity,
  });
  async function save(closeToTray: boolean) {
    setBusy(true);
    setError("");
    try {
      cache.setQueryData(
        key,
        await invoke<Preferences>("desktop_preferences_save", {
          preferences: { closeToTray },
        }),
      );
    } catch (cause) {
      setError(typeof cause === "string" ? cause : "无法保存桌面设置");
    } finally {
      setBusy(false);
    }
  }
  if (!preferences.data && !preferences.error) return null;
  return (
    <section className="network-section">
      <h3>
        <PanelBottom size={15} />
        窗口与系统托盘
      </h3>
      {preferences.data && (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={preferences.data.closeToTray}
              disabled={busy}
              onChange={(event) => void save(event.target.checked)}
            />
            关闭窗口时保留在系统托盘
          </label>
          <p className="muted">
            保留工作台后可继续接收提醒。右键托盘图标可退出工作台，执行服务继续独立运行。
          </p>
        </>
      )}
      {(error || preferences.error) && (
        <p className="error" role="alert">
          {error || String(preferences.error)}
        </p>
      )}
    </section>
  );
}
