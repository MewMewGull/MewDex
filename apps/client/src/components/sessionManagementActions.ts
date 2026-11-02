import { Archive, ArchiveRestore, Pencil, Pin, PinOff } from "lucide-react";
import type { Session } from "@mewdex/sdk";

export function sessionManagementActions(session: Session) {
  return [
    {
      id: "rename",
      label: "重命名",
      Icon: Pencil,
      description: "",
      patch: null,
    },
    {
      id: "pin",
      label: session.pinned ? "取消置顶" : "置顶对话",
      Icon: session.pinned ? PinOff : Pin,
      description: "",
      patch: { pinned: !session.pinned },
    },
    {
      id: "archive",
      label: session.archived ? "恢复对话" : "归档对话",
      Icon: session.archived ? ArchiveRestore : Archive,
      description: session.archived ? "" : "保留内容，正在执行的任务会继续",
      patch: { archived: !session.archived },
    },
  ] as const;
}
