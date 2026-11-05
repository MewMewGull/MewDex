import { t } from "../localization.ts";
import * as Dialog from "@radix-ui/react-dialog";
import { useRef } from "react";
import {
  Archive,
  Activity,
  FileDiff,
  FileClock,
  FolderOpen,
  ListOrdered,
  MoreHorizontal,
  Search,
  Settings2,
  Square,
  Terminal,
  X,
} from "lucide-react";
import type { Session } from "@mewdex/sdk";
import { sessionManagementActions } from "../components/sessionManagementActions";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function ConversationActions({
  open,
  onOpenChange,
  session,
  canManage,
  canStop,
  onPolicy,
  onOutput,
  onQueue,
  onStop,
  onRename,
  onPin,
  onArchive,
  onLibrary,
  onActivity,
  onReceipts,
  onFiles,
  onSearch,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  session?: Session;
  canManage: boolean;
  canStop: boolean;
  onPolicy: () => void;
  onOutput: (kind: "terminal" | "diff") => void;
  onQueue: () => void;
  onStop: () => void;
  onRename: (origin: HTMLElement | null) => void;
  onPin: () => void;
  onArchive: () => void;
  onLibrary: () => void;
  onActivity: () => void;
  onReceipts: () => void;
  onFiles: () => void;
  onSearch: () => void;
}) {
  const trigger = useRef<HTMLButtonElement>(null);
  const returnFocus = useDialogReturnFocus();
  function choose(action: () => void) {
    onOpenChange(false);
    action();
  }
  const organize = {
    rename: () => onRename(trigger.current),
    pin: onPin,
    archive: onArchive,
  };
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Trigger
        ref={trigger}
        className="icon-button"
        aria-label={t("对话选项")}
      >
        <MoreHorizontal size={20} />
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog conversation-actions-dialog"
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("对话选项")}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭对话选项")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t("整理对话，查看输出与执行方式。")}
          </Dialog.Description>
          <div className="conversation-action-list">
            {session && (
              <button onClick={() => choose(onSearch)}>
                <Search size={18} />
                <span>
                  {t("查找对话内容")}
                  <small>Ctrl+F</small>
                </span>
              </button>
            )}
            {session && (
              <>
                {sessionManagementActions(session).map((action) => (
                  <button
                    key={action.id}
                    disabled={!canManage}
                    onClick={() => choose(organize[action.id])}
                  >
                    <action.Icon size={18} />
                    <span>
                      {action.label}
                      {action.description && (
                        <small>{action.description}</small>
                      )}
                    </span>
                  </button>
                ))}
                <div className="conversation-action-divider" />
              </>
            )}
            <button onClick={() => choose(onPolicy)}>
              <Settings2 size={18} />
              <span>
                {t("执行策略")}
                <small>{t("工作范围、提示词与版本历史")}</small>
              </span>
            </button>
            <button
              disabled={!session}
              onClick={() => choose(() => onOutput("terminal"))}
            >
              <Terminal size={18} />
              <span>{t("终端输出")}</span>
            </button>
            <button disabled={!session} onClick={() => choose(onFiles)}>
              <FolderOpen size={18} />
              <span>
                {t("项目文件")}
                <small>{t("浏览目录与已接收文件")}</small>
              </span>
            </button>
            <button
              disabled={!session}
              onClick={() => choose(() => onOutput("diff"))}
            >
              <FileDiff size={18} />
              <span>{t("代码差异")}</span>
            </button>
            <button disabled={!session} onClick={() => choose(onQueue)}>
              <ListOrdered size={18} />
              <span>{t("排队与执行记录")}</span>
            </button>
            {canStop && (
              <button className="danger-action" onClick={() => choose(onStop)}>
                <Square size={16} />
                <span>{t("停止任务与后台进程")}</span>
              </button>
            )}
            <div className="conversation-action-divider" />
            <button onClick={() => choose(onActivity)}>
              <Activity size={18} />
              <span>{t("任务与请求")}</span>
            </button>
            <button onClick={() => choose(onReceipts)}>
              <FileClock size={18} />
              <span>
                {t("待确认操作")}
                <small>{t("查询发送与控制操作的回执")}</small>
              </span>
            </button>
            <button onClick={() => choose(onLibrary)}>
              <Archive size={18} />
              <span>{t("已归档对话")}</span>
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
