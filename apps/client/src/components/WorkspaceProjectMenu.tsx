import { t } from "../localization.ts";
import { useRef, useState, type ReactElement } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Check, Folder, Globe, Plus, Settings2, X } from "lucide-react";
import type { Device, Project } from "@mewdex/sdk";

export function WorkspaceProjectMenu({
  children,
  device,
  project,
  categories,
  conversationCount,
  onNew,
  onEdit,
  onCategory,
  onRemove,
}: {
  children: ReactElement;
  device: Device;
  project: Project;
  categories: string[];
  conversationCount: number;
  onNew: () => void;
  onEdit: (category?: string) => void;
  onCategory: (category: string) => Promise<void>;
  onRemove?: (origin: HTMLElement | null) => void;
}) {
  const [error, setError] = useState("");
  const changing = useRef(false);
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
            className="session-context-menu project-context-menu"
            collisionPadding={8}
            onCloseAutoFocus={(event) => {
              if (openingDialog.current) event.preventDefault();
              openingDialog.current = false;
            }}
          >
            <ContextMenu.Label className="session-context-label">
              <strong>
                <Folder size={15} />
                {project.name}
              </strong>
              <small>
                <Globe size={13} />
                {t(
                  "{0} · {1} 次对话 · {2}",
                  device.name,
                  conversationCount,
                  device.online ? t("已连接") : t("未连接"),
                )}
              </small>
              <small title={project.path}>{project.path}</small>
            </ContextMenu.Label>
            <ContextMenu.Item className="session-context-item" onSelect={onNew}>
              <Plus size={15} />
              {t("新建对话")}
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onEdit()}
            >
              <Settings2 size={15} />
              {t("项目与目录组…")}
            </ContextMenu.Item>
            <ContextMenu.Sub>
              <ContextMenu.SubTrigger className="session-context-item">
                {t("移到分类")}
                <span>›</span>
              </ContextMenu.SubTrigger>
              <ContextMenu.Portal>
                <ContextMenu.SubContent
                  className="session-context-menu"
                  sideOffset={5}
                >
                  {["", ...categories].map((category) => (
                    <ContextMenu.Item
                      key={category}
                      className="session-context-item"
                      disabled={!device.online}
                      onSelect={() => {
                        if (changing.current) return;
                        if (
                          !device.capabilities?.some((capability) =>
                            ["workspace-management", "hub-projects"].includes(
                              capability,
                            ),
                          )
                        ) {
                          onEdit(category);
                          return;
                        }
                        changing.current = true;
                        setError("");
                        void onCategory(category)
                          .catch((cause: Error) => setError(cause.message))
                          .finally(() => {
                            changing.current = false;
                          });
                      }}
                    >
                      <span>{category || t("项目")}</span>
                      {(project.category ?? "") === category && (
                        <Check size={14} />
                      )}
                    </ContextMenu.Item>
                  ))}
                  <ContextMenu.Item
                    className="session-context-item"
                    onSelect={() => onEdit("")}
                  >
                    {t("新建分类…")}
                  </ContextMenu.Item>
                </ContextMenu.SubContent>
              </ContextMenu.Portal>
            </ContextMenu.Sub>
            <ContextMenu.Separator className="file-menu-separator" />
            <ContextMenu.Item
              className="session-context-item project-remove-action"
              disabled={!onRemove}
              onSelect={() => {
                openingDialog.current = true;
                onRemove?.(
                  trigger.current?.querySelector("button") ?? trigger.current,
                );
              }}
            >
              <X size={15} />
              {t("移除项目")}
            </ContextMenu.Item>
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>
      {error && (
        <p className="workspace-project-error" role="alert">
          {error}
        </p>
      )}
    </>
  );
}
