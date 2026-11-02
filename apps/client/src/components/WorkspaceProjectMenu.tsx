import { useRef, useState, type ReactElement } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Check, Folder, Globe, Plus, Settings2 } from "lucide-react";
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
}: {
  children: ReactElement;
  device: Device;
  project: Project;
  categories: string[];
  conversationCount: number;
  onNew: () => void;
  onEdit: (category?: string) => void;
  onCategory: (category: string) => Promise<void>;
}) {
  const [error, setError] = useState("");
  const changing = useRef(false);
  return (
    <>
      <ContextMenu.Root>
        <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content
            className="session-context-menu project-context-menu"
            collisionPadding={8}
          >
            <ContextMenu.Label className="session-context-label">
              <strong>
                <Folder size={15} />
                {project.name}
              </strong>
              <small>
                <Globe size={13} />
                {device.name} · {conversationCount} 次对话 ·{" "}
                {device.online ? "已连接" : "未连接"}
              </small>
              <small title={project.path}>{project.path}</small>
            </ContextMenu.Label>
            <ContextMenu.Item className="session-context-item" onSelect={onNew}>
              <Plus size={15} />
              新建对话
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onEdit()}
            >
              <Settings2 size={15} />
              名称与分类…
            </ContextMenu.Item>
            <ContextMenu.Sub>
              <ContextMenu.SubTrigger className="session-context-item">
                移到分类<span>›</span>
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
                          !device.capabilities?.includes("workspace-management")
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
                      <span>{category || "项目"}</span>
                      {(project.category ?? "") === category && (
                        <Check size={14} />
                      )}
                    </ContextMenu.Item>
                  ))}
                  <ContextMenu.Item
                    className="session-context-item"
                    onSelect={() => onEdit("")}
                  >
                    新建分类…
                  </ContextMenu.Item>
                </ContextMenu.SubContent>
              </ContextMenu.Portal>
            </ContextMenu.Sub>
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
