import {
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type CSSProperties,
} from "react";
import {
  Archive,
  Activity,
  MessageSquare,
  Pin,
  Plus,
  Search,
  MoreHorizontal,
  X,
  ChevronDown,
  Folder,
  FolderPlus,
  Settings,
  Monitor,
} from "lucide-react";
import type { Device, Project, Session } from "@mewdex/sdk";
import { WorkspaceProjectMenu } from "./WorkspaceProjectMenu";
import { DevicePicker } from "./DevicePicker";
import { conversationContext, navigationMatches } from "../workspaceNavigation";
import { SessionContextMenu } from "./SessionContextMenu";
import type { UpdateSession } from "./SessionManagement";
import { SidebarResizeHandle } from "./SidebarResizeHandle";
import { keyboardModifierLabel } from "../inputKeys";

export function WorkspaceSidebar({
  open,
  brand,
  devices,
  sessions,
  deviceId,
  sessionId,
  connected,
  draftSessions,
  unreadSessions,
  activeTasks,
  attentionTasks,
  onActivity,
  onSearch,
  onSettings,
  onLocalDevice,
  onClose,
  onDevice,
  onSession,
  onNew,
  canManageSessions,
  onUpdateSession,
  onRenameSession,
  width,
  onWidthChange,
  onWorkspaces,
  onProject,
  account,
  projectId,
  onEditProject,
  onProjectCategory,
}: {
  open: boolean;
  brand: ReactNode;
  devices: Device[];
  sessions: Session[];
  deviceId: string;
  sessionId: string;
  connected: string;
  draftSessions: ReadonlySet<string>;
  unreadSessions: ReadonlySet<string>;
  activeTasks: number;
  attentionTasks: number;
  onActivity: () => void;
  onSearch: () => void;
  onSettings: () => void;
  onLocalDevice?: () => void;
  onClose: () => void;
  onDevice: (id: string) => void;
  onSession: (session: Session) => void;
  onNew: () => void;
  canManageSessions: boolean;
  onUpdateSession: UpdateSession;
  onRenameSession: (session: Session, origin: HTMLElement | null) => void;
  width: number;
  onWidthChange: (width: number) => void;
  onWorkspaces: () => void;
  onProject: (device: Device, projectId: string) => void;
  account: ReactNode;
  projectId: string;
  onEditProject: (device: Device, project: Project, category?: string) => void;
  onProjectCategory: (
    device: Device,
    project: Project,
    category: string,
  ) => Promise<void>;
}) {
  const sidebar = useRef<HTMLElement>(null);
  const modifier = keyboardModifierLabel();
  const [scope, setScope] = useState<"all" | "unread">("all");
  const [search, setSearch] = useState("");
  const [collapsed, setCollapsed] = useState(new Set<string>());
  const [collapsedCategories, setCollapsedCategories] = useState(
    new Set<string>(),
  );
  const categories = [
    ...new Set(
      devices.flatMap((device) =>
        device.projects
          .map((project) => project.category)
          .filter((value): value is string => !!value),
      ),
    ),
  ].sort();
  const devicesById = useMemo(
    () => new Map(devices.map((device) => [device.id, device])),
    [devices],
  );
  const unreadCount = sessions.filter((session) =>
    unreadSessions.has(session.id),
  ).length;
  const visible = useMemo(
    () =>
      sessions
        .filter(
          (session) =>
            (!session.archived ||
              session.id === sessionId ||
              scope === "unread") &&
            (scope === "unread"
              ? unreadSessions.has(session.id) || session.id === sessionId
              : true) &&
            navigationMatches(
              search,
              session.title,
              ...Object.values(
                conversationContext(session, devicesById.get(session.deviceId)),
              ),
            ),
        )
        .sort(
          (a, b) =>
            Number(!!b.pinned) - Number(!!a.pinned) ||
            b.updatedAt.localeCompare(a.updatedAt),
        ),
    [sessions, devicesById, deviceId, sessionId, scope, search, unreadSessions],
  );
  const groups = useMemo(() => {
    const result = devices
      .flatMap((device) =>
        device.projects.map((project) => ({
          key: `${device.id}:${project.id}`,
          device,
          projectId: project.id,
          name: project.name,
          category: project.category || "项目",
          path: project.path,
          sessions: visible.filter(
            (session) =>
              session.deviceId === device.id &&
              session.projectId === project.id,
          ),
        })),
      )
      .filter((group) =>
        scope === "unread"
          ? group.sessions.length > 0
          : !search.trim() ||
            group.sessions.length > 0 ||
            navigationMatches(
              search,
              group.name,
              group.category,
              group.device.name,
            ),
      );
    for (const session of visible) {
      if (
        !result.some(
          (group) =>
            group.device.id === session.deviceId &&
            group.projectId === session.projectId,
        )
      ) {
        const device = devicesById.get(session.deviceId);
        if (device)
          result.push({
            key: `${device.id}:${session.projectId}`,
            device,
            projectId: session.projectId,
            name: session.projectId,
            category: "未关联目录",
            path: "",
            sessions: visible.filter(
              (s) =>
                s.deviceId === session.deviceId &&
                s.projectId === session.projectId,
            ),
          });
      }
    }
    return result.sort(
      (a, b) =>
        a.category.localeCompare(b.category) || a.name.localeCompare(b.name),
    );
  }, [devices, visible, scope, deviceId, search]);
  return (
    <>
      <aside
        ref={sidebar}
        style={{ "--sidebar-width": `${width}px` } as CSSProperties}
        className={`sidebar ${open ? "open" : ""}`}
        aria-label="对话导航"
      >
        <div className="sidebar-top">
          {brand}
          <button
            className="icon-button mobile-only"
            aria-label="关闭对话导航"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </div>
        <nav className="workspace-navigation" aria-label="工作台入口">
          <button className="new-chat" onClick={onNew}>
            <Plus size={17} />
            <span>新对话</span>
          </button>
          <button
            className="workspace-nav-item"
            onClick={onSearch}
            title="搜索设备、项目与对话"
          >
            <Search size={16} />
            <span>搜索工作台</span>
            <kbd>{modifier} K</kbd>
          </button>
          <button
            className={`workspace-nav-item network-activity-entry ${attentionTasks ? "needs-attention" : ""}`}
            onClick={onActivity}
            title="正在执行的对话和等待你回复的请求 · Ctrl+Shift+J"
          >
            <Activity size={16} />
            <span>任务与请求</span>
            {activeTasks > 0 && (
              <small>
                {attentionTasks
                  ? `${attentionTasks} 项待处理`
                  : `${activeTasks} 项进行中`}
              </small>
            )}
          </button>
        </nav>
        <div className="conversation-nav-heading">
          <strong>项目</strong>
          <div className="conversation-scopes" aria-label="对话范围">
            <button
              aria-pressed={scope === "all"}
              onClick={() => setScope("all")}
            >
              全部
            </button>
            <button
              aria-pressed={scope === "unread"}
              aria-label={`未读对话，${unreadCount} 个`}
              title="所有设备中有新回复的对话"
              onClick={() => setScope("unread")}
            >
              未读
              {unreadCount > 0 && (
                <span className="conversation-unread-count">{unreadCount}</span>
              )}
            </button>
          </div>
          <button
            className="icon-button"
            aria-label="创建项目"
            onClick={onWorkspaces}
            title="选择机器和文件夹，创建项目"
          >
            <FolderPlus size={15} />
          </button>
        </div>
        <label className="search-field conversation-search">
          <Search size={14} />
          <input
            aria-label="搜索对话、设备或项目"
            placeholder="筛选项目和对话"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          {search && (
            <button aria-label="清除搜索" onClick={() => setSearch("")}>
              <X size={13} />
            </button>
          )}
        </label>
        <nav className="session-list" aria-label="历史对话">
          {groups.map((group, index) => (
            <section className="workspace-group" key={group.key}>
              {(index === 0 ||
                groups[index - 1].category !== group.category) && (
                <button
                  className="workspace-category"
                  aria-expanded={
                    !collapsedCategories.has(group.category) || !!search.trim()
                  }
                  onClick={() =>
                    setCollapsedCategories((previous) => {
                      const next = new Set(previous);
                      if (next.has(group.category)) next.delete(group.category);
                      else next.add(group.category);
                      return next;
                    })
                  }
                >
                  <ChevronDown
                    size={12}
                    className={
                      !search.trim() && collapsedCategories.has(group.category)
                        ? "collapsed"
                        : ""
                    }
                  />
                  {group.category}
                </button>
              )}
              {(!!search.trim() ||
                !collapsedCategories.has(group.category)) && (
                <>
                  <WorkspaceProjectMenu
                    device={group.device}
                    project={{
                      id: group.projectId,
                      name: group.name,
                      path: group.path,
                      category:
                        group.category === "项目" ? undefined : group.category,
                    }}
                    categories={categories}
                    conversationCount={
                      sessions.filter(
                        (session) =>
                          session.deviceId === group.device.id &&
                          session.projectId === group.projectId,
                      ).length
                    }
                    onNew={() => onProject(group.device, group.projectId)}
                    onEdit={(category) =>
                      onEditProject(
                        group.device,
                        {
                          id: group.projectId,
                          name: group.name,
                          path: group.path,
                          category:
                            group.category === "项目"
                              ? undefined
                              : group.category,
                        },
                        category,
                      )
                    }
                    onCategory={(category) =>
                      onProjectCategory(
                        group.device,
                        {
                          id: group.projectId,
                          name: group.name,
                          path: group.path,
                        },
                        category,
                      )
                    }
                  >
                    <div className="workspace-group-heading">
                      <button
                        aria-expanded={
                          !!search.trim() || !collapsed.has(group.key)
                        }
                        className={
                          deviceId === group.device.id &&
                          projectId === group.projectId
                            ? "is-current-project"
                            : ""
                        }
                        title={`${group.name}\n${group.device.name} · ${group.device.online ? "已连接" : "未连接"}\n${group.path}`}
                        onClick={() =>
                          setCollapsed((previous) => {
                            const next = new Set(previous);
                            if (next.has(group.key)) next.delete(group.key);
                            else next.add(group.key);
                            return next;
                          })
                        }
                      >
                        <ChevronDown
                          size={12}
                          className={
                            !search.trim() && collapsed.has(group.key)
                              ? "collapsed"
                              : ""
                          }
                        />
                        <Folder size={14} />
                        <span>{group.name}</span>
                        <small className="workspace-machine">
                          <i
                            className={
                              group.device.online ? "online" : "offline"
                            }
                          />
                          {group.device.name}
                        </small>
                      </button>
                      <button
                        className="icon-button"
                        aria-label={`${group.name} 项目设置`}
                        onClick={() =>
                          onEditProject(group.device, {
                            id: group.projectId,
                            name: group.name,
                            path: group.path,
                            category:
                              group.category === "项目"
                                ? undefined
                                : group.category,
                          })
                        }
                      >
                        <MoreHorizontal size={14} />
                      </button>
                      <button
                        className="icon-button"
                        aria-label={`在 ${group.name} 新建对话`}
                        onClick={() => onProject(group.device, group.projectId)}
                      >
                        <Plus size={14} />
                      </button>
                    </div>
                  </WorkspaceProjectMenu>
                  {(!!search.trim() || !collapsed.has(group.key)) && (
                    <div className="workspace-group-sessions">
                      {group.sessions.map((session) => {
                        const device = devicesById.get(session.deviceId);
                        const context = conversationContext(session, device);
                        const showContext = !!search.trim();
                        const label = [
                          context.deviceName,
                          context.projectName,
                        ].join(" · ");
                        return (
                          <SessionContextMenu
                            key={session.id}
                            session={session}
                            context={`${context.deviceName} · ${context.projectName}`}
                            canManage={canManageSessions}
                            onUpdate={onUpdateSession}
                            onRename={onRenameSession}
                          >
                            <button
                              className={`session ${session.id === sessionId ? "selected" : ""} ${unreadSessions.has(session.id) ? "has-unread" : ""}`}
                              aria-current={
                                session.id === sessionId ? "page" : undefined
                              }
                              title={`${session.title}\n${context.deviceName} · ${context.projectName}\n右键或菜单键整理对话`}
                              onClick={() => onSession(session)}
                            >
                              {session.archived ? (
                                <Archive size={15} aria-label="已归档" />
                              ) : session.pinned ? (
                                <Pin
                                  size={15}
                                  className="session-pin"
                                  aria-label="已置顶"
                                />
                              ) : (
                                <MessageSquare size={15} />
                              )}
                              <span className="session-copy">
                                <strong>{session.title}</strong>
                                {showContext && <small>{label}</small>}
                              </span>
                              {draftSessions.has(session.id) && (
                                <span className="session-draft">草稿</span>
                              )}
                              {unreadSessions.has(session.id) && (
                                <span
                                  className="session-unread-dot"
                                  role="img"
                                  aria-label="有新回复"
                                  title="有新回复"
                                />
                              )}
                              {session.state === "running" && (
                                <i
                                  className={`running-dot ${connected === "connected" ? "" : "is-stale"}`}
                                  aria-label={
                                    connected === "connected"
                                      ? "执行中"
                                      : "上次执行中"
                                  }
                                />
                              )}
                            </button>
                          </SessionContextMenu>
                        );
                      })}
                      {!group.sessions.length && (
                        <button
                          className="workspace-empty"
                          onClick={() =>
                            onProject(group.device, group.projectId)
                          }
                        >
                          新建对话
                        </button>
                      )}
                    </div>
                  )}
                </>
              )}
            </section>
          ))}
          {!groups.length && (
            <p className="empty-list">
              {search
                ? "没有找到匹配对话"
                : scope === "unread"
                  ? "没有未读回复"
                  : "从新对话开始"}
            </p>
          )}
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-device">
            <span className="sidebar-section-label">执行设备</span>
            <DevicePicker
              devices={devices}
              selectedId={deviceId}
              connected={connected === "connected"}
              onSelect={onDevice}
            />
          </div>
          <div className="sidebar-utilities">
            {onLocalDevice && (
              <button onClick={onLocalDevice}>
                <Monitor size={15} />
                <span>本机设备</span>
              </button>
            )}
            <button onClick={onSettings}>
              <Settings size={15} />
              <span>设置</span>
              <kbd>{modifier} ,</kbd>
            </button>
          </div>
          {account}
        </div>
      </aside>
      <SidebarResizeHandle
        sidebar={sidebar}
        width={width}
        onCommit={onWidthChange}
      />
      {open && <div className="sidebar-backdrop" onClick={onClose} />}
    </>
  );
}
