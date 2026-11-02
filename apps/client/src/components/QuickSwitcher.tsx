import { useEffect, useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { FolderPlus, MessageSquare, Search, X } from "lucide-react";
import type { Device, Session } from "@mewdex/sdk";
import { composerKey } from "../composerDrafts";
import { deviceDescription, devicePresence } from "../devicePresence";
import { quickSwitchGroups, type SwitchEntry } from "../workspaceNavigation";
import { useKeyboardList } from "../useKeyboardList";
import { DeviceIcon } from "./DeviceIcon";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function QuickSwitcher({
  open,
  onOpenChange,
  devices,
  sessions,
  draftSessions,
  draftKeys,
  unreadSessions,
  connected,
  deviceId,
  onDevice,
  onSession,
  onProject,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  devices: Device[];
  sessions: Session[];
  draftSessions: ReadonlySet<string>;
  draftKeys: ReadonlySet<string>;
  unreadSessions: ReadonlySet<string>;
  connected: boolean;
  deviceId: string;
  onDevice: (id: string) => void;
  onSession: (session: Session) => void;
  onProject: (device: Device, projectId: string) => void;
}) {
  const [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const chosen = useRef(false);
  const returnFocus = useDialogReturnFocus();
  const groups = useMemo(
    () =>
      quickSwitchGroups({
        devices,
        sessions,
        search,
        deviceId,
        unreadSessions,
      }),
    [devices, sessions, search, deviceId, unreadSessions],
  );
  const entries = groups.flatMap((group) => group.entries);
  const { active, listboxId, optionId, setSelectedId, onKeyDown } =
    useKeyboardList(entries, open, choose);
  useEffect(() => {
    if (!open) {
      setSearch("");
    }
  }, [open]);
  function choose(entry: SwitchEntry) {
    chosen.current = true;
    if (entry.kind === "session") onSession(entry.session);
    else if (entry.kind === "project")
      onProject(entry.device, entry.project.id);
    else onDevice(entry.device.id);
    onOpenChange(false);
  }
  function describe(entry: SwitchEntry) {
    if (entry.kind === "session")
      return [
        unreadSessions.has(entry.session.id) ? "未读" : "",
        draftSessions.has(entry.session.id) ? "草稿" : "",
        entry.session.archived
          ? "已归档"
          : entry.session.pinned
            ? "已置顶"
            : "",
        entry.deviceName,
        entry.projectName,
      ]
        .filter(Boolean)
        .join(" · ");
    if (entry.kind === "project")
      return [
        entry.device.name,
        draftKeys.has(composerKey(entry.device.id, "", entry.project.id))
          ? "继续草稿"
          : "新对话",
        entry.device.roles.includes("executor") ? "" : "未开启执行能力",
        entry.device.online && connected
          ? ""
          : devicePresence(entry.device.online, connected).label,
      ]
        .filter(Boolean)
        .join(" · ");
    return deviceDescription(entry.device, connected);
  }
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog quick-switcher"
          onOpenAutoFocus={(event) => {
            returnFocus.capture(event);
            event.preventDefault();
            chosen.current = false;
            input.current?.focus();
          }}
          onCloseAutoFocus={(event) => {
            returnFocus.restore(event, chosen.current);
          }}
        >
          <Dialog.Title className="sr-only">快速切换</Dialog.Title>
          <Dialog.Description className="sr-only">
            搜索设备、项目与对话，用方向键选择，按回车打开。选择项目进入该项目的新对话或未发送草稿。
          </Dialog.Description>
          <div className="quick-switcher-search">
            <Search size={18} />
            <input
              ref={input}
              aria-label="搜索设备、项目与对话"
              placeholder="搜索设备、项目与对话…"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setSelectedId("");
              }}
              role="combobox"
              aria-controls={listboxId}
              aria-expanded="true"
              aria-autocomplete="list"
              aria-activedescendant={active ? optionId(active.id) : undefined}
              onKeyDown={onKeyDown}
            />
            <Dialog.Close className="icon-button" aria-label="关闭快速切换">
              <X size={17} />
            </Dialog.Close>
          </div>
          <div
            className="quick-switcher-results"
            role="listbox"
            id={listboxId}
            aria-label="匹配结果"
          >
            {groups.map((group) => (
              <div
                className="quick-switcher-group"
                role="group"
                aria-labelledby={`${listboxId}-${group.id}`}
                key={group.id}
              >
                <div
                  className="quick-switcher-group-heading"
                  role="presentation"
                >
                  <span id={`${listboxId}-${group.id}`}>{group.label}</span>
                  <small>
                    {group.entries.length < group.total
                      ? `${group.entries.length} / ${group.total}`
                      : group.total}
                  </small>
                </div>
                {group.entries.map((entry) => {
                  const title =
                    entry.kind === "session"
                      ? entry.session.title
                      : entry.kind === "project"
                        ? entry.project.name
                        : entry.device.name;
                  const subtitle = describe(entry);
                  const selected = active?.id === entry.id;
                  const Icon =
                    entry.kind === "session" ? MessageSquare : FolderPlus;
                  return (
                    <button
                      key={entry.id}
                      id={optionId(entry.id)}
                      role="option"
                      tabIndex={-1}
                      aria-selected={selected}
                      title={`${title}\n${subtitle}`}
                      onMouseMove={() => setSelectedId(entry.id)}
                      onClick={() => choose(entry)}
                    >
                      {entry.kind === "device" ? (
                        <DeviceIcon
                          platform={entry.device.platform}
                          size={17}
                        />
                      ) : (
                        <Icon size={17} />
                      )}
                      <span className="quick-switcher-copy">
                        <strong>{title}</strong>
                        <small>{subtitle}</small>
                      </span>
                      {entry.kind === "device" &&
                        entry.device.id === deviceId && (
                          <span className="quick-switcher-current">当前</span>
                        )}
                      {entry.kind === "session" &&
                        unreadSessions.has(entry.session.id) && (
                          <span
                            className="session-unread-dot"
                            role="img"
                            aria-label="有新回复"
                          />
                        )}
                      {selected && <kbd>↵</kbd>}
                    </button>
                  );
                })}
              </div>
            ))}
            {!entries.length && (
              <p className="empty-list">没有找到匹配的设备、项目或对话</p>
            )}
          </div>
          <div className="quick-switcher-caption">
            <span className="keyboard-only-hint">↑ ↓ 选择</span>
            <span className="keyboard-only-hint">Enter 打开</span>
            <span className="keyboard-only-hint">Esc 关闭</span>
            {!search.trim() && (
              <span className="quick-switcher-search-hint">
                输入名称查找全部对话与项目
              </span>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
