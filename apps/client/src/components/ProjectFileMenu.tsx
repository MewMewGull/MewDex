import type { ReactElement } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import type { ProjectOpener } from "../projectFileActions";

export function ProjectFileMenu({
  children,
  directory,
  local,
  native,
  openers,
  defaultOpener,
  onDefault,
  onOpen,
  onPreview,
  onCopy,
  onReveal,
}: {
  children: ReactElement;
  directory: boolean;
  local: boolean;
  native: boolean;
  openers: ProjectOpener[];
  defaultOpener: string;
  onDefault: (value: string) => void;
  onOpen: (action: string) => void;
  onPreview: () => void;
  onCopy: () => void;
  onReveal: () => void;
}) {
  const item = "session-context-item";
  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
      <ContextMenu.Portal>
        <ContextMenu.Content
          className="session-context-menu"
          collisionPadding={8}
        >
          <ContextMenu.Item
            className={item}
            disabled={!native || (directory && !local)}
            onSelect={() => onOpen(defaultOpener)}
          >
            使用默认应用打开
          </ContextMenu.Item>
          {native && (!directory || local) && (
            <ContextMenu.Sub>
              <ContextMenu.SubTrigger className={item}>
                打开方式<span>›</span>
              </ContextMenu.SubTrigger>
              <ContextMenu.Portal>
                <ContextMenu.SubContent
                  className="session-context-menu"
                  sideOffset={5}
                >
                  {openers.map((opener) => (
                    <ContextMenu.Item
                      className={item}
                      key={opener.id}
                      onSelect={() => onOpen(`editor:${opener.id}`)}
                    >
                      {opener.name}
                    </ContextMenu.Item>
                  ))}
                  {local && (
                    <ContextMenu.Item className={item} onSelect={onReveal}>
                      文件资源管理器
                    </ContextMenu.Item>
                  )}
                  {!directory && (
                    <ContextMenu.Item
                      className={item}
                      onSelect={() => onOpen("open-with")}
                    >
                      选择其他应用…
                    </ContextMenu.Item>
                  )}
                </ContextMenu.SubContent>
              </ContextMenu.Portal>
            </ContextMenu.Sub>
          )}
          {!directory && (
            <ContextMenu.Item className={item} onSelect={onPreview}>
              在 MewDex 中预览
            </ContextMenu.Item>
          )}
          {local && (
            <ContextMenu.Item className={item} onSelect={onReveal}>
              在资源管理器中显示
            </ContextMenu.Item>
          )}
          <ContextMenu.Item className={item} onSelect={onCopy}>
            复制路径
          </ContextMenu.Item>
          {native && (
            <>
              <ContextMenu.Separator className="file-menu-separator" />
              <ContextMenu.Label className="session-context-label">
                此工作区默认打开方式
              </ContextMenu.Label>
              <ContextMenu.RadioGroup
                value={defaultOpener}
                onValueChange={onDefault}
              >
                {[
                  { id: "default", name: "系统默认应用" },
                  ...openers.map((opener) => ({
                    id: `editor:${opener.id}`,
                    name: opener.name,
                  })),
                ].map((opener) => (
                  <ContextMenu.RadioItem
                    className={item}
                    key={opener.id}
                    value={opener.id}
                  >
                    <ContextMenu.ItemIndicator>✓ </ContextMenu.ItemIndicator>
                    {opener.name}
                  </ContextMenu.RadioItem>
                ))}
              </ContextMenu.RadioGroup>
            </>
          )}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  );
}
