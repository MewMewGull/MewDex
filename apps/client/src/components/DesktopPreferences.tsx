import { t } from "../localization.ts";
import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PanelBottom } from "lucide-react";
import { inputCapabilities } from "../localDevice";

interface Preferences {
  closeToTray: boolean;
  minimizeToTray: boolean;
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
  async function save(patch: Partial<Preferences>) {
    if (!preferences.data || busy) return;
    setBusy(true);
    setError("");
    try {
      cache.setQueryData(
        key,
        await invoke<Preferences>("desktop_preferences_save", {
          preferences: { ...preferences.data, ...patch },
        }),
      );
    } catch (cause) {
      setError(typeof cause === "string" ? cause : t("无法保存桌面设置"));
    } finally {
      setBusy(false);
    }
  }
  if (!preferences.data && !preferences.error) return null;
  return (
    <section className="network-section">
      <h3>
        <PanelBottom size={15} />
        {t("窗口与系统托盘")}
      </h3>
      {preferences.data && (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={preferences.data.minimizeToTray}
              disabled={busy}
              onChange={(event) =>
                void save({ minimizeToTray: event.target.checked })
              }
            />
            {t("最小化时隐藏至托盘")}
          </label>
          <p className="muted">
            {t(
              "启用后，点击最小化按钮时窗口将隐藏到系统托盘而不是任务栏。点击托盘图标可重新打开。",
            )}
          </p>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={preferences.data.closeToTray}
              disabled={busy}
              onChange={(event) =>
                void save({ closeToTray: event.target.checked })
              }
            />
            {t("关闭窗口时保留在系统托盘")}
          </label>
          <p className="muted">
            {t(
              "保留工作台后可继续接收提醒。右键托盘图标可退出工作台，执行服务继续独立运行。",
            )}
          </p>
        </>
      )}
      {(error || preferences.error) && (
        <p className="error" role="alert">
          {t(error || String(preferences.error))}
        </p>
      )}
    </section>
  );
}
