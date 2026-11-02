import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Folder,
  FolderPlus,
  Globe,
  LoaderCircle,
  Monitor,
  X,
} from "lucide-react";
import {
  CommandOutcomeError,
  commandID,
  type Command,
  type Device,
  type MewDexClient,
  type Project,
} from "@mewdex/sdk";
import { chooseProject, deviceRequest } from "../localDevice";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { WorkspaceFolderPicker } from "./WorkspaceFolderPicker";

export function WorkspaceManagerDialog({
  client,
  devices,
  initialDeviceId,
  initialProject,
  initialCategory,
  localAvailable,
  centralProjects = false,
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
  const [deviceId, setDeviceId] = useState(initialDeviceId);
  const [name, setName] = useState(initialProject?.name ?? "");
  const [path, setPath] = useState(initialProject?.path ?? "");
  const [category, setCategory] = useState(
    initialCategory ?? initialProject?.category ?? "",
  );
  const [picker, setPicker] = useState(false);
  const [manual, setManual] = useState(false);
  const [phase, setPhase] = useState<"" | "choosing" | "updating" | "saving">(
    "",
  );
  const [unconfirmed, setUnconfirmed] = useState(false);
  const [error, setError] = useState("");
  const focus = useDialogReturnFocus();
  const nameInput = useRef<HTMLInputElement>(null);
  const cache = useQueryClient();
  const local = useQuery({
    queryKey: ["workspace-local-device"],
    queryFn: async () =>
      (await deviceRequest({ operation: "status" })).snapshot,
    enabled: localAvailable,
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  });
  const device = devices.find((item) => item.id === deviceId);
  const isLocal = deviceId === local.data?.deviceId;
  const supported = !!device?.capabilities?.includes("workspace-management");
  const busy = !!phase;
  const editing = !!initialProject;
  const categories = [
    ...new Set(
      devices.flatMap((item) =>
        item.projects
          .map((project) => project.category)
          .filter((value): value is string => !!value),
      ),
    ),
  ].sort();
  function selectFolder(value: string) {
    setPath(value);
    setError("");
    setPicker(false);
    if (!name.trim())
      setName(
        value
          .replace(/[\\/]+$/, "")
          .split(/[\\/]/)
          .at(-1) || "新项目",
      );
  }
  async function chooseFolder() {
    if (!device || busy) return;
    if (isLocal) {
      setPhase("choosing");
      setError("");
      try {
        const value = await chooseProject();
        if (value) selectFolder(value);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "无法选择文件夹");
      } finally {
        setPhase("");
      }
    } else setPicker(true);
  }
  async function save() {
    if (!device || busy || unconfirmed || !name.trim() || !path.trim()) return;
    setError("");
    const command = {
      id: commandID(),
      deviceId,
      projectId: initialProject?.id,
      kind: editing ? "workspace.update" : "workspace.create",
      workspace: {
        path: path.trim(),
        name: name.trim(),
        category: category.trim(),
      },
    };
    try {
      if (!supported && isLocal && !(editing && centralProjects)) {
        setPhase("updating");
        const status = await deviceRequest({ operation: "status" });
        await deviceRequest({
          operation: "service.update",
          expectedDeviceId: deviceId,
          expectedInstanceId: status.snapshot.instanceId,
        });
        const refreshed = await client.devices();
        cache.setQueryData(["devices"], refreshed);
        void cache.invalidateQueries({ queryKey: ["account", deviceId] });
      }
      setPhase("saving");
      const project = editing
        ? await client.updateWorkspace(
            deviceId,
            initialProject.id,
            command.workspace,
            command.id,
          )
        : await client.createWorkspace(deviceId, command.workspace, command.id);
      cache.setQueryData<Device[]>(["devices"], (current) =>
        current?.map((item) =>
          item.id === deviceId
            ? {
                ...item,
                projects: [
                  ...item.projects.filter(
                    (existing) => existing.id !== project.id,
                  ),
                  project,
                ],
              }
            : item,
        ),
      );
      await cache.invalidateQueries({ queryKey: ["devices"] });
      if (!editing) onSelect(device, project);
      onClose();
    } catch (cause) {
      if (
        cause instanceof CommandOutcomeError &&
        ["accepted", "unknown"].includes(cause.state)
      ) {
        setUnconfirmed(true);
        onUnconfirmed(command);
      }
      setError(cause instanceof Error ? cause.message : "无法保存项目");
    } finally {
      setPhase("");
    }
  }
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
            <Dialog.Title>{editing ? "项目设置" : "创建项目"}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label="关闭项目设置"
              disabled={busy}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description className="sr-only">
            选择机器与已有文件夹，为工作区命名并选择分类。
          </Dialog.Description>
          <label className="project-name-field">
            <Folder size={21} />
            <input
              ref={nameInput}
              aria-label="项目名称"
              placeholder="项目名称"
              value={name}
              maxLength={100}
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <h3 className="project-source-heading">源文件夹</h3>
          <div className="project-source-card">
            <label className="project-source-device">
              {isLocal ? <Monitor size={16} /> : <Globe size={16} />}
              <select
                aria-label="工作区所在机器"
                value={deviceId}
                disabled={busy || editing}
                onChange={(event) => {
                  setDeviceId(event.target.value);
                  setPath("");
                  setError("");
                }}
              >
                {devices
                  .filter((item) => item.roles.includes("executor"))
                  .map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.id === local.data?.deviceId
                        ? `此电脑 · ${item.name}`
                        : item.name}
                      {item.online ? "" : " · 离线"}
                    </option>
                  ))}
              </select>
            </label>
            {path && !manual ? (
              <div className="project-chosen-folder">
                <Folder size={21} />
                <span title={path}>{path}</span>
                {!editing && (
                  <button
                    disabled={busy || !device?.online}
                    onClick={() => void chooseFolder()}
                  >
                    更改
                  </button>
                )}
              </div>
            ) : (
              !manual && (
                <button
                  className="project-add-folder"
                  disabled={
                    busy ||
                    !device?.online ||
                    (localAvailable && local.isPending)
                  }
                  onClick={() => void chooseFolder()}
                >
                  <FolderPlus size={17} />
                  添加
                </button>
              )
            )}
            {manual && (
              <input
                className="project-manual-path"
                aria-label="源文件夹路径"
                placeholder={
                  device?.platform.startsWith("windows")
                    ? "例如 D:\\Projects\\MyProject"
                    : "例如 /home/me/projects/my-project"
                }
                value={path}
                disabled={busy || editing}
                onChange={(event) => setPath(event.target.value)}
              />
            )}
            {!editing && (
              <button
                className="project-path-toggle"
                disabled={busy}
                onClick={() => setManual((value) => !value)}
              >
                {manual ? "通过文件夹选择" : "输入文件夹路径"}
              </button>
            )}
          </div>
          <label className="project-category-field">
            分类
            <input
              aria-label="项目分类"
              list="project-category-options"
              value={category}
              maxLength={80}
              disabled={busy}
              onChange={(event) => setCategory(event.target.value)}
              placeholder="项目（默认），或输入分类名称"
            />
            <datalist id="project-category-options">
              {categories.map((item) => (
                <option key={item} value={item} />
              ))}
            </datalist>
          </label>
          {!supported && isLocal && !(editing && centralProjects) && (
            <p className="project-connection-note">
              创建时会更新本机服务并重新连接；已有任务需先完成。
            </p>
          )}
          {!supported &&
            device &&
            !isLocal &&
            !(editing && centralProjects) && (
              <p className="project-connection-note">
                此机器的 MewDex 版本较旧，需更新后才能添加新文件夹。
              </p>
            )}
          {!device?.online && !(editing && centralProjects) && (
            <p className="project-connection-note">
              所选机器未连接，连接后可创建项目。
            </p>
          )}
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="project-dialog-footer">
            <Dialog.Close disabled={busy}>取消</Dialog.Close>
            <button
              className="primary"
              disabled={
                busy ||
                unconfirmed ||
                (!device?.online && !(editing && centralProjects)) ||
                !name.trim() ||
                !path.trim()
              }
              onClick={() => void save()}
            >
              {busy && <LoaderCircle size={15} className="spinning" />}
              {phase === "updating"
                ? "正在更新并连接…"
                : phase === "saving"
                  ? "正在保存…"
                  : editing
                    ? "保存"
                    : "创建项目"}
            </button>
          </div>
          {picker && device && (
            <WorkspaceFolderPicker
              client={client}
              device={device}
              initialPath={path}
              onSelect={selectFolder}
              onClose={() => setPicker(false)}
            />
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
