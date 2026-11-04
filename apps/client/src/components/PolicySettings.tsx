import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, History, Layers3, RefreshCw, Settings2 } from "lucide-react";
import {
  MewDexClient,
  type Policy,
  type PromptInspection,
  type Session,
} from "@mewdex/sdk";
import { ModelSettings } from "../hub-controller/ModelSettings";
import { reasoningLabel } from "../modelSelection";
import { SettingsFrame } from "./SettingsFrame";
import { PromptText } from "./PromptText";
import type { PolicyEditor } from "../usePolicyEditor";

type Tab = "settings" | "preview" | "sources" | "history";
export function PolicySettings({
  client,
  open,
  onOpenChange,
  deviceId,
  deviceName = "当前设备",
  projectId,
  session,
  online,
  capabilities,
  editor,
  onApply,
}: {
  client: MewDexClient;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  deviceId: string;
  deviceName?: string;
  projectId: string;
  session?: Session;
  online: boolean;
  capabilities: readonly string[];
  editor: PolicyEditor;
  onApply: (policy: Policy, version: string) => void;
}) {
  const cache = useQueryClient();
  const [tab, setTab] = useState<Tab>("settings");
  const { draft, basis, busy, error } = editor;
  const currentlyOpen = useRef(open);
  currentlyOpen.current = open;
  const [preview, setPreview] = useState<PromptInspection | null>(null);
  useEffect(() => {
    if (open) {
      setPreview(null);
      setTab("settings");
    }
  }, [open, editor.key]);
  const sources = useQuery({
    queryKey: [
      "prompt-sources",
      deviceId,
      session?.id ?? projectId,
      session?.policyVersion,
    ],
    queryFn: () =>
      client.inspectPolicy({
        deviceId,
        projectId: session?.projectId ?? projectId,
        sessionId: session?.id,
      }),
    enabled: open && online && !!deviceId,
    retry: false,
  });
  const history = useQuery({
    queryKey: ["policies", session?.id],
    queryFn: () => client.policies(session!.id),
    enabled: open && !!session,
  });
  const editable =
    !session ||
    (session.executionRecovery?.state !== "retired" &&
      ["idle", "paused", "failed"].includes(session.state));
  function update(value: Partial<Policy>) {
    editor.change(value);
    setPreview(null);
  }
  function applySaved(value: Session) {
    cache.setQueryData<Session[]>(["sessions"], (old) =>
      old?.map((s) => (s.id === value.id ? value : s)),
    );
    editor.complete(editor.key, editor.entry);
    onApply(value.policy, value.policyVersion);
    if (editor.isCurrent()) setPreview(null);
    void cache.invalidateQueries({ queryKey: ["policies", value.id] });
    void cache.invalidateQueries({ queryKey: ["prompt-sources", deviceId] });
  }
  async function perform<T>(
    operation: () => Promise<T>,
    completed: (value: T) => void,
  ) {
    const submission = editor.begin();
    if (!submission) return;
    let value: T;
    try {
      value = await operation();
    } catch (e) {
      editor.finish(submission, (e as Error).message);
      return;
    }
    if (editor.finish(submission)) completed(value);
  }
  function save() {
    if (!session) {
      onApply(draft, "");
      editor.complete(editor.key, editor.entry);
      onOpenChange(false);
      return;
    }
    void perform(
      () => client.savePolicy(session, draft, basis),
      (value) => {
        applySaved(value);
        if (editor.isCurrent() && currentlyOpen.current) onOpenChange(false);
      },
    );
  }
  function restore(version: string) {
    if (!session) return;
    void perform(
      () => client.restorePolicy(session, version, basis),
      (value) => {
        applySaved(value);
        if (editor.isCurrent() && currentlyOpen.current) setTab("settings");
      },
    );
  }
  function inspect() {
    void perform(
      () =>
        client.inspectPolicy(
          {
            deviceId,
            projectId: session?.projectId ?? projectId,
            sessionId: session?.id,
          },
          draft,
        ),
      (inspected) => {
        if (!editor.isCurrent() || !currentlyOpen.current) return;
        setPreview(inspected);
        setTab((current) => (current === "settings" ? "preview" : current));
      },
    );
  }
  function resetDraft() {
    editor.reset();
    setPreview(null);
    setTab("settings");
  }
  return (
    <SettingsFrame<Tab>
      open={open}
      onOpenChange={onOpenChange}
      title="执行策略与提示词"
      description={`${deviceName} · ${session ? `${session.title}，保存后从下一回合生效。` : "当前新对话，应用策略后仍需发送消息才开始工作。"}`}
      className="policy-dialog"
      sizeKey="policy"
      closeLabel="关闭执行策略"
      navigation="tabs"
      navigationLabel="策略设置内容"
      selected={tab}
      onSelect={setTab}
      sections={[
        { id: "settings", label: "本回合策略", icon: <Settings2 size={14} /> },
        ...(preview
          ? [
              {
                id: "preview" as const,
                label: "指令预览",
                icon: <FileText size={14} />,
              },
            ]
          : []),
        { id: "sources", label: "指令来源", icon: <Layers3 size={14} /> },
        {
          id: "history",
          label: "版本历史",
          icon: <History size={14} />,
          disabled: !session,
        },
      ]}
      footer={
        <>
          {error && (
            <div className="error" role="alert">
              {error}
            </div>
          )}
          {!editable && (
            <p className="policy-note">
              {session?.executionRecovery?.state === "retired"
                ? "此执行已结束，策略与历史版本保留供查阅。新建对话后可设置新策略。"
                : "任务结束后可保存新策略。运行中的任务可通过追加消息调整。"}
            </p>
          )}
          <div className="policy-footer">
            <p className="policy-note">
              {busy
                ? "正在处理，请稍候…"
                : editor.dirty
                  ? "未应用更改，关闭后暂存在本次工作台。"
                  : session
                    ? "仅作用于当前对话，不会启动任务。"
                    : "仅作用于当前新对话。"}
            </p>
            <div className="policy-footer-actions">
              {editor.dirty && (
                <button
                  disabled={busy}
                  onClick={resetDraft}
                  title="使用当前已应用的策略，放弃尚未应用的填写"
                >
                  还原填写
                </button>
              )}
              {tab !== "settings" && (
                <button onClick={() => setTab("settings")}>返回策略</button>
              )}
              {tab === "settings" && (
                <button
                  disabled={!online || busy || !deviceId}
                  onClick={() => void inspect()}
                >
                  预览任务指令
                </button>
              )}
              {tab === "settings" || tab === "preview" ? (
                <button
                  className="primary"
                  disabled={!editable || busy || (!!session && !online)}
                  onClick={() => void save()}
                >
                  {busy
                    ? "正在处理…"
                    : session
                      ? "保存到下一回合"
                      : "应用到新对话"}
                </button>
              ) : (
                <button className="primary" onClick={() => onOpenChange(false)}>
                  关闭
                </button>
              )}
            </div>
          </div>
        </>
      }
    >
      {tab === "settings" && (
        <div className="policy-fields">
          {editor.stale && (
            <p className="policy-edit-notice" role="status">
              此对话的策略已有更新，你的填写仍保留。还原填写可载入最新策略。
            </p>
          )}
          {(capabilities.includes("collaboration-modes") ||
            draft.collaborationMode === "plan") && (
            <label>
              协作方式
              <select
                value={draft.collaborationMode ?? ""}
                disabled={!editable || busy}
                onChange={(event) =>
                  update({
                    collaborationMode: event.target.value || undefined,
                  })
                }
              >
                <option value="">直接工作</option>
                <option
                  value="plan"
                  disabled={!capabilities.includes("collaboration-modes")}
                >
                  先讨论方案
                </option>
              </select>
              <small className="policy-note">
                {!capabilities.includes("collaboration-modes")
                  ? "当前网络或执行端尚未支持方案讨论，需要更新相应组件后使用。"
                  : draft.collaborationMode === "plan"
                    ? "先调查与讨论、提出方案，确认后切换为直接工作。项目权限保持原配置。"
                    : "按当前目标推进工作，验证范围另行设置。"}
              </small>
            </label>
          )}
          <label>
            验证范围
            <select
              value={draft.mode}
              disabled={!editable || busy}
              onChange={(e) => update({ mode: e.target.value })}
            >
              <option value="edit">直接修改</option>
              <option value="focused">局部验证（默认）</option>
              <option value="full">完整验证</option>
            </select>
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              disabled={!editable || busy}
              checked={draft.noTests}
              onChange={(e) => update({ noTests: e.target.checked })}
            />
            本任务不要测试
          </label>
          <ModelSettings
            key={`${deviceId}:${session?.id ?? projectId}`}
            client={client}
            deviceId={deviceId}
            projectId={session?.projectId ?? projectId}
            sessionId={session?.id}
            inheritedModel={session?.defaultModel}
            catalogSupported={capabilities.includes("model-catalog")}
            reasoningSupported={capabilities.includes("reasoning-policy")}
            online={online}
            disabled={!editable || busy}
            value={draft}
            onChange={update}
          />
          <label>
            访问权限
            <select
              aria-label="任务访问权限"
              value={draft.accessMode ?? ""}
              disabled={
                !editable || busy || !capabilities.includes("access-modes")
              }
              onChange={(event) =>
                update({ accessMode: event.target.value || undefined })
              }
            >
              <option value="read-only">只读访问</option>
              <option value="">工作区权限</option>
              <option value="workspace-network">工作区与网络</option>
            </select>
            <small className="policy-note">
              控制下一回合的项目读写与网络访问。工作区外的额外访问仍需要单独批准。
            </small>
          </label>
          <label>
            完成条件
            <textarea
              disabled={!editable || busy}
              value={draft.completion}
              onChange={(e) => update({ completion: e.target.value })}
              placeholder="达到什么条件后结束工作"
            />
          </label>
          <label>
            追加指令
            <textarea
              disabled={!editable || busy}
              value={draft.instructions}
              onChange={(e) => update({ instructions: e.target.value })}
              placeholder="仅作用于当前对话的任务策略"
            />
          </label>
          <details className="policy-advanced">
            <summary>
              基础指令（高级）
              <small>
                {draft.baseInstructions != null ? "已替换" : "继承原设置"}
              </small>
            </summary>
            <div>
              <label className="checkbox">
                <input
                  type="checkbox"
                  disabled={!editable || busy}
                  checked={
                    draft.baseInstructions !== undefined &&
                    draft.baseInstructions !== null
                  }
                  onChange={(e) =>
                    update({
                      baseInstructions: e.target.checked
                        ? (sources.data?.sources.find(
                            (s) => s.kind === "engine-base",
                          )?.text ?? "")
                        : null,
                    })
                  }
                />
                替换本会话的基础指令
              </label>
              {draft.baseInstructions !== undefined &&
                draft.baseInstructions !== null && (
                  <label>
                    基础指令
                    <textarea
                      className="base-instructions-editor"
                      disabled={!editable || busy}
                      value={draft.baseInstructions}
                      onChange={(e) =>
                        update({ baseInstructions: e.target.value })
                      }
                    />
                  </label>
                )}
              <p className="policy-note">
                关闭替换后继承会话初始指令。已有对话历史会保留。
              </p>
            </div>
          </details>
        </div>
      )}
      {tab === "preview" && preview && (
        <div className="prompt-preview-pane">
          <p className="policy-note">
            以下为当前策略生成的任务指令，预览不会启动模型回合。
          </p>
          <PromptText
            label="将发送的任务指令"
            text={preview.developerInstructions}
          />
          <details className="policy-version-detail">
            <summary>版本信息</summary>
            <code>{preview.version}</code>
          </details>
        </div>
      )}
      {tab === "sources" && (
        <div className="prompt-sources">
          <div className="policy-content-heading">
            <p className="policy-note">
              查看执行端提供的来源，完整内容按需展开。
            </p>
            <button
              className="policy-read-action"
              disabled={!online || sources.isFetching}
              onClick={() => void sources.refetch()}
            >
              <RefreshCw size={13} />
              {sources.isFetching ? "读取中…" : "重新读取"}
            </button>
          </div>
          {sources.isPending && online && (
            <p className="policy-note">正在读取执行端指令来源…</p>
          )}
          {!online && (
            <p className="policy-note">
              设备离线，
              {sources.data
                ? "下方保留上次读取的来源。"
                : "暂时无法读取指令来源。"}
            </p>
          )}
          {sources.error && (
            <div className="error">{sources.error.message}</div>
          )}
          {sources.data?.notes.map((note) => (
            <p className="policy-note" key={note}>
              {note}
            </p>
          ))}
          {sources.data?.sources.map((source) => (
            <details key={source.id}>
              <summary>
                <strong>{source.label}</strong>
                <small>
                  {source.enabled
                    ? source.readOnly
                      ? "执行端来源"
                      : "当前任务"
                    : "未启用"}
                </small>
              </summary>
              {source.path && <code>{source.path}</code>}
              {source.note && <p className="policy-note">{source.note}</p>}
              {source.text && (
                <PromptText label="来源指令" text={source.text} />
              )}
            </details>
          ))}
        </div>
      )}
      {tab === "history" && (
        <div className="policy-history">
          <div className="policy-content-heading">
            <p className="policy-note">
              查看保存过的策略，恢复后从下一回合生效。
            </p>
            <button
              className="policy-read-action"
              disabled={history.isFetching}
              onClick={() => void history.refetch()}
            >
              <RefreshCw size={13} />
              {history.isFetching ? "读取中…" : "刷新版本"}
            </button>
          </div>
          {history.isPending && <p className="policy-note">正在读取版本…</p>}
          {history.isSuccess && !history.data.length && (
            <p className="policy-note">还没有可查看的策略版本。</p>
          )}
          {history.error && (
            <div className="error">{history.error.message}</div>
          )}
          {history.data?.map((value) => (
            <article key={value.id}>
              <div>
                <strong>
                  {value.policy.collaborationMode === "plan"
                    ? "方案讨论"
                    : "直接工作"}{" "}
                  ·{" "}
                  {value.policy.mode === "focused"
                    ? "局部验证"
                    : value.policy.mode === "edit"
                      ? "直接修改"
                      : "完整验证"}
                </strong>
                {value.version === session?.policyVersion && (
                  <span>当前版本</span>
                )}
              </div>
              <time className="policy-history-time" dateTime={value.createdAt}>
                {new Date(value.createdAt).toLocaleString("zh-CN")}
              </time>
              <p>
                {value.policy.noTests ? "本任务不要测试" : "遵循所选验证范围"}
                {value.policy.completion
                  ? ` · 完成条件：${value.policy.completion}`
                  : ""}
              </p>
              {(value.policy.model || value.policy.reasoningEffort) && (
                <p>
                  {value.policy.model || "继承模型"}
                  {value.policy.reasoningEffort
                    ? ` · 推理强度：${reasoningLabel(value.policy.reasoningEffort)}`
                    : ""}
                </p>
              )}
              <details>
                <summary>查看指令与版本信息</summary>
                <code className="policy-version-code">{value.version}</code>
                <PromptText
                  label="当时发送的任务指令"
                  text={value.developerInstructions}
                />
              </details>
              <button
                disabled={
                  !editable ||
                  busy ||
                  !online ||
                  value.version === session?.policyVersion
                }
                onClick={() => void restore(value.version)}
              >
                恢复到下一回合
              </button>
            </article>
          ))}
        </div>
      )}
    </SettingsFrame>
  );
}
