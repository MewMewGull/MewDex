import { getLanguage, t } from "../localization.ts";
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
import { WorkspaceProjectMenu } from "../components/WorkspaceProjectMenu";
import { RemoveProjectDialog } from "../components/RemoveProjectDialog";
import { DevicePicker } from "../components/DevicePicker";
import {
  conversationContext,
  navigationMatches,
  workspaceConversationGroups,
} from "../workspaceNavigation";
import { SessionContextMenu } from "../components/SessionContextMenu";
import type { UpdateSession } from "../components/SessionManagement";
import { SidebarResizeHandle } from "../components/SidebarResizeHandle";
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
  onManageHub,
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
  canRemoveProjects,
  onRemoveProject,
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
  onManageHub: () => void;
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
  canRemoveProjects: boolean;
  onRemoveProject: (device: Device, project: Project) => Promise<void>;
}) {
  const sidebar = useRef<HTMLElement>(null);
  const modifier = keyboardModifierLabel();
  const [scope, setScope] = useState<"all" | "unread">("all");
  const [search, setSearch] = useState("");
  const [removingProject, setRemovingProject] = useState<{
    device: Device;
    project: Project;
    origin: HTMLElement | null;
  } | null>(null);
  const [collapsed, setCollapsed] = useState(new Set<string>());
  const [collapsedCategories, setCollapsedCategories] = useState(
    new Set<string>(),
  );
  const categories = [
    ...new Set(
      devices.flatMap((device) =>
        device.projects
          .filter((project) => !project.removed)
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
    [
      sessions,
      devicesById,
      deviceId,
      sessionId,
      scope,
      search,
      unreadSessions,
      getLanguage(),
    ],
  );
  const { groups, recent } = useMemo(() => {
    const grouped = workspaceConversationGroups(devices, visible);
    const groups = grouped.groups.filter((group) =>
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
    groups.sort(
      (a, b) =>
        a.category.localeCompare(b.category) || a.name.localeCompare(b.name),
    );
    return { groups, recent: grouped.recent };
  }, [devices, visible, scope, deviceId, search, getLanguage()]);
  function renderSession(session: Session) {
    const device = devicesById.get(session.deviceId);
    const context = conversationContext(session, device);
    const showContext = !!search.trim();
    const label = [context.deviceName, context.projectName].join(" · ");
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
          aria-current={session.id === sessionId ? "page" : undefined}
          title={t(
            "{0}\n{1} · {2}\n右键或菜单键整理对话",
            session.title,
            context.deviceName,
            context.projectName,
          )}
          onClick={() => onSession(session)}
        >
          {session.archived ? (
            <Archive size={15} aria-label={t("已归档")} />
          ) : session.pinned ? (
            <Pin size={15} className="session-pin" aria-label={t("已置顶")} />
          ) : (
            <MessageSquare size={15} />
          )}
          <span className="session-copy">
            <strong>{session.title}</strong>
            {showContext && <small>{label}</small>}
          </span>
          {draftSessions.has(session.id) && (
            <span className="session-draft">{t("草稿")}</span>
          )}
          {unreadSessions.has(session.id) && (
            <span
              className="session-unread-dot"
              role="img"
              aria-label={t("有新回复")}
              title={t("有新回复")}
            />
          )}
          {session.state === "running" && (
            <i
              className={`running-dot ${connected === "connected" ? "" : "is-stale"}`}
              aria-label={
                connected === "connected" ? t("执行中") : t("上次执行中")
              }
            />
          )}
        </button>
      </SessionContextMenu>
    );
  }
  return (
    <>
      <aside
        ref={sidebar}
        style={{ "--sidebar-width": `${width}px` } as CSSProperties}
        className={`sidebar ${open ? "open" : ""}`}
        aria-label={t("对话导航")}
      >
        <div className="sidebar-top">
          {brand}
          <button
            className="icon-button mobile-only"
            aria-label={t("关闭对话导航")}
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </div>
        <nav className="workspace-navigation" aria-label={t("工作台入口")}>
          <button className="new-chat" onClick={onNew}>
            <Plus size={17} />
            <span>{t("新对话")}</span>
          </button>
          <button
            className="workspace-nav-item"
            onClick={onSearch}
            title={t("搜索设备、项目与对话")}
          >
            <Search size={16} />
            <span>{t("搜索工作台")}</span>
            <kbd>{modifier} K</kbd>
          </button>
          <button
            className={`workspace-nav-item network-activity-entry ${attentionTasks ? "needs-attention" : ""}`}
            onClick={onActivity}
            title={t("正在执行的对话和等待你回复的请求 · Ctrl+Shift+J")}
          >
            <Activity size={16} />
            <span>{t("任务与请求")}</span>
            {activeTasks > 0 && (
              <small>
                {attentionTasks
                  ? t("{0} 项待处理", attentionTasks)
                  : t("{0} 项进行中", activeTasks)}
              </small>
            )}
          </button>
        </nav>
        <div className="conversation-nav-heading">
          <strong>{t("项目")}</strong>
          <div className="conversation-scopes" aria-label={t("对话范围")}>
            <button
              aria-pressed={scope === "all"}
              onClick={() => setScope("all")}
            >
              {t("全部")}
            </button>
            <button
              aria-pressed={scope === "unread"}
              aria-label={t("未读对话，{0} 个", unreadCount)}
              title={t("所有设备中有新回复的对话")}
              onClick={() => setScope("unread")}
            >
              {t("未读")}
              {unreadCount > 0 && (
                <span className="conversation-unread-count">{unreadCount}</span>
              )}
            </button>
          </div>
          <button
            className="icon-button"
            aria-label={t("创建项目")}
            onClick={onWorkspaces}
            title={t("选择机器和文件夹，创建项目")}
          >
            <FolderPlus size={15} />
          </button>
        </div>
        <label className="search-field conversation-search">
          <Search size={14} />
          <input
            aria-label={t("搜索对话、设备或项目")}
            placeholder={t("筛选项目和对话")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          {search && (
            <button aria-label={t("清除搜索")} onClick={() => setSearch("")}>
              <X size={13} />
            </button>
          )}
        </label>
        <nav className="session-list" aria-label={t("历史对话")}>
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
                    project={group.project}
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
                      onEditProject(group.device, group.project, category)
                    }
                    onCategory={(category) =>
                      onProjectCategory(group.device, group.project, category)
                    }
                    onRemove={
                      canRemoveProjects &&
                      group.device.roles.includes("coordinator")
                        ? (origin) =>
                            setRemovingProject({
                              device: group.device,
                              project: group.project,
                              origin,
                            })
                        : undefined
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
                        title={`${group.name}\n${group.device.name} · ${group.device.online ? t("已连接") : t("未连接")}\n${group.path}`}
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
                        aria-label={t("{0} 项目设置", group.name)}
                        onClick={() =>
                          onEditProject(group.device, {
                            id: group.projectId,
                            name: group.name,
                            path: group.path,
                            category:
                              group.category === t("项目")
                                ? undefined
                                : group.category,
                          })
                        }
                      >
                        <MoreHorizontal size={14} />
                      </button>
                      <button
                        className="icon-button"
                        aria-label={t("在 {0} 新建对话", group.name)}
                        onClick={() => onProject(group.device, group.projectId)}
                      >
                        <Plus size={14} />
                      </button>
                    </div>
                  </WorkspaceProjectMenu>
                  {(!!search.trim() || !collapsed.has(group.key)) && (
                    <div className="workspace-group-sessions">
                      {group.sessions.map(renderSession)}
                      {!group.sessions.length && (
                        <button
                          className="workspace-empty"
                          onClick={() =>
                            onProject(group.device, group.projectId)
                          }
                        >
                          {t("新建对话")}
                        </button>
                      )}
                    </div>
                  )}
                </>
              )}
            </section>
          ))}
          {recent.length > 0 && (
            <section
              className="recent-conversations"
              aria-label={t("最近对话")}
            >
              <h2 className="sidebar-section-label">{t("最近对话")}</h2>
              {recent.map(renderSession)}
            </section>
          )}
          {!groups.length && !recent.length && (
            <p className="empty-list">
              {search
                ? t("没有找到匹配对话")
                : scope === "unread"
                  ? t("没有未读回复")
                  : t("从新对话开始")}
            </p>
          )}
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-device">
            <span className="sidebar-section-label">{t("Hub 连接")}</span>
            <DevicePicker
              devices={devices}
              selectedId={deviceId}
              connected={connected === "connected"}
              onSelect={onDevice}
              onManageHub={onManageHub}
            />
          </div>
          <div className="sidebar-utilities">
            {onLocalDevice && (
              <button onClick={onLocalDevice}>
                <Monitor size={15} />
                <span>{t("连接与服务")}</span>
              </button>
            )}
            <button onClick={onSettings}>
              <Settings size={15} />
              <span>{t("设置")}</span>
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
      {removingProject && (
        <RemoveProjectDialog
          project={removingProject.project}
          returnFocus={removingProject.origin}
          onClose={() => setRemovingProject(null)}
          onRemove={() =>
            onRemoveProject(removingProject.device, removingProject.project)
          }
        />
      )}
    </>
  );
}
