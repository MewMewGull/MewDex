import { getLanguage, t } from "../localization.ts";
import {
  useMemo,
  useRef,
  useState,
  type PointerEvent,
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
import {
  completeSidebarOrder,
  emptySidebarLayout,
  moveSidebarItem,
  type SidebarLayout,
} from "../sidebarLayout";
import {
  WorkspaceSectionMenu,
  openSidebarContextMenu,
  sidebarMenuKey,
} from "../components/WorkspaceSectionMenu";
import { WorkspaceSectionDialog } from "../components/WorkspaceSectionDialog";

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
  sidebarLayout,
  onSidebarLayout,
  canManageProjects,
  onReadSessions,
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
  sidebarLayout?: SidebarLayout;
  onSidebarLayout?: (layout: SidebarLayout) => void;
  canManageProjects?: boolean;
  onReadSessions?: (sessions: Session[]) => void;
  canRemoveProjects: boolean;
  onRemoveProject: (device: Device, project: Project) => Promise<void>;
}) {
  const canOrganizeProjects = canManageProjects ?? canRemoveProjects;
  const sidebar = useRef<HTMLElement>(null);
  const modifier = keyboardModifierLabel();
  const [scope, setScope] = useState<"all" | "unread">("all");
  const [search, setSearch] = useState("");
  const [removingProject, setRemovingProject] = useState<{
    device: Device;
    project: Project;
    origin: HTMLElement | null;
  } | null>(null);
  const [localLayout, setLocalLayout] = useState(emptySidebarLayout);
  const layout = sidebarLayout ?? localLayout;
  const collapsed = new Set(layout.collapsedProjects);
  const collapsedCategories = new Set(layout.collapsedSections);
  const [sectionDialog, setSectionDialog] = useState<{
    mode: "create" | "rename" | "remove";
    name: string;
    project?: { device: Device; project: Project };
  } | null>(null);
  const [dragging, setDragging] = useState<{
    kind: "section" | "project";
    id: string;
  } | null>(null);
  const dragSource = useRef<{
    kind: "section" | "project";
    id: string;
    x: number;
    y: number;
    active: boolean;
    destination: {
      kind: "section" | "project";
      id: string;
      after: boolean;
    } | null;
  } | null>(null);
  const suppressClick = useRef(false);
  const [drop, setDrop] = useState<{
    kind: "section" | "project";
    id: string;
    after: boolean;
  } | null>(null);
  const [sidebarError, setSidebarError] = useState("");
  const [changing, setChanging] = useState(false);
  const changingRef = useRef(false);
  const allGroups = useMemo(
    () => workspaceConversationGroups(devices, sessions).groups,
    [devices, sessions],
  );
  const sectionOrder = completeSidebarOrder(
    layout.sections,
    [
      "",
      ...new Set(allGroups.map((group) => group.project.category ?? "")),
    ].sort(),
  );
  const categories = sectionOrder.filter(Boolean);
  const projectOrder = completeSidebarOrder(
    layout.projects,
    [...allGroups]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((group) => group.key),
  );

  function saveLayout(next: SidebarLayout) {
    setLocalLayout(next);
    onSidebarLayout?.(next);
  }
  function toggleCollapsed(
    key: "collapsedSections" | "collapsedProjects",
    id: string,
  ) {
    saveLayout({
      ...layout,
      [key]: layout[key].includes(id)
        ? layout[key].filter((item) => item !== id)
        : [...layout[key], id],
    });
  }
  function moveSection(name: string, direction: -1 | 1) {
    const target = sectionOrder[sectionOrder.indexOf(name) + direction];
    if (target === undefined) return;
    saveLayout({
      ...layout,
      sections: moveSidebarItem(sectionOrder, name, target, direction === 1),
    });
  }
  async function saveSection(name: string) {
    if (!sectionDialog) return;
    if (changingRef.current) throw new Error(t("正在整理项目，请稍后重试。"));
    const original = sectionDialog.name;
    if (
      sectionDialog.mode !== "remove" &&
      name !== original &&
      (sectionOrder.includes(name) || name === t("项目"))
    )
      throw new Error(t("已存在同名分区，请使用其他名称。"));
    changingRef.current = true;
    setChanging(true);
    try {
      const destination = sectionDialog.mode === "remove" ? "" : name;
      const members =
        sectionDialog.mode === "create"
          ? sectionDialog.project
            ? [sectionDialog.project]
            : []
          : allGroups.filter(
              (group) => (group.project.category ?? "") === original,
            );
      for (const member of members) {
        if (!canOrganizeProjects || !member.device.online)
          throw new Error(t("请连接 Hub 并使用管理员账号整理项目分区。"));
        await onProjectCategory(member.device, member.project, destination);
      }
      const sections =
        sectionDialog.mode === "create"
          ? completeSidebarOrder(sectionOrder, [name])
          : sectionDialog.mode === "remove"
            ? sectionOrder.filter((section) => section !== original)
            : sectionOrder.map((section) =>
                section === original ? name : section,
              );
      saveLayout({
        ...layout,
        sections,
        collapsedSections: layout.collapsedSections.filter(
          (section) => section !== original && section !== destination,
        ),
      });
    } finally {
      changingRef.current = false;
      setChanging(false);
    }
  }
  async function moveProject(
    source: string,
    category: string,
    target?: string,
    after = false,
  ) {
    if (changingRef.current) return;
    const group = allGroups.find((item) => item.key === source);
    if (!group) return;
    changingRef.current = true;
    setChanging(true);
    setSidebarError("");
    try {
      if ((group.project.category ?? "") !== category) {
        if (!canOrganizeProjects || !group.device.online)
          throw new Error(t("请连接 Hub 并使用管理员账号整理项目分区。"));
        await onProjectCategory(group.device, group.project, category);
      }
      const next = target
        ? moveSidebarItem(projectOrder, source, target, after)
        : [...projectOrder.filter((key) => key !== source), source];
      saveLayout({
        ...layout,
        projects: next,
        sections: sectionOrder,
        collapsedSections: layout.collapsedSections.filter(
          (name) => name !== category,
        ),
      });
    } catch (cause) {
      setSidebarError(
        cause instanceof Error ? cause.message : t("分区更新失败，请重试。"),
      );
    } finally {
      changingRef.current = false;
      setChanging(false);
    }
  }
  function beginDrag(
    event: PointerEvent<HTMLElement>,
    kind: "section" | "project",
    id: string,
  ) {
    if (
      event.button !== 0 ||
      event.pointerType === "touch" ||
      changingRef.current ||
      (event.target as Element).closest(".icon-button")
    )
      return;
    dragSource.current = {
      kind,
      id,
      x: event.clientX,
      y: event.clientY,
      active: false,
      destination: null,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }
  function updateDrag(event: PointerEvent<HTMLElement>) {
    const source = dragSource.current;
    if (
      !source ||
      (!source.active &&
        Math.hypot(event.clientX - source.x, event.clientY - source.y) < 5)
    )
      return;
    event.preventDefault();
    if (!source.active) {
      source.active = true;
      setDragging({ kind: source.kind, id: source.id });
    }
    const hit = document.elementFromPoint(event.clientX, event.clientY);
    const section = hit?.closest<HTMLElement>(".workspace-section");
    const project =
      source.kind === "project"
        ? hit?.closest<HTMLElement>(".workspace-group")
        : null;
    const target = project ?? section;
    const id = target?.getAttribute(project ? "data-project" : "data-section");
    if (
      !target ||
      id == null ||
      (source.kind === (project ? "project" : "section") && source.id === id)
    ) {
      source.destination = null;
      setDrop(null);
      return;
    }
    const rect = target.getBoundingClientRect();
    source.destination = {
      kind: project ? "project" : "section",
      id,
      after: event.clientY > rect.top + rect.height / 2,
    };
    setDrop(source.destination);
    const list = sidebar.current?.querySelector<HTMLElement>(".session-list");
    if (list) {
      const bounds = list.getBoundingClientRect();
      if (event.clientY < bounds.top + 24) list.scrollTop -= 12;
      else if (event.clientY > bounds.bottom - 24) list.scrollTop += 12;
    }
  }
  function endDrag(event: PointerEvent<HTMLElement>) {
    const source = dragSource.current;
    dragSource.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    setDragging(null);
    setDrop(null);
    if (!source?.active) return;
    suppressClick.current = true;
    setTimeout(() => {
      suppressClick.current = false;
    }, 0);
    const destination = source.destination;
    if (!destination || event.type === "pointercancel") return;
    if (source.kind === "section") {
      saveLayout({
        ...layout,
        sections: moveSidebarItem(
          sectionOrder,
          source.id,
          destination.id,
          destination.after,
        ),
      });
    } else {
      const target = allGroups.find((group) => group.key === destination.id);
      if (destination.kind === "project" && target)
        void moveProject(
          source.id,
          target.project.category ?? "",
          target.key,
          destination.after,
        );
      else if (destination.kind === "section")
        void moveProject(source.id, destination.id);
    }
  }
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
      (a, b) => projectOrder.indexOf(a.key) - projectOrder.indexOf(b.key),
    );
    return { groups, recent: grouped.recent };
  }, [
    devices,
    visible,
    scope,
    deviceId,
    search,
    layout.projects,
    getLanguage(),
  ]);
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
            aria-label={t("新建分区")}
            title={t("新建分区")}
            onClick={() => setSectionDialog({ mode: "create", name: "" })}
          >
            <Plus size={15} />
          </button>
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
        {sidebarError && (
          <p className="workspace-project-error" role="alert">
            {sidebarError}
            <button
              className="icon-button"
              aria-label={t("关闭")}
              onClick={() => setSidebarError("")}
            >
              <X size={12} />
            </button>
          </p>
        )}
        <nav
          className="session-list"
          aria-label={t("历史对话")}
          onClickCapture={(event) => {
            if (!suppressClick.current) return;
            suppressClick.current = false;
            event.preventDefault();
            event.stopPropagation();
          }}
        >
          {sectionOrder.map((category, sectionIndex) => {
            const sectionGroups = groups.filter(
              (group) => (group.project.category ?? "") === category,
            );
            const sectionMembers = allGroups.filter(
              (group) => (group.project.category ?? "") === category,
            );
            if (
              (scope === "unread" || search.trim()) &&
              !sectionGroups.length &&
              !navigationMatches(search, category)
            )
              return null;
            if (scope === "unread" && !sectionGroups.length) return null;
            const name = category || t("项目");
            const sectionSessions = sectionMembers.flatMap(
              (group) => group.sessions,
            );
            const expanded =
              !!search.trim() || !collapsedCategories.has(category);
            return (
              <section
                className="workspace-section"
                key={category}
                data-section={category}
                data-drop={
                  drop?.kind === "section" && drop.id === category
                    ? dragging?.kind === "section"
                      ? drop.after
                        ? "after"
                        : "before"
                      : "inside"
                    : undefined
                }
              >
                <WorkspaceSectionMenu
                  name={name}
                  editable={
                    !!category &&
                    (canOrganizeProjects || !sectionMembers.length) &&
                    !changing
                  }
                  canMoveUp={sectionIndex > 0 && !changing}
                  canMoveDown={
                    sectionIndex < sectionOrder.length - 1 && !changing
                  }
                  onCreate={() =>
                    setSectionDialog({ mode: "create", name: "" })
                  }
                  onRename={() =>
                    setSectionDialog({ mode: "rename", name: category })
                  }
                  onRemove={() =>
                    setSectionDialog({ mode: "remove", name: category })
                  }
                  onMove={(direction) => moveSection(category, direction)}
                  onRead={
                    onReadSessions &&
                    sectionSessions.some((session) =>
                      unreadSessions.has(session.id),
                    )
                      ? () => onReadSessions(sectionSessions)
                      : undefined
                  }
                >
                  <div
                    className="workspace-section-heading"
                    data-sortable={!changing}
                    onPointerDown={(event) =>
                      beginDrag(event, "section", category)
                    }
                    onPointerMove={updateDrag}
                    onPointerUp={endDrag}
                    onPointerCancel={endDrag}
                    onKeyDown={sidebarMenuKey}
                  >
                    <button
                      className="workspace-category"
                      aria-expanded={expanded}
                      title={t("拖拽排序，右键或菜单按钮整理分区")}
                      onClick={() =>
                        toggleCollapsed("collapsedSections", category)
                      }
                    >
                      <ChevronDown
                        size={12}
                        className={!expanded ? "collapsed" : ""}
                      />
                      <span>{name}</span>
                      <small>{sectionMembers.length}</small>
                    </button>
                    <button
                      className="icon-button"
                      aria-label={t("{0} 分区操作", name)}
                      onClick={(event) =>
                        openSidebarContextMenu(event.currentTarget)
                      }
                    >
                      <MoreHorizontal size={14} />
                    </button>
                  </div>
                </WorkspaceSectionMenu>
                {expanded &&
                  sectionGroups.map((group) => {
                    const peers = allGroups
                      .filter(
                        (entry) => (entry.project.category ?? "") === category,
                      )
                      .sort(
                        (a, b) =>
                          projectOrder.indexOf(a.key) -
                          projectOrder.indexOf(b.key),
                      );
                    const position = peers.findIndex(
                      (entry) => entry.key === group.key,
                    );
                    return (
                      <section
                        className="workspace-group"
                        key={group.key}
                        data-project={group.key}
                        data-drop={
                          drop?.kind === "project" && drop.id === group.key
                            ? drop.after
                              ? "after"
                              : "before"
                            : undefined
                        }
                      >
                        <WorkspaceProjectMenu
                          device={group.device}
                          project={group.project}
                          categories={categories}
                          canCategorize={canOrganizeProjects && !changing}
                          conversationCount={group.sessions.length}
                          onNew={() => onProject(group.device, group.projectId)}
                          onEdit={(destination) =>
                            onEditProject(
                              group.device,
                              group.project,
                              destination,
                            )
                          }
                          onNewSection={() =>
                            setSectionDialog({
                              mode: "create",
                              name: "",
                              project: group,
                            })
                          }
                          onMove={(direction) => {
                            const target = peers[position + direction];
                            if (target)
                              void moveProject(
                                group.key,
                                category,
                                target.key,
                                direction === 1,
                              );
                          }}
                          canMoveUp={position > 0 && !changing}
                          canMoveDown={position < peers.length - 1 && !changing}
                          onCategory={(destination) =>
                            moveProject(group.key, destination)
                          }
                          onRemove={
                            canRemoveProjects &&
                            !changing &&
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
                          <div
                            className="workspace-group-heading"
                            data-sortable={!changing}
                            onKeyDown={sidebarMenuKey}
                            onPointerDown={(event) =>
                              beginDrag(event, "project", group.key)
                            }
                            onPointerMove={updateDrag}
                            onPointerUp={endDrag}
                            onPointerCancel={endDrag}
                          >
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
                                toggleCollapsed("collapsedProjects", group.key)
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
                              aria-label={t("{0} 项目操作", group.name)}
                              onClick={(event) =>
                                openSidebarContextMenu(event.currentTarget)
                              }
                            >
                              <MoreHorizontal size={14} />
                            </button>
                            <button
                              className="icon-button"
                              aria-label={t("在 {0} 新建对话", group.name)}
                              onClick={() =>
                                onProject(group.device, group.projectId)
                              }
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
                      </section>
                    );
                  })}
                {expanded &&
                  !sectionGroups.length &&
                  !search.trim() &&
                  scope === "all" && (
                    <p className="workspace-section-empty">
                      {t("拖拽项目到此分区")}
                    </p>
                  )}
              </section>
            );
          })}
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
      {sectionDialog && (
        <WorkspaceSectionDialog
          mode={sectionDialog.mode}
          name={sectionDialog.name}
          count={
            allGroups.filter(
              (group) => (group.project.category ?? "") === sectionDialog.name,
            ).length
          }
          onSave={saveSection}
          onClose={() => setSectionDialog(null)}
        />
      )}
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
