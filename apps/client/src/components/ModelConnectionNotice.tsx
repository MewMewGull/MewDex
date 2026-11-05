import { t } from "../localization.ts";
import { KeyRound, RefreshCw } from "lucide-react";
import type { ModelAccount } from "@mewdex/sdk";

export function ModelConnectionNotice({
  account,
  pending,
  error,
  refreshing,
  onManage,
  onRefresh,
}: {
  account?: ModelAccount;
  pending: boolean;
  error: boolean;
  refreshing: boolean;
  onManage: () => void;
  onRefresh: () => void;
}) {
  if (account?.signedIn || (!account && !error)) return null;
  const unread = error && !account;
  return (
    <div className="model-connection-notice" role="status">
      <KeyRound size={19} />
      <div>
        <strong>
          {unread
            ? t("暂时无法确认模型账号状态")
            : pending
              ? t("等待完成模型登录")
              : t("先连接模型账号")}
        </strong>
        <p>
          {unread
            ? t("可以继续编辑草稿，刷新状态或到设置中查看详情。")
            : pending
              ? t("完成设置页中的网页登录后，这里会自动更新。草稿会继续保留。")
              : account?.loginSupported
                ? t("Hub 已在线。连接共用模型账号后，即可开始对话。")
                : t("请在 Hub 所在机器完成模型账号登录后刷新。")}
        </p>
      </div>
      <div className="model-connection-actions">
        <button
          className="icon-button"
          aria-label={t("刷新 Hub 的模型账号状态")}
          disabled={refreshing}
          onClick={onRefresh}
        >
          <RefreshCw size={15} />
        </button>
        <button className="model-connection-manage" onClick={onManage}>
          {unread
            ? t("账号设置")
            : pending
              ? t("继续登录")
              : account?.loginSupported
                ? t("连接账号")
                : t("查看设置")}
        </button>
      </div>
    </div>
  );
}
