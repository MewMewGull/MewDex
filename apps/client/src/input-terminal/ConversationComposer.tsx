import { t } from "../localization.ts";
import {
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import {
  ArrowUp,
  LoaderCircle,
  Plus,
  FolderOpen,
  ListPlus,
  Square,
  RefreshCw,
  Sparkles,
  Terminal,
} from "lucide-react";
import type { Device, MewDexClient, Session } from "@mewdex/sdk";
import type { ComposerSubmission } from "../composerSubmission";
import { hasTouchInput } from "../inputViewport";
import { sendKeyHint, shouldSendMessage, type SendKey } from "../inputKeys";
import {
  composerSlashQuery,
  filterComposerCommands,
  typedComposerCommand,
  type ComposerCommandEntry,
  type ComposerCommandId,
} from "../composerCommands";
import { useComposerCommands } from "../useComposerCommands";

export function ConversationComposer({
  editor,
  files,
  actions,
  sendKey,
  context,
  status,
  planMode,
  deviceControl,
  executorStatus,
  projectControl,
  projectLabel = t("选择项目"),
  children,
  controls,
  commands,
}: {
  editor: {
    ref: RefObject<HTMLTextAreaElement | null>;
    draftKey: string;
    text: string;
    readOnly: boolean;
    placeholder: string;
    onChange: (text: string) => void;
    onRemember: (input: HTMLTextAreaElement) => void;
  };
  files: {
    canAdd: boolean;
    unavailable: string;
    onAdd: (files: FileList | null) => void;
  };
  actions: {
    intent: ComposerSubmission;
    busy: boolean;
    canSend: boolean;
    canQueue: boolean;
    canStop: boolean;
    onSend: (kind?: "enqueue", preparedText?: string) => void;
    onStop: () => void;
  };
  sendKey: SendKey;
  context: string;
  status: string;
  planMode: boolean;
  deviceControl?: ReactNode;
  executorStatus?: ReactNode;
  projectControl?: ReactNode;
  projectLabel?: string;
  children?: ReactNode;
  controls?: ReactNode;
  commands?: {
    client: MewDexClient;
    device?: Device;
    projectId: string;
    session?: Session;
    connected: boolean;
    onCommand: (command: ComposerCommandId) => void;
  };
}) {
  const fileInput = useRef<HTMLInputElement>(null);
  const hintId = useId();
  const menuId = useId();
  const menu = useRef<HTMLDivElement>(null);
  const [focused, setFocused] = useState(false);
  const [selection, setSelection] = useState({
    key: editor.draftKey,
    start: editor.text.length,
    end: editor.text.length,
  });
  const [dismissed, setDismissed] = useState("");
  const [highlight, setHighlight] = useState({ query: "", id: "" });
  const [activeCommand, setActiveCommand] = useState<string>();
  const query =
    commands && !editor.readOnly && selection.key === editor.draftKey
      ? composerSlashQuery(editor.text, selection.start, selection.end)
      : null;
  const queryKey = `${editor.draftKey}:${editor.text}:${selection.start}:${selection.end}`;
  const open = !!query && focused && dismissed !== queryKey;
  // The catalog component is mounted only when the caller supplies a target.
  // This keeps existing lightweight composer consumers independent of QueryClient.
  const { intent } = actions;
  const touch = hasTouchInput();
  const shortcut = sendKeyHint(
    sendKey,
    touch,
    undefined,
    intent.command === "steer" ? t("追加") : t("发送"),
  );
  function rememberSelection(input: HTMLTextAreaElement) {
    editor.onRemember(input);
    setSelection({
      key: editor.draftKey,
      start: input.selectionStart,
      end: input.selectionEnd,
    });
  }
  function replaceRange(
    start: number,
    end: number,
    insertion: string,
    restoreFocus = true,
  ) {
    const key = editor.draftKey;
    editor.onChange(
      editor.text.slice(0, start) + insertion + editor.text.slice(end),
    );
    setDismissed("");
    if (!restoreFocus) return;
    requestAnimationFrame(() => {
      const input = editor.ref.current;
      if (!input || input.dataset.draftKey !== key) return;
      input.focus({ preventScroll: true });
      input.setSelectionRange(
        start + insertion.length,
        start + insertion.length,
      );
      rememberSelection(input);
    });
  }
  const commandActions = useRef<{
    keyDown: (event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean;
    send: (kind?: "enqueue") => void;
  } | null>(null);
  return (
    <>
      <div
        className="composer-context"
        role="group"
        aria-label={t("项目与 Hub 连接")}
      >
        <div className="composer-project-control" title={context}>
          <FolderOpen size={16} />
          <span className="composer-project-label">{projectLabel}</span>
          {projectControl}
        </div>
        {deviceControl}
      </div>
      {executorStatus}
      <div className="composer" aria-busy={actions.busy}>
        {commands && !editor.readOnly && (
          <ComposerCommandMenu
            key={editor.draftKey}
            {...commands}
            text={editor.text}
            query={query}
            queryKey={queryKey}
            open={open}
            menuId={menuId}
            menu={menu}
            highlight={highlight}
            onHighlight={setHighlight}
            onDismiss={() => setDismissed(queryKey)}
            onReplace={replaceRange}
            onSend={actions.onSend}
            commandActions={commandActions}
            onActiveId={setActiveCommand}
            onCommand={commands.onCommand}
          />
        )}
        {children}
        <textarea
          ref={editor.ref}
          data-draft-key={editor.draftKey}
          aria-label={t("对话输入")}
          aria-describedby={hintId}
          role={commands ? "combobox" : undefined}
          aria-autocomplete={commands ? "list" : undefined}
          aria-expanded={commands ? open : undefined}
          aria-controls={open ? menuId : undefined}
          aria-activedescendant={open ? activeCommand : undefined}
          title={context}
          readOnly={editor.readOnly}
          value={editor.text}
          onChange={(event) => {
            editor.onChange(event.target.value);
            rememberSelection(event.currentTarget);
          }}
          onSelect={(event) => rememberSelection(event.currentTarget)}
          onScroll={(event) => editor.onRemember(event.currentTarget)}
          onFocus={(event) => {
            setFocused(true);
            setDismissed("");
            rememberSelection(event.currentTarget);
          }}
          onBlur={(event) => {
            setFocused(false);
            editor.onRemember(event.currentTarget);
          }}
          onPaste={(event) => {
            if (!event.clipboardData.files.length) return;
            event.preventDefault();
            files.onAdd(event.clipboardData.files);
          }}
          placeholder={editor.placeholder}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing || event.keyCode === 229) return;
            if (commands && commandActions.current?.keyDown(event)) return;
            if (
              shouldSendMessage(event.nativeEvent, sendKey, hasTouchInput())
            ) {
              event.preventDefault();
              if (actions.canSend) {
                if (commands) commandActions.current?.send();
                else actions.onSend();
              }
            }
          }}
        />
        <div className="composer-toolbar">
          <div className="composer-options">
            <input
              ref={fileInput}
              className="file-input"
              type="file"
              multiple
              onChange={(event) => {
                const selected = event.target.files;
                files.onAdd(selected);
                event.target.value = "";
              }}
            />
            <button
              className="icon-button attachment-button"
              aria-label={t("添加附件")}
              disabled={!files.canAdd}
              title={files.canAdd ? t("添加附件") : files.unavailable}
              onClick={() => fileInput.current?.click()}
            >
              <Plus size={19} />
            </button>
            {projectControl && (
              <div className="composer-project-control" title={context}>
                <FolderOpen size={16} />
                <span className="composer-project-label">{projectLabel}</span>
                {projectControl}
              </div>
            )}
          </div>
          {controls}
          <div className="send-options">
            {intent.queueVisible && (
              <button
                className="queue-button"
                aria-label={intent.queueLabel}
                disabled={!actions.canQueue}
                title={intent.queueDescription}
                onClick={() =>
                  commands
                    ? commandActions.current?.send("enqueue")
                    : actions.onSend("enqueue")
                }
              >
                <ListPlus size={16} />
              </button>
            )}
            {actions.canStop && (
              <button
                className="stop-button"
                aria-label={t("停止任务和后台进程")}
                title={t("停止当前任务与后台进程，保留未发送输入。")}
                onClick={actions.onStop}
              >
                <Square size={13} fill="currentColor" />
              </button>
            )}
            <button
              className="send-button"
              aria-label={actions.busy ? t("正在发送消息") : intent.label}
              title={`${intent.description}${intent.command ? ` ${shortcut}` : ""}`}
              disabled={!actions.canSend}
              onClick={() =>
                commands ? commandActions.current?.send() : actions.onSend()
              }
            >
              {actions.busy ? (
                <LoaderCircle size={17} className="spinning" />
              ) : (
                <ArrowUp size={19} />
              )}
            </button>
          </div>
        </div>
      </div>
      <div className="sr-only" id={hintId}>
        <span title={context}>{context}</span>
        <span>
          {planMode && (
            <span className="composer-plan-mode">{t("方案讨论 · ")}</span>
          )}
          {actions.busy
            ? t("正在发送…")
            : status || (!intent.command ? intent.description : shortcut)}
        </span>
      </div>
      {status && (
        <p className="composer-status" role="status">
          {status}
        </p>
      )}
    </>
  );
}

function ComposerCommandMenu({
  client,
  device,
  projectId,
  session,
  connected,
  text,
  query,
  queryKey,
  open,
  menuId,
  menu,
  highlight,
  onHighlight,
  onDismiss,
  onReplace,
  onSend,
  onCommand,
  commandActions,
  onActiveId,
}: NonNullable<Parameters<typeof ConversationComposer>[0]["commands"]> & {
  text: string;
  query: ReturnType<typeof composerSlashQuery>;
  queryKey: string;
  open: boolean;
  menuId: string;
  menu: RefObject<HTMLDivElement | null>;
  highlight: { query: string; id: string };
  onHighlight: (value: { query: string; id: string }) => void;
  onDismiss: () => void;
  onReplace: (
    start: number,
    end: number,
    text: string,
    restoreFocus?: boolean,
  ) => void;
  onActiveId: (id: string | undefined) => void;
  onSend: (kind?: "enqueue", preparedText?: string) => void;
  commandActions: RefObject<{
    keyDown: (event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean;
    send: (kind?: "enqueue") => void;
  } | null>;
}) {
  const catalog = useComposerCommands({
    client,
    device,
    projectId,
    session,
    connected,
    enabled: !!query || /^\s*\/[^\s/]+(?:[ \t]|$)/u.test(text),
  });
  const entries = query ? filterComposerCommands(catalog.entries, query) : [];
  const selected = Math.max(
    0,
    entries.findIndex(
      (entry) => highlight.query === queryKey && entry.id === highlight.id,
    ),
  );
  const activeId = entries.length ? `${menuId}-${selected}` : undefined;
  useLayoutEffect(
    () => onActiveId(open ? activeId : undefined),
    [open, activeId, onActiveId],
  );
  useEffect(() => {
    if (open)
      menu.current
        ?.querySelector<HTMLElement>('[data-active="true"]')
        ?.scrollIntoView({ block: "nearest" });
  }, [open, selected, menu, entries.length]);
  function choose(entry: ComposerCommandEntry) {
    if (!query) return;
    if (entry.command === "skills") {
      onReplace(query.start, query.end, "/skills ");
      return;
    }
    onReplace(query.start, query.end, entry.insert ?? "", !entry.command);
    if (entry.command) onCommand(entry.command);
  }
  useImperativeHandle(commandActions, () => ({
    keyDown(event) {
      if (!open) return false;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onDismiss();
        return true;
      }
      if (entries.length && ["ArrowDown", "ArrowUp"].includes(event.key)) {
        event.preventDefault();
        const index =
          (selected + (event.key === "ArrowDown" ? 1 : -1) + entries.length) %
          entries.length;
        onHighlight({ query: queryKey, id: entries[index].id });
        return true;
      }
      if (
        (event.key === "Enter" || event.key === "Tab") &&
        !event.shiftKey &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.altKey
      ) {
        if (entries.length) {
          event.preventDefault();
          choose(entries[selected]);
          return true;
        }
        if (catalog.loading) {
          event.preventDefault();
          return true;
        }
      }
      return false;
    },
    send(kind) {
      const typed = typedComposerCommand(text, catalog.entries);
      if (typed?.entry.command) {
        if (typed.entry.command === "skills")
          onReplace(0, text.length, `/skills ${typed.rest}`);
        else {
          onReplace(0, text.length, typed.rest, false);
          onCommand(typed.entry.command);
        }
        return;
      }
      if (typed?.entry.insert) {
        onSend(kind, typed.entry.insert + typed.rest);
        return;
      }
      if (
        /^\s*\/(?:skills(?:\s|$)|\s*$)/u.test(text) ||
        (catalog.loading && /^\s*\/[^\s/]+(?:[ \t]|$)/u.test(text))
      )
        return;
      onSend(kind);
    },
  }));
  if (!open) return null;
  return (
    <div
      className="composer-command-menu"
      onMouseDown={(event) => event.preventDefault()}
    >
      <div className="composer-command-heading">
        <strong>{query?.skillsOnly ? t("Skills") : t("命令与 Skills")}</strong>
        <button
          type="button"
          className="icon-button"
          aria-label={t("刷新 Skill 列表")}
          disabled={catalog.loading || !connected}
          onClick={catalog.refresh}
        >
          <RefreshCw size={14} className={catalog.loading ? "spinning" : ""} />
        </button>
      </div>
      <div
        ref={menu}
        id={menuId}
        role="listbox"
        aria-label={t("命令与 Skills")}
        className="composer-command-list"
      >
        {entries.map((entry, index) => (
          <button
            type="button"
            role="option"
            id={`${menuId}-${index}`}
            key={entry.id}
            aria-selected={index === selected}
            data-active={index === selected}
            onPointerMove={() => onHighlight({ query: queryKey, id: entry.id })}
            onClick={() => choose(entry)}
          >
            {entry.source === "command" ? (
              <Terminal size={16} />
            ) : (
              <Sparkles size={16} />
            )}
            <span>
              <strong>
                <code>/{entry.name}</code>
                <span>{entry.label !== entry.name ? entry.label : ""}</span>
              </strong>
              <small>{entry.description}</small>
            </span>
            <small className="composer-command-source">
              {entry.source === "command"
                ? t("命令")
                : entry.source === "project"
                  ? t("项目 Skill")
                  : t("Hub Skill")}
            </small>
          </button>
        ))}
      </div>
      {catalog.loading && <p role="status">{t("正在读取可用 Skill…")}</p>}
      {!entries.length && !catalog.loading && (
        <p role="status">{t("没有匹配的命令或 Skill")}</p>
      )}
      {catalog.error && (
        <p className="error" role="alert">
          {catalog.error}
        </p>
      )}
      {!connected && <p>{t("连接 Hub 后可读取 Skill 列表")}</p>}
      {connected && !catalog.runtimeSupported && (
        <p>{t("更新 Hub 后可浏览其已安装的 Skill")}</p>
      )}
      <footer>{t("↑↓ 选择 · Enter / Tab 确认 · Esc 关闭")}</footer>
    </div>
  );
}
