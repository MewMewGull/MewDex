import { useId, useRef, type ReactNode, type RefObject } from "react";
import {
  ArrowUp,
  LoaderCircle,
  Plus,
  FolderOpen,
  ListPlus,
  Square,
} from "lucide-react";
import type { ComposerSubmission } from "../composerSubmission";
import { hasTouchInput } from "../inputViewport";
import { sendKeyHint, shouldSendMessage, type SendKey } from "../inputKeys";

export function ConversationComposer({
  editor,
  files,
  actions,
  sendKey,
  context,
  status,
  planMode,
  deviceControl,
  projectControl,
  projectLabel = "选择项目",
  children,
  controls,
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
    onSend: (kind?: "enqueue") => void;
    onStop: () => void;
  };
  sendKey: SendKey;
  context: string;
  status: string;
  planMode: boolean;
  deviceControl?: ReactNode;
  projectControl?: ReactNode;
  projectLabel?: string;
  children?: ReactNode;
  controls?: ReactNode;
}) {
  const fileInput = useRef<HTMLInputElement>(null);
  const hintId = useId();
  const { intent } = actions;
  const touch = hasTouchInput();
  const shortcut = sendKeyHint(
    sendKey,
    touch,
    undefined,
    intent.command === "steer" ? "追加" : "发送",
  );
  return (
    <>
      <div className="composer-context" role="group" aria-label="项目与执行设备">
        <div className="composer-project-control" title={context}>
          <FolderOpen size={16} />
          <span className="composer-project-label">{projectLabel}</span>
          {projectControl}
        </div>
        {deviceControl}
      </div>
      <div className="composer" aria-busy={actions.busy}>
        {children}
        <textarea
          ref={editor.ref}
          data-draft-key={editor.draftKey}
          aria-label="对话输入"
          aria-describedby={hintId}
          title={context}
          readOnly={editor.readOnly}
          value={editor.text}
          onChange={(event) => {
            editor.onChange(event.target.value);
            editor.onRemember(event.currentTarget);
          }}
          onSelect={(event) => editor.onRemember(event.currentTarget)}
          onScroll={(event) => editor.onRemember(event.currentTarget)}
          onBlur={(event) => editor.onRemember(event.currentTarget)}
          onPaste={(event) => {
            if (!event.clipboardData.files.length) return;
            event.preventDefault();
            files.onAdd(event.clipboardData.files);
          }}
          placeholder={editor.placeholder}
          onKeyDown={(event) => {
            if (
              shouldSendMessage(event.nativeEvent, sendKey, hasTouchInput())
            ) {
              event.preventDefault();
              if (actions.canSend) actions.onSend();
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
              aria-label="添加附件"
              disabled={!files.canAdd}
              title={files.canAdd ? "添加附件" : files.unavailable}
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
                onClick={() => actions.onSend("enqueue")}
              >
                <ListPlus size={16} />
              </button>
            )}
            {actions.canStop && (
              <button
                className="stop-button"
                aria-label="停止任务和后台进程"
                title="停止当前任务与后台进程，保留未发送输入。"
                onClick={actions.onStop}
              >
                <Square size={13} fill="currentColor" />
              </button>
            )}
            <button
              className="send-button"
              aria-label={actions.busy ? "正在发送消息" : intent.label}
              title={`${intent.description}${intent.command ? ` ${shortcut}` : ""}`}
              disabled={!actions.canSend}
              onClick={() => actions.onSend()}
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
          {planMode && <span className="composer-plan-mode">方案讨论 · </span>}
          {actions.busy
            ? "正在发送…"
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
