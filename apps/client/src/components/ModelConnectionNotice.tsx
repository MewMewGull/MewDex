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
            ? "暂时无法确认模型账号状态"
            : pending
              ? "等待完成模型登录"
              : "先连接模型账号"}
        </strong>
        <p>
          {unread
            ? "可以继续编辑草稿，刷新状态或到设置中查看详情。"
            : pending
              ? "完成设置页中的网页登录后，这里会自动更新。草稿会继续保留。"
              : account?.loginSupported
                ? "设备已在线。连接模型账号后，即可在这里开始对话。"
                : "此设备沿用已有的模型配置，需要设备所有者完成登录后刷新。"}
        </p>
      </div>
      <div className="model-connection-actions">
        <button
          className="icon-button"
          aria-label="刷新当前设备的模型账号状态"
          disabled={refreshing}
          onClick={onRefresh}
        >
          <RefreshCw size={15} />
        </button>
        <button className="model-connection-manage" onClick={onManage}>
          {unread
            ? "账号设置"
            : pending
              ? "继续登录"
              : account?.loginSupported
                ? "连接账号"
                : "查看设置"}
        </button>
      </div>
    </div>
  );
}
