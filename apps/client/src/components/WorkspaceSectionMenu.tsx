import { useRef, type KeyboardEvent, type ReactElement } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { ArrowDown, ArrowUp, Check, Pencil, Plus, X } from "lucide-react";
import { t } from "../localization.ts";

export function openSidebarContextMenu(target: HTMLElement) {
  const rect = target.getBoundingClientRect();
  target.dispatchEvent(
    new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
      clientX: rect.left,
      clientY: rect.bottom,
    }),
  );
}

export function sidebarMenuKey(event: KeyboardEvent<HTMLElement>) {
  if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10"))
    return;
  event.preventDefault();
  openSidebarContextMenu(event.currentTarget);
}

export function WorkspaceSectionMenu({
  children,
  name,
  editable,
  canMoveUp,
  canMoveDown,
  onCreate,
  onRename,
  onRemove,
  onMove,
  onRead,
}: {
  children: ReactElement;
  name: string;
  editable: boolean;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onCreate: () => void;
  onRename: () => void;
  onRemove: () => void;
  onMove: (direction: -1 | 1) => void;
  onRead?: () => void;
}) {
  const openingDialog = useRef(false);
  function openDialog(action: () => void) {
    openingDialog.current = true;
    action();
  }
  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
      <ContextMenu.Portal>
        <ContextMenu.Content
          className="session-context-menu"
          collisionPadding={8}
          aria-label={t("{0} 分区操作", name)}
          onCloseAutoFocus={(event) => {
            if (openingDialog.current) event.preventDefault();
            openingDialog.current = false;
          }}
        >
          <ContextMenu.Label className="session-context-label">
            <strong>{name}</strong>
          </ContextMenu.Label>
          {editable && (
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => openDialog(onRename)}
            >
              <Pencil size={15} />
              {t("重命名分区")}
            </ContextMenu.Item>
          )}
          {onRead && (
            <ContextMenu.Item
              className="session-context-item"
              onSelect={onRead}
            >
              <Check size={15} />
              {t("全部标为已读")}
            </ContextMenu.Item>
          )}
          <ContextMenu.Item
            className="session-context-item"
            disabled={!canMoveUp}
            onSelect={() => onMove(-1)}
          >
            <ArrowUp size={15} />
            {t("上移")}
          </ContextMenu.Item>
          <ContextMenu.Item
            className="session-context-item"
            disabled={!canMoveDown}
            onSelect={() => onMove(1)}
          >
            <ArrowDown size={15} />
            {t("下移")}
          </ContextMenu.Item>
          <ContextMenu.Separator className="file-menu-separator" />
          <ContextMenu.Item
            className="session-context-item"
            onSelect={() => openDialog(onCreate)}
          >
            <Plus size={15} />
            {t("新建分区")}
          </ContextMenu.Item>
          {editable && (
            <ContextMenu.Item
              className="session-context-item project-remove-action"
              onSelect={() => openDialog(onRemove)}
            >
              <X size={15} />
              {t("移除分区")}
            </ContextMenu.Item>
          )}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  );
}
