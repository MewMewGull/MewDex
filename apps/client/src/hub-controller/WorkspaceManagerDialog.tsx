import { t } from "../localization.ts";
import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useQueryClient } from "@tanstack/react-query";
import {
  Folder,
  FolderPlus,
  LoaderCircle,
  Plus,
  Trash2,
  X,
} from "lucide-react";
import {
  CommandOutcomeError,
  commandID,
  type Command,
  type Device,
  type MewDexClient,
  type Project,
  type ProjectDirectory,
} from "@mewdex/sdk";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { WorkspaceFolderPicker } from "../components/WorkspaceFolderPicker";
import { ProjectHarnessDialog } from "./ProjectHarnessDialog";

interface DirectoryDraft {
  key: string;
  deviceId: string;
  projectId: string;
  path: string;
  name: string;
}

export function WorkspaceManagerDialog({
  client,
  devices,
  initialDeviceId,
  initialProject,
  initialCategory,
  onClose,
  onSelect,
  onUnconfirmed,
}: {
  client: MewDexClient;
  devices: Device[];
  initialDeviceId: string;
  initialProject?: Project;
  initialCategory?: string;
  localAvailable: boolean;
  centralProjects?: boolean;
  onClose: () => void;
  onSelect: (device: Device, project: Project) => void;
  onUnconfirmed: (command: Omit<Command, "sourceId" | "createdAt">) => void;
}) {
  const hub = devices.find((device) => device.roles.includes("coordinator"));
  const existing = hub?.projects.find(
    (project) => project.id === initialProject?.id,
  );
  const nodes = devices.filter(
    (device) =>
      device.roles.includes("executor") &&
      !device.roles.includes("coordinator"),
  );
  const [id] = useState(() => existing?.id ?? `hp_${commandID()}`);
  const [name, setName] = useState(
    existing?.name ?? initialProject?.name ?? "",
  );
  const [category, setCategory] = useState(
    initialCategory ?? existing?.category ?? "",
  );
  const [directories, setDirectories] = useState<DirectoryDraft[]>(() =>
    (existing?.directories ?? []).map((directory) => ({
      ...directory,
      key: commandID(),
    })),
  );
  const [picker, setPicker] = useState<string | null>(null);
  const [harnessOpen, setHarnessOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [unconfirmed, setUnconfirmed] = useState(false);
  const [revision, setRevision] = useState(existing?.revision ?? 0);
  const focus = useDialogReturnFocus();
  const nameInput = useRef<HTMLInputElement>(null);
  const cache = useQueryClient();
  const categories = [
    ...new Set(
      hub?.projects
        .map((project) => project.category)
        .filter((value): value is string => !!value),
    ),
  ].sort();
  function update(key: string, patch: Partial<DirectoryDraft>) {
    setDirectories((current) =>
      current.map((directory) =>
        directory.key === key ? { ...directory, ...patch } : directory,
      ),
    );
  }
  function addDirectory() {
    const deviceId = nodes.some((node) => node.id === initialDeviceId)
      ? initialDeviceId
      : (nodes[0]?.id ?? "");
    setDirectories((current) => [
      ...current,
      { key: commandID(), deviceId, projectId: "", path: "", name: "" },
    ]);
  }
  async function save() {
    if (!hub || busy || unconfirmed || !name.trim()) return;
    setBusy(true);
    setError("");
    try {
      const resolved: ProjectDirectory[] = [];
      for (const directory of directories) {
        let binding = devices
          .find((device) => device.id === directory.deviceId)
          ?.projects.find((project) => project.id === directory.projectId);
        if (!binding) {
          const command = {
            id: commandID(),
            deviceId: directory.deviceId,
            kind: "workspace.create",
            workspace: {
              path: directory.path.trim(),
              name: directory.name.trim() || name.trim(),
            },
          };
          try {
            binding = await client.createWorkspace(
              command.deviceId,
              command.workspace,
              command.id,
            );
          } catch (cause) {
            if (
              cause instanceof CommandOutcomeError &&
              ["accepted", "unknown"].includes(cause.state)
            ) {
              setUnconfirmed(true);
              onUnconfirmed(command);
            }
            throw cause;
          }
          update(directory.key, { projectId: binding.id, path: binding.path });
        }
        resolved.push({
          deviceId: directory.deviceId,
          projectId: binding.id,
          path: binding.path,
          name: directory.name.trim() || binding.name,
        });
      }
      const project = await client.saveHubProject(id, {
        name: name.trim(),
        category: category.trim(),
        directories: resolved,
        expectedRevision: revision,
      });
      setRevision(project.revision ?? 0);
      await cache.invalidateQueries({ queryKey: ["devices"] });
      if (!existing) onSelect(hub, project);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("无法保存 Hub 项目"));
    } finally {
      setBusy(false);
    }
  }
  const picked = directories.find((directory) => directory.key === picker);
  const pickedDevice = devices.find((device) => device.id === picked?.deviceId);
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog project-create-dialog"
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (busy) event.preventDefault();
          }}
          onOpenAutoFocus={(event) => {
            focus.capture(event);
            event.preventDefault();
            nameInput.current?.focus();
          }}
          onCloseAutoFocus={focus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>
              {existing ? t("项目设置") : t("创建 Hub 项目")}
            </Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭项目设置")}
              disabled={busy}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t(
              "Hub 保存项目名称和目录组。同一项目可以包含多台机器上的多个目录。",
            )}
          </Dialog.Description>
          <label className="project-name-field">
            <Folder size={21} />
            <input
              ref={nameInput}
              aria-label={t("项目名称")}
              placeholder={t("项目名称")}
              value={name}
              maxLength={100}
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <div className="project-directories-heading">
            <h3>{t("项目目录组")}</h3>
            <button disabled={busy || !nodes.length} onClick={addDirectory}>
              <Plus size={15} />
              {t("添加目录")}
            </button>
          </div>
          {!directories.length && (
            <p className="muted">
              {t("添加工程、引擎源码、打包环境或服务器配置所在的目录。")}
            </p>
          )}
          <div className="project-directories">
            {directories.map((directory) => {
              const device = devices.find(
                (item) => item.id === directory.deviceId,
              );
              return (
                <div className="project-source-card" key={directory.key}>
                  <div className="project-directory-controls">
                    <select
                      aria-label={t("目录所属机器")}
                      value={directory.deviceId}
                      disabled={busy}
                      onChange={(event) =>
                        update(directory.key, {
                          deviceId: event.target.value,
                          projectId: "",
                          path: "",
                        })
                      }
                    >
                      {nodes.map((node) => (
                        <option key={node.id} value={node.id}>
                          {node.name}
                          {node.online ? "" : t(" · 离线")}
                        </option>
                      ))}
                    </select>
                    <input
                      aria-label={t("目录用途")}
                      placeholder={t("目录用途")}
                      value={directory.name}
                      maxLength={100}
                      disabled={busy}
                      onChange={(event) =>
                        update(directory.key, { name: event.target.value })
                      }
                    />
                    <button
                      className="icon-button"
                      aria-label={t("从项目组移除此目录")}
                      disabled={busy}
                      onClick={() =>
                        setDirectories((current) =>
                          current.filter((item) => item.key !== directory.key),
                        )
                      }
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                  <select
                    aria-label={t("已绑定的目录")}
                    value={directory.projectId}
                    title={directory.path}
                    disabled={busy}
                    onChange={(event) => {
                      const project = device?.projects.find(
                        (item) => item.id === event.target.value,
                      );
                      update(directory.key, {
                        projectId: event.target.value,
                        path: project?.path ?? "",
                      });
                    }}
                  >
                    <option value="">{t("添加新目录")}</option>
                    {device?.projects.map((project) => (
                      <option key={project.id} value={project.id}>
                        {project.name} · {project.path}
                      </option>
                    ))}
                  </select>
                  {!directory.projectId && (
                    <div className="project-directory-path">
                      <input
                        aria-label={t("执行端目录路径")}
                        placeholder={
                          device?.platform.startsWith("windows")
                            ? t("例如 D:\\Projects\\Sott")
                            : t("例如 /home/me/Sott")
                        }
                        value={directory.path}
                        disabled={busy || !!directory.projectId}
                        onChange={(event) =>
                          update(directory.key, { path: event.target.value })
                        }
                      />
                      <button
                        title={t("浏览执行端目录")}
                        aria-label={t("浏览执行端目录")}
                        disabled={
                          busy || !device?.online || !!directory.projectId
                        }
                        onClick={() => setPicker(directory.key)}
                      >
                        <FolderPlus size={17} />
                      </button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          <label className="project-category-field">
            {t("分类")}
            <input
              aria-label={t("项目分类")}
              list="project-category-options"
              value={category}
              maxLength={80}
              disabled={busy}
              onChange={(event) => setCategory(event.target.value)}
              placeholder={t("项目（默认），或输入分类名称")}
            />
            <datalist id="project-category-options">
              {categories.map((item) => (
                <option key={item} value={item} />
              ))}
            </datalist>
          </label>
          {existing && hub?.capabilities?.includes("project-harness-v1") && (
            <button disabled={busy} onClick={() => setHarnessOpen(true)}>
              {t("项目指令与自动化")}
            </button>
          )}
          {!hub && (
            <p className="error">{t("请更新 Hub 后使用跨机器项目。")}</p>
          )}
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="project-dialog-footer">
            <Dialog.Close disabled={busy}>{t("取消")}</Dialog.Close>
            <button
              className="primary"
              disabled={
                busy ||
                unconfirmed ||
                !hub ||
                !name.trim() ||
                directories.some(
                  (directory) => !directory.deviceId || !directory.path.trim(),
                )
              }
              onClick={() => void save()}
            >
              {busy && <LoaderCircle size={15} className="spinning" />}
              {busy ? t("正在保存…") : existing ? t("保存项目") : t("创建项目")}
            </button>
          </div>
          {picked && pickedDevice && (
            <WorkspaceFolderPicker
              client={client}
              device={pickedDevice}
              initialPath={picked.path}
              onSelect={(path) => {
                update(picked.key, { path });
                setPicker(null);
              }}
              onClose={() => setPicker(null)}
            />
          )}
          {harnessOpen && existing && (
            <ProjectHarnessDialog
              key={existing.id}
              client={client}
              project={existing}
              devices={devices}
              onClose={() => setHarnessOpen(false)}
            />
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
