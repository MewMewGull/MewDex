import { useEffect, useLayoutEffect, useState } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { getCurrentWindow, type Window } from "@tauri-apps/api/window";
import type { UnlistenFn } from "@tauri-apps/api/event";
import { Network } from "lucide-react";

export function DesktopTitlebar() {
  const [appWindow, setAppWindow] = useState<Window | null>(null);
  const [maximized, setMaximized] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [focused, setFocused] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!isTauri()) return;
    const current = getCurrentWindow();
    let disposed = false;
    const listeners: UnlistenFn[] = [];
    const subscribe = (subscription: Promise<UnlistenFn>) => {
      void subscription
        .then((unlisten) => {
          if (disposed) unlisten();
          else listeners.push(unlisten);
        })
        .catch(console.error);
    };
    const update = async () => {
      const [isMaximized, isFullscreen, isFocused] = await Promise.all([
        current.isMaximized(),
        current.isFullscreen(),
        current.isFocused(),
      ]);
      if (disposed) return;
      setMaximized(isMaximized);
      setFullscreen(isFullscreen);
      setFocused(isFocused);
    };
    void current
      .isDecorated()
      .then(async (decorated) => {
        if (disposed || decorated) return;
        // Only the Windows configuration replaces native window decorations.
        setAppWindow(current);
        subscribe(current.onResized(() => void update().catch(console.error)));
        subscribe(
          current.onFocusChanged(({ payload }) => {
            if (!disposed) setFocused(payload);
          }),
        );
        await update();
      })
      .catch(console.error);
    return () => {
      disposed = true;
      for (const unlisten of listeners) unlisten();
    };
  }, []);

  const visible = !!appWindow && !fullscreen;
  useLayoutEffect(() => {
    if (visible) document.documentElement.dataset.customTitlebar = "true";
    return () => {
      delete document.documentElement.dataset.customTitlebar;
    };
  }, [visible]);

  async function run(action: () => Promise<void>) {
    setError("");
    try {
      await action();
    } catch {
      setError("窗口操作失败，请重试");
    }
  }

  if (!visible || !appWindow) return null;
  return (
    <header
      className="desktop-titlebar"
      data-focused={focused}
      aria-label="窗口标题栏"
    >
      <div className="desktop-titlebar-drag" data-tauri-drag-region>
        <Network size={14} aria-hidden="true" />
        <span>MewDex</span>
      </div>
      {error && (
        <span className="desktop-titlebar-error" role="alert">
          {error}
        </span>
      )}
      <div className="desktop-window-controls">
        <button
          type="button"
          aria-label="最小化"
          title="最小化"
          onClick={() => void run(() => appWindow.minimize())}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
            <path d="M0 5.5h10" />
          </svg>
        </button>
        <button
          type="button"
          aria-label={maximized ? "还原窗口" : "最大化"}
          title={maximized ? "还原窗口" : "最大化"}
          onClick={() => void run(() => appWindow.toggleMaximize())}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
            {maximized ? (
              <path d="M2.5 2.5v-2h7v7h-2m-7-5h7v7h-7z" />
            ) : (
              <rect x="0.5" y="0.5" width="9" height="9" />
            )}
          </svg>
        </button>
        <button
          type="button"
          className="desktop-window-close"
          aria-label="关闭窗口"
          title="关闭窗口"
          onClick={() => void run(() => appWindow.close())}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
            <path d="m.5.5 9 9m0-9-9 9" />
          </svg>
        </button>
      </div>
    </header>
  );
}
