import { t } from "../localization.ts";
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
            ? t("Hub 离线")
            : account.isPending
              ? t("正在读取账号状态")
              : account.isError
                ? t("暂时无法读取账号状态")
                : account.data?.signedIn
                  ? t("Hub 模型账号已连接")
                  : t("Hub 尚未连接模型账号")}
        </strong>
        <button
          className="icon-button"
          aria-label={t("刷新模型账号状态")}
          disabled={!online || busy}
          onClick={() => void account.refetch()}
        >
          <RefreshCw size={13} />
        </button>
      </div>
      {account.data?.signedIn && (
        <p>
          {account.data.type === "chatgpt"
            ? t("ChatGPT 登录")
            : account.data.type === "apiKey"
              ? t("Hub 已配置 API 凭据")
              : account.data.type}
          {account.data.planType ? ` · ${account.data.planType}` : ""}
          {account.data.email ? ` · ${account.data.email}` : ""}
        </p>
      )}
      {account.error && <div className="error">{account.error.message}</div>}
      {account.data && !account.data.loginSupported && (
        <p>
          {account.data.signedIn
            ? t("Hub 使用已有的模型账号配置。")
            : t("请在 Hub 所在机器完成模型账号登录后刷新。")}
        </p>
      )}
      {account.data?.loginSupported && !account.data.signedIn && !pending && (
        <button
          className="primary"
          disabled={!online || busy}
          onClick={() => void login()}
        >
          {busy ? t("正在准备登录…") : t("登录官方 ChatGPT")}
        </button>
      )}
      {pending && !account.data?.signedIn && (
        <div className="device-login">
          <p>
            {t(
              "在官方网页完成登录，并输入下面的一次性代码。无需安装或登录官方 App。",
            )}
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
            {t("打开官方登录网页 ")}
            <ExternalLink size={13} />
          </button>
          <button
            className="cancel-login"
            disabled={busy}
            onClick={() => void cancel()}
          >
            {t("取消本次登录")}
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
