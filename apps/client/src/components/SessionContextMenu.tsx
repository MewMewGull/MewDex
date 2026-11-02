import { useRef, useState, type ReactElement } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { X } from "lucide-react";
import type { Session } from "@mewdex/sdk";
import type { UpdateSession } from "./SessionManagement";
import { sessionManagementActions } from "./sessionManagementActions";

export function SessionContextMenu({
  session,
  context,
  canManage,
  onUpdate,
  onRename,
  children,
}: {
  session: Session;
  context: string;
  canManage: boolean;
  onUpdate: UpdateSession;
  onRename: (session: Session, origin: HTMLElement | null) => void;
  children: ReactElement;
}) {
  const [error, setError] = useState("");
  const openingDialog = useRef(false);
  const trigger = useRef<HTMLSpanElement>(null);
  return (
    <>
      <ContextMenu.Root>
        <ContextMenu.Trigger ref={trigger} asChild>
          {children}
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content
            className="session-context-menu"
            collisionPadding={8}
            aria-label={`${session.title} 的对话操作`}
            onCloseAutoFocus={(event) => {
              if (openingDialog.current) event.preventDefault();
              openingDialog.current = false;
            }}
          >
            <ContextMenu.Label className="session-context-label">
              <strong title={session.title}>{session.title}</strong>
              <small title={context}>{context}</small>
            </ContextMenu.Label>
            {sessionManagementActions(session).map((action) => (
              <ContextMenu.Item
                className="session-context-item"
                key={action.id}
                disabled={!canManage}
                onSelect={() => {
                  setError("");
                  if (action.patch) {
                    void onUpdate(session, action.patch).catch((cause: Error) =>
                      setError(`未能${action.label}：${cause.message}`),
                    );
                  } else {
                    openingDialog.current = true;
                    onRename(session, trigger.current);
                  }
                }}
              >
                <action.Icon size={16} />
                <span>
                  {action.label}
                  {action.description && <small>{action.description}</small>}
                </span>
              </ContextMenu.Item>
            ))}
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>
      {error && (
        <p className="session-context-error" role="alert">
          <span>
            {session.title}：{error}
          </span>
          <button
            className="icon-button"
            aria-label="关闭对话整理错误"
            onClick={() => setError("")}
          >
            <X size={12} />
          </button>
        </p>
      )}
    </>
  );
}
