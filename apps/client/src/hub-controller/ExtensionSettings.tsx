import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import {
  commandID,
  CommandOutcomeError,
  type Device,
  type ExtensionRequest,
  type ExtensionMutation,
  type MewDexClient,
} from "@mewdex/sdk";
import { t } from "../localization";
import { ProjectHarnessDialog } from "./ProjectHarnessDialog";
import "../extensions.css";

export type ExtensionSection = "skills" | "memory" | "hooks" | "plugins";

const titles = {
  skills: "Skills",
  memory: "Memory",
  hooks: "Hooks",
  plugins: "Plugins",
};
const hookEvents = [
  "SessionStart",
  "SessionEnd",
  "UserPromptSubmit",
  "PreToolUse",
  "PostToolUse",
  "PermissionRequest",
  "PreCompact",
  "PostCompact",
  "SubagentStart",
  "SubagentStop",
  "Stop",
  "Interrupt",
];

export function ExtensionSettings({
  client,
  devices,
  section,
  admin,
  initialProjectId,
}: {
  client: MewDexClient;
  devices: Device[];
  section: ExtensionSection;
  admin: boolean;
  initialProjectId?: string;
}) {
  const cache = useQueryClient();
  const hub = devices.find((device) => device.roles.includes("coordinator"));
  const projects = hub?.projects.filter((project) => !project.removed) ?? [];
  const [projectId, setProjectId] = useState(initialProjectId ?? "");
  const project = projects.find((value) => value.id === projectId);
  const [editor, setEditor] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState("");
  const [event, setEvent] = useState("UserPromptSubmit");
  const [command, setCommand] = useState("");
  const [matcher, setMatcher] = useState("");
  const [timeout, setTimeoutSeconds] = useState(60);
  const [source, setSource] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const pendingKey = ["extension-pending", client, hub?.id];
  const pendingState = useQuery({
    queryKey: pendingKey,
    queryFn: async () => "",
    enabled: false,
    initialData: "",
    gcTime: Infinity,
  });
  const pending = pendingState.data;
  const supported = !!hub?.capabilities?.includes("extension-management");
  const catalog = useQuery({
    queryKey: ["extensions", client, hub?.id, section, project?.id ?? ""],
    queryFn: () => client.extensions(hub!.id, section, project?.id),
    enabled: admin && supported && !!hub?.online,
    staleTime: 30000,
    retry: false,
  });
  const configuration = useQuery({
    queryKey: ["extension-project", client, project?.id],
    queryFn: ({ signal }) => client.projectHarness(project!.id, 0, signal),
    enabled: admin && !!project && ["skills", "memory"].includes(section),
    staleTime: 30000,
  });

  function invalidate() {
    void cache.invalidateQueries({ queryKey: ["extensions", client, hub?.id] });
    void cache.invalidateQueries({ queryKey: ["composer-skills"] });
  }

  function completed(result?: ExtensionMutation) {
    setStatus(
      result?.appsNeedingAuth?.length
        ? t(
            "插件已安装，应用 {0} 仍需授权。",
            result.appsNeedingAuth.join("、"),
          )
        : result?.status && result.status !== "ok"
          ? t("配置已保存，运行时返回状态 {0}。请核对生效设置。", result.status)
          : t("扩展设置已保存，新对话将使用最新配置。"),
    );
    invalidate();
  }

  async function mutate(
    action: string,
    changes: Partial<ExtensionRequest> = {},
  ) {
    if (!hub || !catalog.data || busy || pending) return false;
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const result = await client.updateExtension(
        hub.id,
        {
          section,
          action,
          expectedVersion: catalog.data.configVersion,
          enabled: false,
          generateMemory: catalog.data.generateMemory,
          useMemory: catalog.data.useMemory,
          ...changes,
        },
        project?.id,
        commandID(),
      );
      completed(result);
      return true;
    } catch (cause) {
      if (
        cause instanceof CommandOutcomeError &&
        ["accepted", "unknown"].includes(cause.state)
      ) {
        cache.setQueryData(pendingKey, cause.commandId);
      }
      setError((cause as Error).message);
      invalidate();
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function reconcile() {
    if (!pending || busy) return;
    setBusy(true);
    try {
      const receipt = await client.result(pending);
      if (receipt.state === "completed" || receipt.state === "failed") {
        cache.setQueryData(pendingKey, "");
        if (receipt.state === "completed") {
          setError("");
          completed(receipt.data as ExtensionMutation);
        } else {
          setError(receipt.error ?? t("扩展操作失败"));
          invalidate();
        }
      } else setError(t("操作结果仍待确认，请稍后再次查询原回执。"));
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  if (!admin) return <p role="status">{t("只有管理员可以管理 Hub 扩展。")}</p>;

  return (
    <div className="extension-settings">
      <div className="extension-heading">
        <h3>{titles[section]}</h3>
        <button
          className="icon-button"
          aria-label={t("刷新扩展设置")}
          onClick={() => {
            void catalog.refetch();
            if (project) void configuration.refetch();
          }}
          disabled={!supported || busy}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      <p className="muted">
        {t(
          "原生技能、自动记忆、Hook 和插件运行于 Hub。项目配置由 Hub 保存并同步到各输入端。",
        )}
      </p>
      <p className="muted">
        {t("原生扩展使用 Hub 共享配置，项目范围用于发现该项目的可用条目。")}
      </p>
      <label>
        {t("扩展项目范围")}
        <select
          aria-label={t("扩展项目范围")}
          value={project?.id ?? ""}
          disabled={busy || !!pending}
          onChange={(change) => {
            setProjectId(change.target.value);
            setAdding(false);
            setError("");
            setStatus("");
          }}
        >
          <option value="">{t("Hub 全局扩展")}</option>
          {projects.map((value) => (
            <option key={value.id} value={value.id}>
              {value.name}
            </option>
          ))}
        </select>
      </label>
      {!supported && (
        <p role="status">
          {t("更新 Hub 后可管理原生 Skills、Memory、Hooks 和 Plugins。")}
        </p>
      )}
      {supported && !hub?.online && <p role="status">{t("Hub 已离线")}</p>}
      {catalog.isPending && supported && hub?.online && (
        <p role="status">{t("正在读取扩展设置…")}</p>
      )}
      {(error || catalog.error || configuration.error) && (
        <p className="error" role="alert">
          {error || catalog.error?.message || configuration.error?.message}
        </p>
      )}
      {status && <p role="status">{status}</p>}
      {pending && (
        <div className="extension-card">
          <p>{t("扩展操作结果待确认，先查询原回执再继续修改。")}</p>
          <code>{pending}</code>
          <button disabled={busy} onClick={() => void reconcile()}>
            {t("查询操作结果")}
          </button>
        </div>
      )}
      {catalog.data?.warnings.map((warning, index) => (
        <p className="error" key={index}>
          {warning}
        </p>
      ))}

      {["skills", "memory"].includes(section) && (
        <section className="network-section">
          <h3>{section === "skills" ? t("项目 Skills") : t("项目记忆")}</h3>
          {project ? (
            <>
              <p>{project.name}</p>
              {section === "skills" ? (
                <p className="muted">
                  {t(
                    "已配置 {0} 个项目 Skill，可在输入框的 / 菜单调用。",
                    configuration.data?.configuration.skills.length ?? 0,
                  )}
                </p>
              ) : (
                <>
                  <p className="muted">
                    {configuration.data?.configuration.memory?.enabled
                      ? t("项目记忆已启用")
                      : t("项目记忆已关闭")}
                  </p>
                  <pre className="extension-preview">
                    {configuration.data?.configuration.memory?.content ||
                      t("尚未保存项目记忆。")}
                  </pre>
                </>
              )}
              <button
                className="primary"
                disabled={
                  busy ||
                  !!pending ||
                  !configuration.data ||
                  (section === "memory" && !supported)
                }
                onClick={() => setEditor(true)}
              >
                {section === "skills"
                  ? t("管理项目 Skills")
                  : t("编辑项目记忆")}
              </button>
            </>
          ) : (
            <p className="muted">{t("选择项目以管理其 Skills 和长期记忆。")}</p>
          )}
        </section>
      )}

      {catalog.data && (
        <fieldset
          className="extension-controls"
          disabled={busy || !!pending || !hub?.online}
        >
          {section === "skills" && (
            <>
              <div className="extension-heading">
                <h3>{t("Hub 原生 Skills")}</h3>
                <button onClick={() => setAdding(!adding)}>
                  <Plus size={14} />
                  {t("新建 Skill")}
                </button>
              </div>
              {adding && (
                <div className="extension-form extension-card">
                  <label>
                    {t("Skill 编号")}
                    <input
                      value={name}
                      onChange={(change) => setName(change.target.value)}
                      maxLength={64}
                      placeholder="my-skill"
                    />
                  </label>
                  <label>
                    {t("Skill 说明")}
                    <textarea
                      value={description}
                      onChange={(change) => setDescription(change.target.value)}
                      rows={2}
                      maxLength={4096}
                    />
                  </label>
                  <label>
                    {t("Skill 指令正文")}
                    <textarea
                      value={instructions}
                      onChange={(change) =>
                        setInstructions(change.target.value)
                      }
                      rows={6}
                      maxLength={65536}
                    />
                  </label>
                  <button
                    className="primary"
                    disabled={
                      !name.trim() ||
                      !description.trim() ||
                      !instructions.trim()
                    }
                    onClick={() =>
                      void mutate("skill.create", {
                        name,
                        description,
                        instructions,
                      }).then((saved) => {
                        if (saved) {
                          setAdding(false);
                          setName("");
                          setDescription("");
                          setInstructions("");
                        }
                      })
                    }
                  >
                    {t("保存 Skill")}
                  </button>
                </div>
              )}
              {!catalog.data.skills.length && (
                <p className="muted">
                  {t("当前范围没有原生 Skill，可新建或通过插件安装。")}
                </p>
              )}
              {catalog.data.skills.map((skill) => (
                <article key={skill.path} className="extension-card">
                  <label className="checkbox">
                    <input
                      type="checkbox"
                      checked={skill.enabled}
                      onChange={(change) =>
                        void mutate("skill.set", {
                          path: skill.path,
                          enabled: change.target.checked,
                        })
                      }
                    />
                    <strong>{skill.name}</strong>
                  </label>
                  <p>{skill.description}</p>
                  <small>{skill.scope}</small>
                  <code>{skill.path}</code>
                </article>
              ))}
            </>
          )}

          {section === "memory" && (
            <section className="network-section extension-form">
              <h3>{t("Hub 自动记忆")}</h3>
              <p className="muted">
                {t(
                  "运行时可从对话生成长期记忆并在后续对话中使用，共享范围为当前 Hub。项目记忆由你在上方单独维护。",
                )}
              </p>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={catalog.data.memoryEnabled}
                  onChange={(change) =>
                    void mutate("memory.set", {
                      enabled: change.target.checked,
                    })
                  }
                />
                {t("启用运行时自动记忆")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={catalog.data.generateMemory}
                  onChange={(change) =>
                    void mutate("memory.set", {
                      enabled: catalog.data!.memoryEnabled,
                      generateMemory: change.target.checked,
                    })
                  }
                />
                {t("从新对话生成记忆")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={catalog.data.useMemory}
                  onChange={(change) =>
                    void mutate("memory.set", {
                      enabled: catalog.data!.memoryEnabled,
                      useMemory: change.target.checked,
                    })
                  }
                />
                {t("在后续对话使用运行时记忆")}
              </label>
            </section>
          )}

          {section === "hooks" && (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={catalog.data.hooksEnabled}
                  onChange={(change) =>
                    void mutate("feature.set", {
                      enabled: change.target.checked,
                    })
                  }
                />
                {t("启用 Hub Hooks")}
              </label>
              <p className="muted">
                {t(
                  "Hook 在 Hub 的运行时目录执行。先核对事件、命令和来源，再信任并启用对应内容。",
                )}
              </p>
              <button onClick={() => setAdding(!adding)}>
                <Plus size={14} />
                {t("新建 Hook")}
              </button>
              {adding && (
                <div className="extension-form extension-card">
                  <label>
                    {t("触发事件")}
                    <select
                      value={event}
                      onChange={(change) => setEvent(change.target.value)}
                    >
                      {hookEvents.map((value) => (
                        <option key={value}>{value}</option>
                      ))}
                    </select>
                  </label>
                  <label>
                    {t("Hook 命令")}
                    <textarea
                      value={command}
                      onChange={(change) => setCommand(change.target.value)}
                      rows={4}
                      maxLength={16384}
                    />
                  </label>
                  <label>
                    {t("匹配条件（可选）")}
                    <input
                      value={matcher}
                      onChange={(change) => setMatcher(change.target.value)}
                      maxLength={4096}
                    />
                  </label>
                  <label>
                    {t("超时秒数")}
                    <input
                      type="number"
                      min={1}
                      max={600}
                      value={timeout}
                      onChange={(change) =>
                        setTimeoutSeconds(Number(change.target.value))
                      }
                    />
                  </label>
                  <button
                    className="primary"
                    disabled={!command.trim() || timeout < 1 || timeout > 600}
                    onClick={() =>
                      void mutate("hook.create", {
                        event,
                        command,
                        matcher,
                        timeoutSec: timeout,
                      }).then((saved) => {
                        if (saved) {
                          setAdding(false);
                          setCommand("");
                        }
                      })
                    }
                  >
                    {t("保存 Hook")}
                  </button>
                </div>
              )}
              {!catalog.data.hooks.length && (
                <p className="muted">{t("当前范围没有 Hook。")}</p>
              )}
              {catalog.data.hooks.map((hook) => (
                <article key={hook.key} className="extension-card">
                  <strong>{hook.eventName}</strong>
                  <small>
                    {hook.trustStatus} · {hook.source}
                  </small>
                  <code>{hook.command || hook.handlerType}</code>
                  {hook.matcher && <code>{hook.matcher}</code>}
                  <code>{hook.sourcePath}</code>
                  {!hook.isManaged && (
                    <button
                      onClick={() =>
                        void mutate("hook.set", {
                          key: hook.key,
                          hash: hook.currentHash,
                          enabled:
                            !hook.enabled ||
                            !["trusted", "managed"].includes(hook.trustStatus),
                        })
                      }
                    >
                      {hook.enabled && hook.trustStatus === "trusted"
                        ? t("禁用 Hook")
                        : t("信任并启用此 Hook")}
                    </button>
                  )}
                  {!hook.isManaged &&
                    hook.enabled &&
                    hook.trustStatus !== "trusted" && (
                      <button
                        onClick={() =>
                          void mutate("hook.set", {
                            key: hook.key,
                            hash: hook.currentHash,
                            enabled: false,
                          })
                        }
                      >
                        {t("禁用 Hook")}
                      </button>
                    )}
                </article>
              ))}
            </>
          )}

          {section === "plugins" && (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={catalog.data.pluginsEnabled}
                  onChange={(change) =>
                    void mutate("feature.set", {
                      enabled: change.target.checked,
                    })
                  }
                />
                {t("启用 Hub Plugins")}
              </label>
              <p className="muted">
                {t(
                  "管理兼容当前 Codex 运行时的插件。插件可提供 Skills、Hooks、MCP 工具和应用，来源及依赖由 Hub 运行时处理。",
                )}
              </p>
              <div className="extension-form extension-card">
                <label>
                  {t("添加插件来源")}
                  <input
                    value={source}
                    onChange={(change) => setSource(change.target.value)}
                    placeholder={t("HTTPS 仓库地址或 Hub 上的绝对路径")}
                  />
                </label>
                <button
                  disabled={!source.trim()}
                  onClick={() =>
                    void mutate("marketplace.add", { source }).then((saved) => {
                      if (saved) setSource("");
                    })
                  }
                >
                  {t("添加来源")}
                </button>
              </div>
              {!catalog.data.plugins.length && (
                <p className="muted">
                  {t("当前没有可用插件，请添加兼容的插件来源。")}
                </p>
              )}
              {catalog.data.plugins.map((plugin) => (
                <article key={plugin.id} className="extension-card">
                  <strong>
                    {plugin.name}
                    {plugin.version ? ` · ${plugin.version}` : ""}
                  </strong>
                  <p>{plugin.description}</p>
                  <small>
                    {plugin.marketplace} ·{" "}
                    {plugin.installed ? t("已安装") : t("未安装")}
                  </small>
                  <code>{plugin.source}</code>
                  <div className="extension-actions">
                    {plugin.installed ? (
                      <>
                        <button
                          onClick={() =>
                            void mutate("plugin.set", {
                              pluginId: plugin.id,
                              enabled: !plugin.enabled,
                            })
                          }
                        >
                          {plugin.enabled ? t("禁用插件") : t("启用插件")}
                        </button>
                        <button
                          disabled={
                            plugin.installPolicy === "INSTALLED_BY_DEFAULT"
                          }
                          onClick={() =>
                            void mutate("plugin.remove", {
                              pluginId: plugin.id,
                            })
                          }
                        >
                          {t("卸载插件")}
                        </button>
                      </>
                    ) : (
                      <button
                        disabled={
                          plugin.installPolicy === "NOT_AVAILABLE" ||
                          (!!plugin.availability &&
                            plugin.availability !== "AVAILABLE")
                        }
                        onClick={() =>
                          void mutate("plugin.install", {
                            pluginId: plugin.id,
                            name: plugin.name,
                            hash: plugin.fingerprint,
                            source: plugin.source,
                            marketplace: plugin.marketplace,
                            marketplacePath: plugin.marketplacePath,
                          })
                        }
                      >
                        {t("安装插件")}
                      </button>
                    )}
                  </div>
                </article>
              ))}
            </>
          )}
        </fieldset>
      )}
      {editor && project && (
        <ProjectHarnessDialog
          client={client}
          project={project}
          devices={devices}
          initialSection={section === "memory" ? "memory" : "skills"}
          onClose={() => {
            setEditor(false);
            void configuration.refetch();
          }}
        />
      )}
    </div>
  );
}
