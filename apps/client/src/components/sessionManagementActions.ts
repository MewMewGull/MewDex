import { t } from "../localization.ts";
import { Archive, ArchiveRestore, Pencil, Pin, PinOff } from "lucide-react";
import type { Session } from "@mewdex/sdk";

export function sessionManagementActions(session: Session) {
  return [
    {
      id: "rename",
      label: t("重命名"),
      Icon: Pencil,
      description: "",
      patch: null,
    },
    {
      id: "pin",
      label: session.pinned ? t("取消置顶") : t("置顶对话"),
      Icon: session.pinned ? PinOff : Pin,
      description: "",
      patch: { pinned: !session.pinned },
    },
    {
      id: "archive",
      label: session.archived ? t("恢复对话") : t("归档对话"),
      Icon: session.archived ? ArchiveRestore : Archive,
      description: session.archived ? "" : t("保留内容，正在执行的任务会继续"),
      patch: { archived: !session.archived },
    },
  ] as const;
}
