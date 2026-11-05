import { getLanguage, t } from "../localization.ts";
import { useEffect, useMemo, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  ArrowUp,
  Copy,
  FileText,
  Folder,
  FolderOpen,
  LoaderCircle,
  RefreshCw,
  X,
} from "lucide-react";
import {
  CommandOutcomeError,
  commandID,
  type Command,
  type Device,
  type MewDexClient,
  type OutputFile,
  type Session,
} from "@mewdex/sdk";
import { deviceRequest, inputCapabilities } from "../localDevice";
import { formatBytes } from "../formatBytes";
import {
  parentProjectPath,
  projectFileLocation,
  projectFileTarget,
} from "../projectFileLinks";
import { AttachmentList, AttachmentPreview } from "./AttachmentList";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { ProjectFileMenu } from "./ProjectFileMenu";
import {
  defaultProjectOpener,
  setDefaultProjectOpener,
  projectOpeners,
  openLocalProjectFile,
  openReceivedFile,
} from "../projectFileActions";

export function ProjectFilesDialog({
  client,
  session,
  device,
  initialFile,
  initialError,
  localAvailable,
  onClose,
  onUnconfirmed,
}: {
  client: MewDexClient;
  session: Session;
  device?: Device;
  initialFile?: string;
  initialError?: string;
  localAvailable: boolean;
  onClose: () => void;
  onUnconfirmed: (command: Omit<Command, "sourceId" | "createdAt">) => void;
}) {
  const returnFocus = useDialogReturnFocus();
  const project = device?.projects.find(
    (item) => item.id === session.projectId,
  );
  const initialLocation = projectFileLocation(initialFile);
  const initialTarget = initialLocation
    ? projectFileTarget(initialLocation.path, project)
    : undefined;
  const [directoryIndex, setDirectoryIndex] = useState(
    initialTarget?.directory ?? 0,
  );
  const projectRoot =
    project?.directories?.[directoryIndex]?.path ?? project?.path ?? "";
  const initial = initialTarget?.path;
  const [tab, setTab] = useState<"project" | "outputs">("project");
  const [directory, setDirectory] = useState(
    initial ? parentProjectPath(initial) : ".",
  );
  const [address, setAddress] = useState(directory);
  const [selected, setSelected] = useState(initial ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(initialError ?? "");
  const [openedCopy, setOpenedCopy] = useState("");
  const [defaultOpener, setDefaultOpener] = useState(() =>
    defaultProjectOpener(session.deviceId, session.projectId),
  );
  const openers = useQuery({
    queryKey: ["project-openers"],
    queryFn: projectOpeners,
    staleTime: 60000,
    refetchOnWindowFocus: false,
  });
  const [snapshot, setSnapshot] = useState<OutputFile | null>(null);
  const [snapshotLine, setSnapshotLine] = useState<number>();
  const [previewSnapshot, setPreviewSnapshot] = useState(false);
  const [pending, setPending] = useState<{
    id: string;
    path: string;
    line?: number;
  } | null>(null);
  const cache = useQueryClient();
  const capable = !!device?.capabilities?.includes("project-files");
  const online = capable && !!device?.online;
  const files = useInfiniteQuery({
    queryKey: ["project-files", session.id, directoryIndex, directory],
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      client.projectFiles(
        session,
        directory,
        pageParam,
        project?.directories ? directoryIndex : undefined,
      ),
    getNextPageParam: (page) => (page.hasMore ? page.next : undefined),
    enabled: online && tab === "project",
    retry: false,
    refetchOnWindowFocus: false,
  });
  const outputs = useQuery({
    queryKey: ["outputs", session.id],
    queryFn: ({ signal }) => client.outputs(session.id, signal),
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: pending ? 3000 : false,
  });
  const local = useQuery({
    queryKey: ["file-local-device"],
    queryFn: async () => {
      if (!(await inputCapabilities()).projectDirectory) return null;
      return (await deviceRequest({ operation: "status" })).snapshot;
    },
    enabled: localAvailable,
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  });
  const entries = useMemo(
    () =>
      (files.data?.pages.flatMap((page) => page.entries) ?? []).sort(
        (a, b) =>
          Number(b.directory) - Number(a.directory) ||
          a.name.localeCompare(b.name),
      ),
    [files.data],
  );
  const current = files.data?.pages[0]?.path ?? directory;
  const originalOutput = outputs.data?.find(
    (output) => output.commandId === pending?.id,
  );
  useEffect(() => {
    if (originalOutput) {
      setSnapshot(originalOutput);
      setSnapshotLine(pending?.line);
    }
  }, [originalOutput, pending?.line]);
  useEffect(() => {
    setAddress(directory);
  }, [directory]);

  function browse(path: string) {
    setDirectory(path);
    setSelected("");
    setError("");
    setSnapshot(null);
  }
  const isLocal = local.data?.deviceId === session.deviceId;
  function chooseDefault(value: string) {
    try {
      setDefaultProjectOpener(session.deviceId, session.projectId, value);
      setDefaultOpener(value);
    } catch {
      setError(t("无法保存默认打开方式"));
    }
  }
  async function openFile(path: string, action = defaultOpener) {
    setSelected(path);
    setError("");
    if (isLocal) {
      try {
        await openLocalProjectFile(session, path, action);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : t("无法打开文件"));
      }
    } else await exportFile(path, "open", action);
  }
  async function exportFile(
    path = selected,
    mode: "preview" | "open" = "preview",
    action = defaultOpener,
  ) {
    if (busy || pending || !path) return;
    setSelected(path);
    const line = path === initial ? initialLocation?.line : undefined;
    const command = {
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "file.export",
      file: {
        path,
        directory: project?.directories ? directoryIndex : undefined,
      },
    };
    setBusy(true);
    setError("");
    setSnapshot(null);
    try {
      const value = await client.exportFile(
        session,
        path,
        command.id,
        project?.directories ? directoryIndex : undefined,
      );
      setSnapshot(value);
      setSnapshotLine(line);
      if (mode === "open") {
        const opened = await openReceivedFile(client, value, action);
        if (opened) setOpenedCopy(opened);
        else setPreviewSnapshot(true);
      } else setPreviewSnapshot(true);
      await cache.invalidateQueries({ queryKey: ["outputs", session.id] });
    } catch (cause) {
      if (
        cause instanceof CommandOutcomeError &&
        ["accepted", "unknown"].includes(cause.state)
      ) {
        setPending({ id: command.id, path, line });
        onUnconfirmed(command);
        void outputs.refetch();
      }
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }
  async function observe() {
    if (!pending || busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.result(pending.id);
      if (result.state === "completed") {
        setSnapshot(result.data as OutputFile);
        setSnapshotLine(pending.line);
        setPending(null);
      } else if (result.state === "failed") {
        setPending(null);
        setError(result.error || t("文件文件副本未完成"));
      } else setError(t("回传结果仍待确认，可以查看已有文件副本或稍后查询。"));
      await outputs.refetch();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog.Root
      modal={false}
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Content
          className="settings-dialog project-files-dialog project-files-panel"
          onInteractOutside={(event) => event.preventDefault()}
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("项目文件")}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭项目文件")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {device?.name ?? t("执行设备")} ·{" "}
            {project?.name ?? session.projectId}
          </Dialog.Description>
          {!!project?.directories?.length && (
            <label>
              {t("项目目录")}
              <select
                aria-label={t("浏览的项目目录")}
                value={directoryIndex}
                disabled={busy || !!pending}
                onChange={(event) => {
                  setDirectoryIndex(Number(event.target.value));
                  browse(".");
                }}
              >
                {project.directories.map((binding, index) => (
                  <option
                    key={`${binding.deviceId}:${binding.projectId}`}
                    value={index}
                  >
                    {binding.name} · {binding.path}
                  </option>
                ))}
              </select>
            </label>
          )}
          <div className="project-files-heading">
            <div className="project-file-tabs" aria-label={t("文件视图")}>
              <button
                aria-pressed={tab === "project"}
                onClick={() => setTab("project")}
              >
                {t("当前文件")}
              </button>
              <button
                aria-pressed={tab === "outputs"}
                onClick={() => {
                  setTab("outputs");
                  void outputs.refetch();
                }}
              >
                {t("已接收文件")}
              </button>
            </div>
            {local.data?.deviceId === session.deviceId && (
              <ProjectFileMenu
                directory
                local
                native={localAvailable}
                openers={openers.data ?? []}
                defaultOpener={defaultOpener}
                onDefault={chooseDefault}
                onOpen={(action) => void openFile(".", action)}
                onPreview={() => {}}
                onReveal={() => void openFile(".", "reveal")}
                onCopy={() =>
                  void navigator.clipboard.writeText(project?.path ?? "")
                }
              >
                <button
                  className="project-folder-open"
                  onClick={() => void openFile(".")}
                >
                  <FolderOpen size={15} />
                  {t(" 打开工作区")}
                </button>
              </ProjectFileMenu>
            )}
          </div>
          {error && (
            <div className="error" role="alert">
              {error}
            </div>
          )}
          {pending && (
            <div className="file-export-pending" role="status">
              <strong>{t("文件回传结果待确认")}</strong>
              <p>{pending.path}</p>
              <button disabled={busy} onClick={() => void observe()}>
                {t("查询原回执")}
              </button>
              <button
                onClick={() => {
                  setTab("outputs");
                  void outputs.refetch();
                }}
              >
                {t("查看已回传文件副本")}
              </button>
            </div>
          )}
          <div className="project-files-body">
            {tab === "project" ? (
              <>
                <form
                  className="project-path-bar"
                  onSubmit={(event) => {
                    event.preventDefault();
                    browse(address.trim() || ".");
                  }}
                >
                  <button
                    type="button"
                    className="icon-button"
                    disabled={current === "." || !online}
                    aria-label={t("返回上级目录")}
                    onClick={() => browse(parentProjectPath(current))}
                  >
                    <ArrowUp size={16} />
                  </button>
                  <input
                    aria-label={t("项目内目录")}
                    value={address}
                    onChange={(event) => setAddress(event.target.value)}
                    placeholder={t("项目内目录")}
                    spellCheck={false}
                  />
                  <button disabled={!online} type="submit">
                    {t("打开")}
                  </button>
                  <button
                    className="icon-button"
                    disabled={!online || files.isFetching}
                    type="button"
                    aria-label={t("刷新项目文件")}
                    onClick={() => void files.refetch()}
                  >
                    <RefreshCw size={15} />
                  </button>
                </form>
                {!capable && (
                  <p className="muted">
                    {t(
                      "此设备尚未提供文件查看，请更新执行端；已回传的文件副本仍可查看。",
                    )}
                  </p>
                )}
                {capable && !device?.online && (
                  <p className="muted">
                    {t(
                      "执行设备离线。可以查看已回传文件副本，设备在线后再读取当前文件。",
                    )}
                  </p>
                )}
                {files.error && (
                  <p className="error" role="alert">
                    {t("无法读取此目录：{0}", files.error.message)}
                  </p>
                )}
                {files.isPending && online && (
                  <p className="project-files-loading">
                    <LoaderCircle size={18} className="spinning" />
                    {t(" 正在读取文件…")}
                  </p>
                )}
                <div
                  className="project-file-list"
                  aria-label={t("当前项目文件")}
                >
                  {entries.map((file) => (
                    <ProjectFileMenu
                      key={file.path}
                      directory={file.directory}
                      local={isLocal}
                      native={localAvailable}
                      openers={openers.data ?? []}
                      defaultOpener={defaultOpener}
                      onDefault={chooseDefault}
                      onOpen={(action) => void openFile(file.path, action)}
                      onPreview={() => void exportFile(file.path)}
                      onReveal={() => void openFile(file.path, "reveal")}
                      onCopy={() =>
                        void navigator.clipboard
                          .writeText(
                            project
                              ? `${projectRoot.replace(/[\\/]+$/, "")}/${file.path}`
                              : file.path,
                          )
                          .catch(() => setError(t("无法复制路径")))
                      }
                    >
                      <button
                        key={file.path}
                        className={`project-file-row ${selected === file.path ? "selected" : ""}`}
                        onClick={() =>
                          file.directory
                            ? browse(file.path)
                            : void openFile(file.path)
                        }
                      >
                        {file.directory ? (
                          <Folder size={18} />
                        ) : (
                          <FileText size={18} />
                        )}
                        <strong>{file.name}</strong>
                        <small>
                          {file.directory
                            ? t("文件夹")
                            : formatBytes(file.size)}
                        </small>
                      </button>
                    </ProjectFileMenu>
                  ))}
                  {!entries.length && files.isSuccess && (
                    <p className="empty-list">
                      {t("此目录没有可查看的文件。")}
                    </p>
                  )}
                </div>
                {files.hasNextPage && (
                  <button
                    className="project-files-more"
                    disabled={files.isFetchingNextPage}
                    onClick={() => void files.fetchNextPage()}
                  >
                    {files.isFetchingNextPage
                      ? t("正在读取…")
                      : t("加载更多文件")}
                  </button>
                )}
                {selected && (
                  <div className="project-file-selection">
                    <div>
                      <strong>{selected.split(/[\\/]/).at(-1)}</strong>
                      <p>
                        {selected}
                        {selected === initial && initialLocation?.line
                          ? t(" · 第 {0} 行", initialLocation.line)
                          : ""}
                      </p>
                    </div>
                    <button
                      className="primary"
                      disabled={!online || busy || !!pending}
                      onClick={() => void exportFile()}
                    >
                      <Copy size={15} />{" "}
                      {busy ? t("正在读取…") : t("在 MewDex 中预览")}
                    </button>
                  </div>
                )}
                <p className="project-files-note">
                  {isLocal
                    ? t(
                        "左键使用默认应用打开，右键选择 IDE、打开方式或在应用内预览。",
                      )
                    : t(
                        "远程文件会下载为本机副本后打开，修改副本不会自动同步到远程设备。",
                      )}
                </p>
                {openedCopy && (
                  <p className="project-files-note" role="status">
                    {t("已打开本机副本：{0}", openedCopy)}
                  </p>
                )}
                {snapshot && (
                  <AttachmentList
                    client={client}
                    items={[
                      {
                        id: snapshot.attachment.id,
                        metadata: snapshot.attachment,
                        initialLine: snapshotLine,
                      },
                    ]}
                  />
                )}
              </>
            ) : (
              <div className="project-output-list">
                {outputs.isPending && (
                  <p className="muted">{t("正在读取文件副本…")}</p>
                )}
                {outputs.error && (
                  <p className="error">
                    {t("暂时无法读取文件副本。")}
                    <button onClick={() => void outputs.refetch()}>
                      {t("重试")}
                    </button>
                  </p>
                )}
                {outputs.data?.map((output) => (
                  <article key={output.id} className="project-output-item">
                    <p title={output.path}>
                      {output.path}
                      <time>
                        {new Date(output.createdAt).toLocaleString(
                          getLanguage(),
                        )}
                      </time>
                    </p>
                    <AttachmentList
                      client={client}
                      items={[
                        {
                          id: output.attachment.id,
                          metadata: output.attachment,
                        },
                      ]}
                    />
                  </article>
                ))}
                {outputs.data?.length === 0 && (
                  <p className="empty-list">
                    {t("还没有从这个对话回传文件。")}
                  </p>
                )}
                <p className="project-files-note">
                  {t(
                    "显示最近 200 份文件副本。执行设备离线后仍可从设备网络读取。",
                  )}
                </p>
              </div>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
      {snapshot && previewSnapshot && (
        <AttachmentPreview
          client={client}
          file={snapshot.attachment}
          initialLine={snapshotLine}
          onClose={() => setPreviewSnapshot(false)}
        />
      )}
    </Dialog.Root>
  );
}
