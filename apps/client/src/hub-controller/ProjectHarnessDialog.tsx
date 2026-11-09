import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, Plus, Trash2, X } from "lucide-react";
import {
  commandID,
  type MewDexClient,
  type Device,
  type Project,
  type ProjectHarness,
  type ProjectHarnessHistory,
  type ProjectHarnessState,
  type ProjectScript,
  type ProjectSkill,
  type ProjectWorkflow,
} from "@mewdex/sdk";
import { t } from "../localization.ts";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

type Section =
  | "instructions"
  | "skills"
  | "memory"
  | "scripts"
  | "workflows"
  | "history";

function AssetMetadata({
  value,
  onChange,
}: {
  value: Pick<ProjectSkill, "id" | "name" | "description">;
  onChange: (
    patch: Partial<Pick<ProjectSkill, "id" | "name" | "description">>,
  ) => void;
}) {
  return (
    <>
      <label>
        {t("条目编号")}
        <input
          value={value.id}
          maxLength={64}
          onChange={(event) => onChange({ id: event.target.value })}
        />
        <small className="muted">
          {t("使用小写英文字母、数字和连字符，以字母开头。")}
        </small>
      </label>
      <label>
        {t("条目名称")}
        <input
          value={value.name}
          maxLength={100}
          onChange={(event) => onChange({ name: event.target.value })}
        />
      </label>
      <label>
        {t("条目说明")}
        <textarea
          rows={2}
          value={value.description}
          maxLength={4096}
          onChange={(event) => onChange({ description: event.target.value })}
        />
      </label>
    </>
  );
}

export function ProjectHarnessDialog({
  client,
  project,
  devices,
  onClose,
  initialSection = "instructions",
}: {
  client: MewDexClient;
  project: Project;
  devices: Device[];
  onClose: () => void;
  initialSection?: Section;
}) {
  const cache = useQueryClient();
  const [base, setBase] = useState<ProjectHarnessState | null>(null);
  const [draft, setDraft] = useState<ProjectHarness>({
    format: 1,
    instructions: "",
    skills: [],
    scripts: [],
    workflows: [],
  });
  const [history, setHistory] = useState<ProjectHarnessHistory>({ items: [] });
  const [preview, setPreview] = useState<ProjectHarnessState | null>(null);
  const [section, setSection] = useState<Section>(initialSection);
  const [selected, setSelected] = useState(0);
  const [busy, setBusy] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const focus = useDialogReturnFocus();

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([
      client.projectHarness(project.id, 0, controller.signal),
      client.projectHarnessHistory(project.id, 0, controller.signal),
    ])
      .then(([current, history]) => {
        if (controller.signal.aborted) return;
        setBase(current);
        setDraft(current.configuration);
        setHistory(history);
      })
      .catch((cause) => {
        if (!controller.signal.aborted)
          setError(
            cause instanceof Error ? cause.message : t("无法读取项目配置"),
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [client, project.id]);

  async function save() {
    if (!base || busy) return;
    setBusy(true);
    setSaving(true);
    setError("");
    setStatus("");
    try {
      const current = await client.saveProjectHarness(project.id, {
        expectedRevision: base.revision,
        expectedProjectRevision: base.projectRevision,
        configuration: draft,
      });
      setBase(current);
      setDraft(current.configuration);
      void cache.invalidateQueries({ queryKey: ["composer-project-skills"] });
      void cache.invalidateQueries({ queryKey: ["extension-project"] });
      setStatus(t("已保存为 v{0}，从下一回合开始使用。", current.revision));
      setSaving(false);
      setHistory(await client.projectHarnessHistory(project.id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("无法保存项目配置"));
    } finally {
      setBusy(false);
      setSaving(false);
    }
  }

  async function refresh() {
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const [current, history] = await Promise.all([
        client.projectHarness(project.id),
        client.projectHarnessHistory(project.id),
      ]);
      setBase(current);
      setDraft(current.configuration);
      setHistory(history);
      setPreview(null);
      setSelected(0);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("无法读取项目配置"));
    } finally {
      setBusy(false);
    }
  }

  async function inspectRevision(revision: number) {
    setBusy(true);
    setError("");
    try {
      setPreview(await client.projectHarness(project.id, revision));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("无法读取项目配置"));
    } finally {
      setBusy(false);
    }
  }

  async function moreHistory() {
    if (!history.nextBefore || busy) return;
    setBusy(true);
    setError("");
    try {
      const page = await client.projectHarnessHistory(
        project.id,
        history.nextBefore,
      );
      setHistory({
        items: [...history.items, ...page.items],
        nextBefore: page.nextBefore,
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("无法读取项目配置"));
    } finally {
      setBusy(false);
    }
  }

  function updateSkill(patch: Partial<ProjectSkill>) {
    setDraft((current) => ({
      ...current,
      skills: current.skills.map((skill, index) =>
        index === selected ? { ...skill, ...patch } : skill,
      ),
    }));
  }

  function updateScript(patch: Partial<ProjectScript>) {
    setDraft((current) => {
      const previous = current.scripts[selected];
      return {
        ...current,
        scripts: current.scripts.map((script, index) =>
          index === selected ? { ...script, ...patch } : script,
        ),
        workflows: current.workflows.map((workflow) => ({
          ...workflow,
          steps: workflow.steps.map((step) =>
            patch.id !== undefined && step === previous.id ? patch.id : step,
          ),
        })),
      };
    });
  }

  function updateWorkflow(patch: Partial<ProjectWorkflow>) {
    setDraft((current) => ({
      ...current,
      workflows: current.workflows.map((workflow, index) =>
        index === selected ? { ...workflow, ...patch } : workflow,
      ),
    }));
  }

  function add() {
    setStatus("");
    if (section === "skills") {
      setSelected(draft.skills.length);
      setDraft({
        ...draft,
        skills: [
          ...draft.skills,
          {
            id: `skill-${commandID().slice(0, 8)}`,
            name: "",
            description: "",
            instructions: "",
          },
        ],
      });
    }
    if (section === "scripts") {
      setSelected(draft.scripts.length);
      const directory = project.directories?.[0];
      setDraft({
        ...draft,
        scripts: [
          ...draft.scripts,
          {
            id: `script-${commandID().slice(0, 8)}`,
            name: "",
            description: "",
            deviceId: directory?.deviceId ?? "",
            projectId: directory?.projectId ?? "",
            command: [""],
            cwd: "",
            timeoutMs: 0,
          },
        ],
      });
    }
    if (section === "workflows") {
      setSelected(draft.workflows.length);
      setDraft({
        ...draft,
        workflows: [
          ...draft.workflows,
          {
            id: `workflow-${commandID().slice(0, 8)}`,
            name: "",
            description: "",
            instructions: "",
            steps: [draft.scripts[0]?.id ?? ""],
          },
        ],
      });
    }
  }

  const items =
    section === "skills"
      ? draft.skills
      : section === "scripts"
        ? draft.scripts
        : section === "workflows"
          ? draft.workflows
          : [];
  const skill = section === "skills" ? draft.skills[selected] : undefined;
  const script = section === "scripts" ? draft.scripts[selected] : undefined;
  const workflow =
    section === "workflows" ? draft.workflows[selected] : undefined;
  const referencedScript =
    !!script &&
    draft.workflows.some((workflow) => workflow.steps.includes(script.id));
  const dirty =
    !!base && JSON.stringify(base.configuration) !== JSON.stringify(draft);
  const sections: { id: Section; label: string }[] = [
    { id: "instructions", label: t("项目指令") },
    { id: "skills", label: t("Skills") },
    { id: "memory", label: t("Memory") },
    { id: "scripts", label: t("项目脚本") },
    { id: "workflows", label: t("工作流") },
    { id: "history", label: t("版本历史") },
  ];

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog project-harness-dialog"
          onOpenAutoFocus={focus.capture}
          onCloseAutoFocus={focus.restore}
          onEscapeKeyDown={(event) => {
            if (saving) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (saving) event.preventDefault();
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>
              {t("项目指令与自动化")} · {project.name}
            </Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭项目配置")}
              disabled={saving}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t(
              "配置由 Hub 保存。每个回合固定使用启动时的版本，排队输入在实际启动时读取。",
            )}
          </Dialog.Description>
          <div className="project-harness-version">
            {base && (
              <>
                <strong>{t("当前版本 v{0}", base.revision)}</strong>
                <code title={base.version}>{base.version.slice(0, 12)}</code>
              </>
            )}
            {busy && <LoaderCircle size={16} className="spinning" />}
          </div>
          <nav
            className="project-harness-sections"
            aria-label={t("项目配置分类")}
          >
            {sections.map((item) => (
              <button
                key={item.id}
                aria-pressed={section === item.id}
                disabled={busy || !base}
                onClick={() => {
                  setSection(item.id);
                  setSelected(0);
                }}
              >
                {item.label}
              </button>
            ))}
          </nav>
          <fieldset
            className="project-harness-content"
            disabled={busy || !base}
          >
            {section === "instructions" && (
              <label>
                {t("项目指令")}
                <textarea
                  rows={15}
                  value={draft.instructions}
                  maxLength={65536}
                  onChange={(event) =>
                    setDraft({ ...draft, instructions: event.target.value })
                  }
                />
              </label>
            )}
            {section === "memory" && (
              <div className="extension-form">
                <label className="checkbox">
                  <input
                    type="checkbox"
                    checked={draft.memory?.enabled ?? false}
                    onChange={(event) =>
                      setDraft({
                        ...draft,
                        memory: {
                          content: draft.memory?.content ?? "",
                          enabled: event.target.checked,
                        },
                      })
                    }
                  />
                  {t("启用项目记忆")}
                </label>
                <p className="muted">
                  {t(
                    "记录项目约定、长期偏好和已确认的事实。启用后随下一回合的项目快照提供，可在版本历史中恢复。",
                  )}
                </p>
                <label>
                  {t("项目记忆正文")}
                  <textarea
                    rows={15}
                    maxLength={65536}
                    value={draft.memory?.content ?? ""}
                    onChange={(event) =>
                      setDraft({
                        ...draft,
                        memory: {
                          enabled: draft.memory?.enabled ?? false,
                          content: event.target.value,
                        },
                      })
                    }
                  />
                </label>
              </div>
            )}
            {["skills", "scripts", "workflows"].includes(section) && (
              <div className="project-harness-assets">
                <aside>
                  {items.map((item, index) => (
                    <button
                      key={index}
                      aria-pressed={selected === index}
                      onClick={() => setSelected(index)}
                    >
                      <strong>{item.name || t("未命名条目")}</strong>
                      <small>{item.id}</small>
                    </button>
                  ))}
                  <button
                    onClick={add}
                    disabled={
                      items.length >= (section === "workflows" ? 32 : 64) ||
                      (section === "workflows" && !draft.scripts.length)
                    }
                  >
                    <Plus size={15} />
                    {t("添加条目")}
                  </button>
                </aside>
                <div className="project-harness-editor">
                  {!items.length && (
                    <p className="muted">
                      {section === "workflows" && !draft.scripts.length
                        ? t("先添加项目脚本，再组合工作流。")
                        : t("尚未添加条目")}
                    </p>
                  )}
                  {skill && (
                    <>
                      <AssetMetadata value={skill} onChange={updateSkill} />
                      <label>
                        {t("Skill 指令正文")}
                        <textarea
                          rows={12}
                          value={skill.instructions}
                          maxLength={65536}
                          onChange={(event) =>
                            updateSkill({ instructions: event.target.value })
                          }
                        />
                      </label>
                    </>
                  )}
                  {script && (
                    <>
                      <AssetMetadata value={script} onChange={updateScript} />
                      <label>
                        {t("执行目录")}
                        <select
                          value={JSON.stringify([
                            script.deviceId,
                            script.projectId,
                          ])}
                          onChange={(event) => {
                            const [deviceId, projectId] = JSON.parse(
                              event.target.value,
                            ) as string[];
                            updateScript({ deviceId, projectId });
                          }}
                        >
                          {!project.directories?.length && (
                            <option value='["",""]'>
                              {t("请先绑定项目目录")}
                            </option>
                          )}
                          {project.directories?.map((directory) => (
                            <option
                              key={JSON.stringify([
                                directory.deviceId,
                                directory.projectId,
                              ])}
                              value={JSON.stringify([
                                directory.deviceId,
                                directory.projectId,
                              ])}
                            >
                              {devices.find(
                                (device) => device.id === directory.deviceId,
                              )?.name || directory.deviceId}{" "}
                              · {directory.name} · {directory.path}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label>
                        {t("相对工作目录")}
                        <input
                          value={script.cwd}
                          placeholder="."
                          maxLength={4096}
                          onChange={(event) =>
                            updateScript({ cwd: event.target.value })
                          }
                        />
                        <small className="muted">
                          {t("使用项目内的相对路径，以 / 分隔目录。")}
                        </small>
                      </label>
                      <div className="project-script-arguments">
                        {script.command.map((argument, index) => (
                          <div key={index}>
                            <label>
                              {index === 0 ? t("程序") : t("参数 {0}", index)}
                              <textarea
                                rows={2}
                                value={argument}
                                onChange={(event) =>
                                  updateScript({
                                    command: script.command.map(
                                      (value, position) =>
                                        position === index
                                          ? event.target.value
                                          : value,
                                    ),
                                  })
                                }
                              />
                            </label>
                            {index > 0 && (
                              <button
                                className="icon-button"
                                aria-label={t("移除参数 {0}", index)}
                                onClick={() =>
                                  updateScript({
                                    command: script.command.filter(
                                      (_, position) => position !== index,
                                    ),
                                  })
                                }
                              >
                                <Trash2 size={14} />
                              </button>
                            )}
                          </div>
                        ))}
                        <button
                          onClick={() =>
                            updateScript({ command: [...script.command, ""] })
                          }
                          disabled={script.command.length >= 256}
                        >
                          <Plus size={15} />
                          {t("添加参数")}
                        </button>
                        <small className="muted">
                          {t(
                            "每项作为一个原始参数传递，需要 shell 时显式指定程序。",
                          )}
                        </small>
                      </div>
                      <label>
                        {t("超时秒数，0 使用默认值")}
                        <input
                          type="number"
                          min={0}
                          max={43200}
                          step={0.001}
                          value={script.timeoutMs / 1000}
                          onChange={(event) =>
                            updateScript({
                              timeoutMs: Math.round(
                                Number(event.target.value) * 1000,
                              ),
                            })
                          }
                        />
                      </label>
                    </>
                  )}
                  {workflow && (
                    <>
                      <AssetMetadata
                        value={workflow}
                        onChange={updateWorkflow}
                      />
                      <label>
                        {t("工作流说明")}
                        <textarea
                          rows={5}
                          value={workflow.instructions}
                          maxLength={65536}
                          onChange={(event) =>
                            updateWorkflow({ instructions: event.target.value })
                          }
                        />
                      </label>
                      <p className="muted">
                        {t(
                          "Agent 按顺序执行脚本，确认前一步成功后继续。失败或结果未确认时停止。",
                        )}
                      </p>
                      {workflow.steps.map((step, index) => (
                        <div className="project-workflow-step" key={index}>
                          <label>
                            {t("步骤 {0}", index + 1)}
                            <select
                              value={step}
                              onChange={(event) =>
                                updateWorkflow({
                                  steps: workflow.steps.map(
                                    (script, stepIndex) =>
                                      stepIndex === index
                                        ? event.target.value
                                        : script,
                                  ),
                                })
                              }
                            >
                              {draft.scripts.map((script) => (
                                <option key={script.id} value={script.id}>
                                  {script.name || script.id}
                                </option>
                              ))}
                            </select>
                          </label>
                          <button
                            className="icon-button"
                            aria-label={t("移除步骤 {0}", index + 1)}
                            onClick={() =>
                              updateWorkflow({
                                steps: workflow.steps.filter(
                                  (_, stepIndex) => stepIndex !== index,
                                ),
                              })
                            }
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      ))}
                      <button
                        disabled={
                          workflow.steps.length >= 32 || !draft.scripts.length
                        }
                        onClick={() =>
                          updateWorkflow({
                            steps: [...workflow.steps, draft.scripts[0].id],
                          })
                        }
                      >
                        <Plus size={15} />
                        {t("添加步骤")}
                      </button>
                    </>
                  )}
                  {!!items.length && (
                    <button
                      className="danger"
                      disabled={referencedScript}
                      title={
                        referencedScript
                          ? t("先移除工作流中对此脚本的引用。")
                          : undefined
                      }
                      onClick={() => {
                        if (section === "skills")
                          setDraft({
                            ...draft,
                            skills: draft.skills.filter(
                              (_, index) => index !== selected,
                            ),
                          });
                        if (section === "scripts")
                          setDraft({
                            ...draft,
                            scripts: draft.scripts.filter(
                              (_, index) => index !== selected,
                            ),
                          });
                        if (section === "workflows")
                          setDraft({
                            ...draft,
                            workflows: draft.workflows.filter(
                              (_, index) => index !== selected,
                            ),
                          });
                        setSelected(Math.max(0, selected - 1));
                      }}
                    >
                      <Trash2 size={15} />
                      {t("删除此条目")}
                    </button>
                  )}
                </div>
              </div>
            )}
            {section === "history" && (
              <div className="project-harness-history">
                <aside>
                  {history.items.map((item) => (
                    <button
                      key={item.revision}
                      aria-pressed={preview?.revision === item.revision}
                      onClick={() => void inspectRevision(item.revision)}
                    >
                      <strong>{t("版本 v{0}", item.revision)}</strong>
                      <small>{new Date(item.createdAt).toLocaleString()}</small>
                      <code>{item.version.slice(0, 12)}</code>
                    </button>
                  ))}
                  {!history.items.length && (
                    <p className="muted">{t("尚未保存版本")}</p>
                  )}
                  {!!history.nextBefore && (
                    <button onClick={() => void moreHistory()}>
                      {t("读取更多版本")}
                    </button>
                  )}
                </aside>
                {preview && (
                  <div className="project-harness-editor">
                    <p className="muted">
                      {t("历史版本保持原内容，采用后保存为新的版本。")}
                    </p>
                    <pre>{JSON.stringify(preview.configuration, null, 2)}</pre>
                    <button
                      onClick={() => {
                        setDraft(preview.configuration);
                        setSection("instructions");
                        setStatus(
                          t(
                            "已将 v{0} 载入草稿，保存后生成新版本。",
                            preview.revision,
                          ),
                        );
                      }}
                    >
                      {t("用此版本替换草稿")}
                    </button>
                  </div>
                )}
              </div>
            )}
          </fieldset>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          {status && <p role="status">{status}</p>}
          <div className="project-dialog-footer">
            <button disabled={busy} onClick={() => void refresh()}>
              {dirty ? t("放弃草稿并刷新") : t("刷新配置")}
            </button>
            <Dialog.Close disabled={saving}>{t("关闭")}</Dialog.Close>
            <button
              className="primary"
              disabled={busy || !base || !dirty}
              onClick={() => void save()}
            >
              {t("保存新版本")}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
