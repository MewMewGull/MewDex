import { useState } from "react";
import { ExternalLink, LogIn, RefreshCw } from "lucide-react";
import { MewDexClient } from "@mewdex/sdk";
import { openOfficialLogin } from "../platform";
import { useModelAccount } from "./useModelAccount";

export function ModelAccountCard({
  client,
  deviceId,
  online,
}: {
  client: MewDexClient;
  deviceId: string;
  online: boolean;
}) {
  const { account, pending, setPending } = useModelAccount(
    client,
    deviceId,
    online,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function login() {
    setBusy(true);
    setError("");
    try {
      setPending(await client.startModelLogin(deviceId));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function cancel() {
    if (!pending) return;
    setBusy(true);
    setError("");
    try {
      await client.cancelModelLogin(deviceId, pending.loginId);
      setPending(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="model-account-card">
      <div className="account-state">
        <LogIn size={16} />
        <strong>
          {!online
            ? "设备离线"
            : account.isPending
              ? "正在读取账号状态"
              : account.isError
                ? "暂时无法读取账号状态"
                : account.data?.signedIn
                  ? "模型账号已连接"
                  : "尚未连接模型账号"}
        </strong>
        <button
          className="icon-button"
          aria-label="刷新模型账号状态"
          disabled={!online || busy}
          onClick={() => void account.refetch()}
        >
          <RefreshCw size={13} />
        </button>
      </div>
      {account.data?.signedIn && (
        <p>
          {account.data.type === "chatgpt"
            ? "ChatGPT 登录"
            : account.data.type === "apiKey"
              ? "设备已有 API 凭据"
              : account.data.type}
          {account.data.planType ? ` · ${account.data.planType}` : ""}
          {account.data.email ? ` · ${account.data.email}` : ""}
        </p>
      )}
      {account.error && <div className="error">{account.error.message}</div>}
      {account.data && !account.data.loginSupported && (
        <p>
          {account.data.signedIn
            ? "此设备使用本机已有的模型账号配置。"
            : "此设备沿用已有的模型配置，需要设备所有者完成登录后刷新。"}
        </p>
      )}
      {account.data?.loginSupported && !account.data.signedIn && !pending && (
        <button
          className="primary"
          disabled={!online || busy}
          onClick={() => void login()}
        >
          {busy ? "正在准备登录…" : "登录官方 ChatGPT"}
        </button>
      )}
      {pending && !account.data?.signedIn && (
        <div className="device-login">
          <p>
            在官方网页完成登录，并输入下面的一次性代码。无需安装或登录官方 App。
          </p>
          <code>{pending.userCode}</code>
          <button
            className="official-login-button"
            onClick={() =>
              void openOfficialLogin(pending.verificationUrl).catch(
                (e: Error) => setError(e.message),
              )
            }
          >
            打开官方登录网页 <ExternalLink size={13} />
          </button>
          <button
            className="cancel-login"
            disabled={busy}
            onClick={() => void cancel()}
          >
            取消本次登录
          </button>
        </div>
      )}
      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}
    </div>
  );
}
