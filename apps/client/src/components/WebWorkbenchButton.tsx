import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { MewDexClient } from "@mewdex/sdk";
import { deviceRequest, inputCapabilities } from "../localDevice";
import { readConnection, sameNetworkAddress } from "../connectionRead";
import { openWebWorkbench } from "../platform";

export function WebWorkbenchButton({ baseURL }: { baseURL: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const status = useQuery({
    queryKey: ["web-workbench", baseURL],
    queryFn: async ({ signal }) => {
      const info = await readConnection(
        (requestSignal) =>
          new MewDexClient({ baseURL }).checkServer(requestSignal),
        signal,
      );
      let deviceId: string | undefined;
      const capabilities = await inputCapabilities();
      if (capabilities.localService && capabilities.helperPresent) {
        const { snapshot } = await deviceRequest({ operation: "status" });
        if (
          snapshot.hub &&
          snapshot.baseUrl &&
          sameNetworkAddress(baseURL, snapshot.baseUrl) &&
          snapshot.operations?.includes("web.apply")
        )
          deviceId = snapshot.deviceId;
      }
      return { info, deviceId };
    },
    retry: false,
    staleTime: 3000,
  });
  const supported = !!status.data?.info.capabilities?.includes("web-ui");
  const enabled = !!status.data?.info.capabilities?.includes("web-ui-enabled");
  async function open() {
    setError("");
    try {
      await openWebWorkbench(baseURL);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }
  async function setEnabled(value: boolean) {
    const deviceId = status.data?.deviceId;
    if (!deviceId || busy) return;
    setBusy(true);
    setError("");
    try {
      await deviceRequest({
        operation: "web.apply",
        expectedDeviceId: deviceId,
        webWorkbench: { enabled: value },
      });
      await status.refetch({ throwOnError: true });
      if (value) await openWebWorkbench(baseURL);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="web-workbench-controls">
      <strong>
        网页版 ·{" "}
        {status.isPending ? "正在读取…" : enabled ? "运行中" : "已停止"}
      </strong>
      {enabled && <code>{baseURL}</code>}
      <div className="web-workbench-actions">
        {enabled ? (
          <>
            <button
              type="button"
              className="settings-link"
              disabled={busy}
              onClick={() => void open()}
            >
              <ExternalLink size={14} /> 在浏览器中打开
            </button>
            {status.data?.deviceId && (
              <button
                type="button"
                className="settings-link"
                disabled={busy}
                onClick={() => void setEnabled(false)}
              >
                停止网页版
              </button>
            )}
          </>
        ) : (
          <button
            type="button"
            className="settings-link"
            disabled={busy || !supported || !status.data?.deviceId}
            onClick={() => void setEnabled(true)}
          >
            {busy ? "正在启动…" : "启动网页版"}
          </button>
        )}
      </div>
      <p className="muted">
        启动后可通过浏览器登录，退出桌面 APP 后继续在后台运行。
      </p>
      {!status.isPending && !status.error && !supported && (
        <p className="muted">当前中央后端需要更新后才能提供网页版。</p>
      )}
      {supported && !enabled && !status.data?.deviceId && (
        <p className="muted">请在中央后端所在电脑的桌面 APP 中启动网页版。</p>
      )}
      {(error || status.error) && (
        <p className="error" role="alert">
          {error || String(status.error)}
        </p>
      )}
    </div>
  );
}
