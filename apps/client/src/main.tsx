import React, {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createRoot } from "react-dom/client";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  ArrowDown,
  Circle,
  Menu,
  MessageCircleQuestion,
  Monitor,
  Paperclip,
  Search,
  X,
  FolderOpen,
  PanelRight,
} from "lucide-react";
import {
  commandID,
  MewDexClient,
  type Command as DeviceCommand,
  type Device,
  type Project,
  type Session,
  type SessionPatch,
  type MessageMatch,
} from "@mewdex/sdk";
import type { Connection } from "./platform";
import { hostedHubURL } from "./platform";
import { useDesktopHub } from "./useDesktopHub";
import { attachInputViewport } from "./inputViewport";
import {
  sameSelection,
  selectionOf,
  useWorkspace,
  type WorkspaceSelection,
} from "./state";
import { samePolicy } from "./policyValues";
import { usePolicyEditor } from "./usePolicyEditor";
import { isSettingsShortcut } from "./inputKeys";
import { composerSubmission } from "./composerSubmission";
import { messagesFrom } from "./messages";
import { commandOutputsFrom, latestDiffFrom } from "./outputs";
import { NetworkSettings } from "./components/NetworkSettings";
import { useInputAlerts } from "./useInputAlerts";
import { LocalDeviceDialog } from "./components/LocalDeviceDialog";
import { ConnectScreen } from "./components/ConnectScreen";
import { Brand } from "./components/Brand";
import { InteractionPanel } from "./components/InteractionPanel";
import { PolicySettings } from "./components/PolicySettings";
import { QueueHistoryDialog, QueuePanel } from "./components/QueuePanel";
import { WorkspaceSidebar } from "./components/WorkspaceSidebar";
import { ConversationActions } from "./components/ConversationActions";
import { ConversationWelcome } from "./components/ConversationWelcome";
import { ConversationComposer } from "./components/ConversationComposer";
import { AssistantActivity } from "./components/AssistantActivity";
import { executionTarget } from "./executionTarget";
import {
  ArchivedSessionsDialog,
  RenameSessionDialog,
} from "./components/SessionManagement";
import { MessageList } from "./components/MessageList";
import { AttachmentList } from "./components/AttachmentList";
import { AttachmentUploadList } from "./components/AttachmentUploadList";
import { useAttachmentUploads } from "./useAttachmentUploads";
import { QuickSwitcher } from "./components/QuickSwitcher";
import { useComposerDrafts } from "./useComposerDrafts";
import { useComposerActivity } from "./useComposerActivity";
import { composerKey, draftDestination, draftEntries } from "./composerDrafts";
import { inputScope } from "./inputScope";
import { useWorkspaceView } from "./useWorkspaceView";
import { useAutosizeTextarea } from "./useAutosizeTextarea";
import { useComposerPosition } from "./useComposerPosition";
import { OutputResizeHandle } from "./components/OutputResizeHandle";
import { useNetwork } from "./useNetwork";
import { useNetworkActivity } from "./useNetworkActivity";
import { useModelAccount } from "./useModelAccount";
import { ModelConnectionNotice } from "./components/ModelConnectionNotice";
import { ActivityDialog } from "./components/ActivityDialog";
import { ProjectFilesDialog } from "./components/ProjectFilesDialog";
import { WorkspaceManagerDialog } from "./components/WorkspaceManagerDialog";
import { AccountMenu } from "./components/AccountMenu";
import { ComposerControls } from "./components/ComposerControls";
import { currentActivity } from "./activityPresentation";
import {
  defaultProjectOpener,
  isLocalProject,
  openLocalProjectFile,
  openReceivedFile,
} from "./projectFileActions";
import { projectFileLocation, relativeProjectPath } from "./projectFileLinks";
import { ConversationSearchDialog } from "./components/ConversationSearchDialog";
import { useConversationHistory } from "./useConversationHistory";
import { useOutputReading } from "./useOutputReading";
import { useAppearance } from "./useAppearance";
import { attachMobileSystemBars } from "./mobileSystemBars";
import { useTaskPlan } from "./useTaskPlan";
import { TaskPlanCard } from "./components/TaskPlanCard";
import {
  useConversationReads,
  useReadVisibleConversation,
} from "./useConversationReads";
import {
  LocalServiceReconnect,
  NetworkConnectionNotice,
} from "./components/ConnectionRecovery";
import { useInputConnection } from "./useInputConnection";
import { useCommandRecovery } from "./useCommandRecovery";
import { useSessionInteractions } from "./useSessionInteractions";
import { useInteractionResponses } from "./useInteractionResponses";
import { useInteractionFocus } from "./useInteractionFocus";
import {
  commandRecoveryView,
  isUnconfirmedCommand,
  type PendingCommand,
} from "./commandRecovery";
import {
  CommandRecoveryDialog,
  CommandRecoveryNotice,
} from "./components/CommandRecovery";
import "./style.css";
import "./theme-palette.css";
import "./appearance.css";
import "./desktop-workspace.css";

const queries = new QueryClient({ defaultOptions: { queries: { retry: 1 } } });
const OutputPanel = lazy(() =>
  import("./components/OutputPanel").then((module) => ({
    default: module.OutputPanel,
  })),
);

function App() {
  useAppearance();
  useEffect(attachMobileSystemBars, []);
  useEffect(attachInputViewport, []);
  useEffect(() => {
    const preventFileNavigation = (event: DragEvent) => {
      if (event.dataTransfer?.types.includes("Files")) event.preventDefault();
    };
    window.addEventListener("dragover", preventFileNavigation);
    window.addEventListener("drop", preventFileNavigation);
    return () => {
      window.removeEventListener("dragover", preventFileNavigation);
      window.removeEventListener("drop", preventFileNavigation);
    };
  }, []);
  const input = useInputConnection(() => {
    useWorkspace.getState().reset();
    queries.clear();
  });
  const { connection, loading } = input;
  const desktopHub = useDesktopHub(input.retry);
  const [localDeviceOpen, setLocalDeviceOpen] = useState(false);
  const localDeviceAvailable = desktopHub.available;
  const [localStartAvailable, setLocalStartAvailable] = useState(false);
  const [connectionTarget, setConnectionTarget] = useState<string>();
  const [connectingExisting, setConnectingExisting] = useState(false);
  useEffect(() => {
    if (desktopHub.setupRequired && !loading && !connection && !input.address)
      setLocalDeviceOpen(true);
  }, [desktopHub.setupRequired, loading, connection, input.address]);
  async function saveConnection(value: Connection) {
    await input.save(value);
    setConnectionTarget(undefined);
    setConnectingExisting(false);
  }
  const openLocalDevice = localDeviceAvailable
    ? () => setLocalDeviceOpen(true)
    : undefined;
  let screen: React.ReactNode;
  const defaultHubURL =
    connectionTarget ?? desktopHub.baseURL ?? hostedHubURL();
  if (loading || desktopHub.pending)
    screen = <div className="loading">正在打开工作台…</div>;
  else if (input.issue)
    screen = (
      <div className="connect-screen">
        <main className="connect-card">
          <Brand />
          <h1>
            {input.issue.kind === "authorization"
              ? "输入端授权已失效"
              : input.issue.kind === "storage"
                ? "暂时无法读取配对记录"
                : input.issue.kind === "configuration"
                  ? "连接地址需要检查"
                  : "正在等待设备网络"}
          </h1>
          <p>
            {input.issue.retrying
              ? "正在自动重试。网络恢复后将使用原配对打开工作台。"
              : "已有配对记录会保留，可以重试连接或查看详情。"}
          </p>
          {input.issue.kind === "network" &&
            input.address &&
            localDeviceAvailable && (
              <LocalServiceReconnect
                address={input.address}
                onRetry={input.retry}
                prominent
                onAvailable={setLocalStartAvailable}
              />
            )}
          <button
            className={
              localStartAvailable
                ? "reconnect-button"
                : "primary connect-button"
            }
            disabled={input.checking}
            onClick={input.retry}
          >
            {input.checking ? "正在连接…" : "立即重试"}
          </button>
          {openLocalDevice && (
            <button className="local-device-entry" onClick={openLocalDevice}>
              <Monitor size={16} /> 管理本机服务
            </button>
          )}
          <details className="connection-recovery-details">
            <summary>连接详情与重新配对</summary>
            {input.address && <p>{input.address}</p>}
            <p className="error">{input.issue.message}</p>
            <button
              className="reconnect-button"
              onClick={() => void input.disconnect()}
            >
              移除本机配对记录并重新连接
            </button>
          </details>
        </main>
      </div>
    );
  else
    screen = connection ? (
      <Workspace
        key={JSON.stringify([
          connection.baseURL,
          connection.credentials.identity.id,
        ])}
        connection={connection}
        onLocalDevice={openLocalDevice}
        onLogout={() => {
          void input.disconnect();
        }}
      />
    ) : (
      <ConnectScreen
        onConnect={saveConnection}
        onLocalDevice={openLocalDevice}
        initialURL={defaultHubURL}
        joining={connectingExisting || !!defaultHubURL}
        onJoiningChange={setConnectingExisting}
        startupError={desktopHub.error}
        onRetryStartup={desktopHub.retry}
      />
    );
  return (
    <>
      {screen}
      {localDeviceAvailable && (
        <LocalDeviceDialog
          open={localDeviceOpen}
          onOpenChange={setLocalDeviceOpen}
          onConnect={async (value) => {
            await saveConnection(value);
            desktopHub.connected(value.baseURL);
          }}
          onJoinNetwork={(url) => {
            if (
              connection?.baseURL.replace(/\/$/, "") === url.replace(/\/$/, "")
            ) {
              input.retry();
              setLocalDeviceOpen(false);
              return;
            }
            input.chooseNetwork();
            setConnectionTarget(url);
            setConnectingExisting(true);
            setLocalDeviceOpen(false);
          }}
        />
      )}
    </>
  );
}

function Workspace({
  connection,
  onLogout,
  onLocalDevice,
}: {
  connection: Connection;
  onLogout: () => void;
  onLocalDevice?: () => void;
}) {
  const client = useMemo(
    () =>
      new MewDexClient({
        baseURL: connection.baseURL,
        token: connection.credentials.token,
      }),
    [connection],
  );
  const cache = useQueryClient();
  const state = useWorkspace();
  const scope = inputScope(
    connection.baseURL,
    connection.credentials.identity.id,
  );
  const view = useWorkspaceView(scope);
  const conversationReads = useConversationReads(scope);
  const privateEpoch = useRef(0);
  const network = useNetwork(
    client,
    connection.credentials.identity,
    clearDrafts,
    view.restore,
    conversationReads,
  );
  const alerts = useInputAlerts(
    client,
    connection.baseURL,
    connection.credentials.identity.id,
    network.ready,
  );
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: () => client.devices(),
    refetchInterval: 10000,
    enabled: network.ready,
    staleTime: 2000,
  });
  const sessions = useQuery({
    queryKey: ["sessions"],
    queryFn: () => client.sessions(),
    enabled: network.ready,
    staleTime: 2000,
  });
  const identity = useQuery({
    queryKey: ["identity"],
    queryFn: () => client.identity(),
    initialData: connection.credentials.identity,
    staleTime: Infinity,
    enabled: network.ready,
  });
  const [sidebar, setSidebar] = useState(false);
  const navigationVisible = view.navigationVisible;
  const [switcher, setSwitcher] = useState(false);
  const [settings, setSettings] = useState(false);
  const [networkSettings, setNetworkSettings] = useState(false);
  const [modelSettingsDevice, setModelSettingsDevice] = useState<string>();
  const [executorSetup, setExecutorSetup] = useState(false);
  const [conversationOptions, setConversationOptions] = useState(false);
  const [conversationSearch, setConversationSearch] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [renameSession, setRenameSession] = useState<Session | null>(null);
  const renameOrigin = useRef<HTMLElement | null>(null);
  const [archiveLibrary, setArchiveLibrary] = useState(false);
  const [fileTarget, setFileTarget] = useState<{
    session: Session;
    file?: string;
    error?: string;
    epoch: number;
  } | null>(null);
  const [activityOpen, setActivityOpen] = useState(false);
  const [workspaceManager, setWorkspaceManager] = useState<{
    epoch: number;
    deviceId: string;
    project?: Project;
    category?: string;
  } | null>(null);
  const activity = useNetworkActivity(client, network.ready, activityOpen);
  const [sessionSaving, setSessionSaving] = useState(false);
  const [queueHistory, setQueueHistory] = useState(false);
  const [receiptHistory, setReceiptHistory] = useState(false);
  const [outputPanel, setOutputPanel] = useState<"terminal" | "diff" | null>(
    null,
  );
  const [outputCommand, setOutputCommand] = useState<{
    id: string;
    version: number;
    sessionId: string;
    contextKey: string;
  } | null>(null);
  const outputFocusVersion = useRef(0);
  const outputReading = useOutputReading();
  const [pendingCommands, setPendingCommands] = useState<PendingCommand[]>([]);
  const [dismissedCommands, setDismissedCommands] = useState(new Set<string>());
  const recovery = useCommandRecovery(
    client,
    connection.credentials.identity.id,
    state.deviceId,
    network.ready,
    pendingCommands,
    dismissedCommands,
    privateEpoch.current,
  );
  const recoveryView = commandRecoveryView(
    recovery.pending,
    state.deviceId,
    devices.data ?? [],
    sessions.data ?? [],
  );
  const controls = useRef(new Set<string>());
  const [droppingFiles, setDroppingFiles] = useState(false);
  const dragDepth = useRef(0);
  const composerInput = useRef<HTMLTextAreaElement>(null);
  const mainPanel = useRef<HTMLElement>(null);
  const activeDevice = devices.data?.find((d) => d.id === state.deviceId);
  const project = view.projectFor(activeDevice);
  const activeSession = sessions.data?.find((s) => s.id === state.sessionId);
  const target = executionTarget(
    activeDevice,
    activeSession?.projectId ?? project,
  );
  const modelStatusEnabled =
    network.ready &&
    network.connected &&
    identity.data.role === "admin" &&
    !!activeDevice?.online &&
    target.canExecute;
  const { account: modelAccount, pending: pendingModelLogin } = useModelAccount(
    client,
    state.deviceId,
    modelStatusEnabled,
  );
  const modelAccountMissing =
    modelStatusEnabled && modelAccount.data?.signedIn === false;
  const policyEditor = usePolicyEditor(
    composerKey(state.deviceId, state.sessionId, project),
    activeSession?.policy ?? state.policy,
    activeSession?.policyVersion ?? "",
    state,
  );
  const openFileReference = useCallback(
    (path: string, requestedAction?: string) => {
      if (!activeSession) return;
      const session = activeSession;
      const epoch = privateEpoch.current;
      if (requestedAction === "panel") {
        setFileTarget({ session, file: path, epoch });
        return;
      }
      const location = projectFileLocation(path);
      const root =
        activeDevice?.projects.find((item) => item.id === session.projectId)
          ?.path ?? "";
      const relative = location
        ? relativeProjectPath(location.path, root)
        : undefined;
      if (!relative) {
        setFileTarget({ session, file: path, epoch });
        return;
      }
      void (async () => {
        try {
          const action =
            requestedAction ||
            defaultProjectOpener(session.deviceId, session.projectId);
          if (await isLocalProject(session.deviceId))
            await openLocalProjectFile(session, relative, action);
          else {
            const command = {
              id: commandID(),
              deviceId: session.deviceId,
              sessionId: session.id,
              kind: "file.export",
              file: { path: relative },
            };
            try {
              const copy = await client.exportFile(
                session,
                relative,
                command.id,
              );
              await openReceivedFile(client, copy, action);
              if (epoch === privateEpoch.current)
                setFileTarget({ session, file: path, epoch });
            } catch (cause) {
              if (isUnconfirmedCommand(cause) && epoch === privateEpoch.current)
                setPendingCommands((items) => [
                  ...items,
                  { command, draft: "" },
                ]);
              throw cause;
            }
          }
        } catch (cause) {
          if (epoch === privateEpoch.current)
            setFileTarget({
              session,
              file: path,
              epoch,
              error:
                cause instanceof Error
                  ? cause.message
                  : "默认应用未能打开此文件，请右键选择打开方式。",
            });
        }
      })();
    },
    [activeSession?.id, activeDevice, client],
  );
  const interactions = useSessionInteractions(
    client,
    activeSession,
    network.ready,
    !!activeDevice?.online,
  );
  const blockingInteractions =
    interactions.data?.filter((request) => request.blocking).length ?? 0;
  const responses = useInteractionResponses(
    client,
    activeSession,
    interactions.data,
  );
  const waitingForReply =
    activeSession?.state === "running" && blockingInteractions > 0;
  const drafts = useComposerDrafts(
    scope,
    state.deviceId,
    state.sessionId,
    activeSession?.projectId ?? project,
  );
  const { text, setText, uploads, setUploads } = drafts;
  const composerActivity = useComposerActivity(drafts.key);
  const { busy, error, setError } = composerActivity;
  const attachmentUploads = useAttachmentUploads(
    client,
    drafts.key,
    drafts.appendUpload,
  );
  const uploading = attachmentUploads.pending > 0;
  const draftSessionIds = new Set([
    ...drafts.sessionIds,
    ...attachmentUploads.sessionIds,
  ]);
  const draftKeys = new Set([...drafts.keys, ...attachmentUploads.draftKeys]);
  const localDrafts = useMemo(
    () =>
      draftEntries(
        drafts.all,
        attachmentUploads.all,
        devices.data ?? [],
        sessions.data ?? [],
      ),
    [drafts.all, attachmentUploads.all, devices.data, sessions.data],
  );
  useAutosizeTextarea(composerInput, text, !network.unauthorized);
  const composerPosition = useComposerPosition(
    composerInput,
    drafts.key,
    !network.unauthorized,
  );
  const executionRetired =
    activeSession?.executionRecovery?.state === "retired";
  const canAttach = target.canExecute && !executionRetired;
  const attachmentUnavailable = executionRetired
    ? "请新建对话后添加附件。"
    : target.message;
  const canSubmit =
    target.canExecute &&
    !!activeDevice?.online &&
    network.ready &&
    network.connected &&
    !busy &&
    !attachmentUploads.blocked &&
    !recovery.blocked &&
    !modelAccountMissing &&
    !(modelStatusEnabled && modelAccount.isPending) &&
    !activeSession?.archived &&
    !executionRetired &&
    (!!text.trim() || uploads.length > 0);
  const submissionIntent = composerSubmission(activeSession);
  const canSend = canSubmit && !!submissionIntent.command;
  const canQueue = canSubmit && submissionIntent.queueVisible;
  const feed = useRef<HTMLDivElement>(null);
  const historyView = useConversationHistory(
    client,
    state.sessionId,
    network.ready,
    feed,
    state.events,
  );
  const openCommandOutput = useCallback(
    (id: string) => {
      setOutputCommand({
        id,
        version: ++outputFocusVersion.current,
        sessionId: state.sessionId,
        contextKey: historyView.contextKey,
      });
      setOutputPanel("terminal");
    },
    [state.sessionId, historyView.contextKey],
  );
  const history = historyView.page;
  const taskPlansAvailable = network.capabilities.includes("task-plans");
  const taskPlan = useTaskPlan(
    client,
    state.sessionId,
    network.ready && taskPlansAvailable,
    historyView.live,
    history.data?.after,
  );
  const messages = useMemo(
    () => messagesFrom(historyView.events, state.sessionId),
    [historyView.events, state.sessionId],
  );
  const commandOutputs = useMemo(
    () => commandOutputsFrom(historyView.events, state.sessionId),
    [historyView.events, state.sessionId],
  );
  const turnDiff = useMemo(
    () => latestDiffFrom(historyView.events, state.sessionId),
    [historyView.events, state.sessionId],
  );
  const interactionList = useRef<HTMLDivElement>(null);
  const readingEarlier = historyView.readingEarlier;
  const interactionFocus = useInteractionFocus({
    selection: state,
    live: historyView.live,
    ready:
      network.ready &&
      !historyView.restoring &&
      !history.isPending &&
      !history.isFetching &&
      !interactions.isPending &&
      !interactions.isFetching,
    area: interactionList,
    onLatest: () => historyView.latest(false),
  });
  useReadVisibleConversation(
    {
      deviceId: state.deviceId,
      sessionId: state.sessionId,
      events: state.events,
      enabled:
        network.connected &&
        historyView.live &&
        !historyView.restoring &&
        !readingEarlier &&
        !history.isFetching &&
        conversationReads.unread.has(state.sessionId),
      area: feed,
    },
    conversationReads.markRead,
  );
  function clearDrafts() {
    privateEpoch.current++;
    policyEditor.clear();
    historyView.clear();
    outputReading.clear();
    responses.clear();
    interactionFocus.clear();
    conversationReads.clear();
    alerts.clear();
    discardComposerDrafts();
    view.reset();
    setPendingCommands([]);
    setDismissedCommands(new Set());
    setSettings(false);
    setNetworkSettings(false);
    setModelSettingsDevice(undefined);
    setConversationOptions(false);
    setConversationSearch(false);
    setSearchTerm("");
    setRenameSession(null);
    setArchiveLibrary(false);
    setFileTarget(null);
    setActivityOpen(false);
    setQueueHistory(false);
    setReceiptHistory(false);
    setSwitcher(false);
    setOutputPanel(null);
    setOutputCommand(null);
    setDroppingFiles(false);
    dragDepth.current = 0;
    composerActivity.clear();
  }
  function disconnectInput() {
    policyEditor.clear();
    historyView.clear();
    outputReading.clear();
    interactionFocus.clear();
    responses.clear();
    composerActivity.clear();
    conversationReads.clear();
    alerts.clear();
    discardComposerDrafts();
    view.reset();
    onLogout();
  }
  function discardComposerDrafts() {
    composerPosition.clear();
    attachmentUploads.clear();
    drafts.reset();
  }
  function moveDraft(from: string, to: string, sessionId: string) {
    composerPosition.move(from, to);
    drafts.move(from, to);
    attachmentUploads.move(from, to, sessionId);
    composerActivity.move(from, to);
  }
  useEffect(() => {
    setConversationOptions(false);
    setConversationSearch(false);
    setSearchTerm("");
    setQueueHistory(false);
    setReceiptHistory(false);
  }, [state.sessionId]);
  useEffect(() => setReceiptHistory(false), [state.deviceId]);
  useEffect(() => {
    if (network.ready) view.rememberSelection(state.deviceId, state.sessionId);
  }, [network.ready, state.deviceId, state.sessionId]);
  useEffect(() => {
    if (
      activeSession &&
      (!state.policyDirty || samePolicy(state.policy, activeSession.policy)) &&
      state.policyBasis !== activeSession.policyVersion
    )
      state.loadPolicy(activeSession.policy, activeSession.policyVersion);
  }, [
    activeSession?.id,
    activeSession?.policyVersion,
    state.policyDirty,
    state.policy,
  ]);
  async function execute(
    command: Omit<DeviceCommand, "sourceId" | "createdAt">,
    draft?: { text: string; key: string; selection: WorkspaceSelection },
  ) {
    const epoch = privateEpoch.current;
    let result;
    try {
      result = await client.run(command);
    } catch (error) {
      if (isUnconfirmedCommand(error) && epoch === privateEpoch.current)
        setPendingCommands((old) =>
          old.some((pending) => pending.command.id === command.id)
            ? old
            : [
                ...old,
                {
                  command,
                  draft: draft?.text ?? "",
                  draftKey: draft?.key,
                  selection: draft?.selection,
                },
              ],
        );
      throw error;
    }
    await cache.invalidateQueries({ queryKey: ["sessions"] });
    if (command.kind === "queue.cancel" || command.kind === "enqueue")
      await cache.invalidateQueries({ queryKey: ["queue", command.sessionId] });
    return result;
  }
  function pendingDraftKey(pending: PendingCommand) {
    return (
      pending.draftKey ??
      composerKey(
        pending.command.deviceId,
        pending.command.sessionId ?? "",
        pending.command.projectId ?? "",
      )
    );
  }
  function completePendingDraft(pending: PendingCommand) {
    if (!pending.draftKey) return;
    drafts.complete(
      pending.draftKey,
      pending.draft,
      pending.command.input?.flatMap((input) =>
        input.attachmentId ? [input.attachmentId] : [],
      ) ?? [],
    );
  }
  function resolvePending(
    id: string,
    result: import("@mewdex/sdk").CommandResult,
  ) {
    if (recovery.epoch !== privateEpoch.current) return;
    const pendingCommand = recovery.pending.find(
      (pending) => pending.command.id === id,
    );
    if (!pendingCommand) return;
    const command = pendingCommand.command;
    if (command.kind === "respond") responses.resolve(id, result);
    let feedbackKey = pendingDraftKey(pendingCommand);
    if (result.state === "completed") {
      if (["start", "steer", "enqueue"].includes(command.kind))
        completePendingDraft(pendingCommand);
      if (
        command.kind === "create" &&
        result.session &&
        pendingCommand.draftKey
      ) {
        feedbackKey = composerKey(
          result.session.deviceId,
          result.session.id,
          result.session.projectId,
        );
        moveDraft(pendingCommand.draftKey, feedbackKey, result.session.id);
        if (
          pendingCommand.selection &&
          sameSelection(pendingCommand.selection, useWorkspace.getState())
        ) {
          state.selectSession(result.session.id);
          state.loadPolicy(result.session.policy, result.session.policyVersion);
        }
      }
      void cache.invalidateQueries({ queryKey: ["sessions"] });
      if (command.kind === "queue.cancel" || command.kind === "enqueue")
        void cache.invalidateQueries({
          queryKey: ["queue", command.sessionId],
        });
      composerActivity.report(feedbackKey, "");
    } else composerActivity.report(feedbackKey, result.error ?? "指令未完成");
    setPendingCommands((old) =>
      old.filter((pending) => pending.command.id !== id),
    );
    setDismissedCommands((old) => new Set(old).add(id));
    void cache.invalidateQueries({ queryKey: ["pending-commands"] });
  }
  function dismissPending(id: string) {
    if (recovery.epoch !== privateEpoch.current) return;
    const pendingCommand = pendingCommands.find(
      (pending) => pending.command.id === id,
    );
    if (
      pendingCommand &&
      ["start", "steer", "enqueue"].includes(pendingCommand.command.kind)
    ) {
      completePendingDraft(pendingCommand);
    }
    setPendingCommands((old) =>
      old.filter((pending) => pending.command.id !== id),
    );
    setDismissedCommands((old) => new Set(old).add(id));
  }
  async function send(kind?: "enqueue") {
    const commandKind = kind ?? submissionIntent.command;
    if (
      !commandKind ||
      !activeDevice ||
      !(kind === "enqueue" ? canQueue : canSend)
    )
      return;
    const submission = composerActivity.begin();
    if (!submission) return;
    const submittedText = text,
      submittedAttachments = uploads.map((upload) => upload.id),
      epoch = privateEpoch.current;
    let submittedSelection = selectionOf(useWorkspace.getState());
    let submittedDraftKey = drafts.key;
    let failure: string | undefined;
    try {
      let session = activeSession;
      if (!session) {
        const result = await execute(
          {
            id: commandID(),
            deviceId: activeDevice.id,
            projectId: project,
            kind: "create",
            title: text.trim().slice(0, 40) || uploads[0].name,
            policy: state.policy,
          },
          {
            text: submittedText,
            key: submittedDraftKey,
            selection: submittedSelection,
          },
        );
        if (epoch !== privateEpoch.current) return;
        session = result.session ?? undefined;
        if (!session) throw new Error("会话尚未创建");
        submittedDraftKey = composerKey(
          session.deviceId,
          session.id,
          session.projectId,
        );
        moveDraft(drafts.key, submittedDraftKey, session.id);
        if (sameSelection(submittedSelection, useWorkspace.getState())) {
          state.selectSession(session.id);
          state.loadPolicy(session.policy, session.policyVersion);
          submittedSelection = selectionOf(useWorkspace.getState());
        }
      }
      const input = [
        ...(submittedText.trim()
          ? [{ type: "text", text: submittedText }]
          : []),
        ...uploads.map((a) => ({
          type: a.mediaType.startsWith("image/") ? "image" : "file",
          attachmentId: a.id,
        })),
      ];
      if (sameSelection(submittedSelection, useWorkspace.getState())) {
        historyView.latest();
      }
      const result = await execute(
        {
          id: commandID(),
          deviceId: session.deviceId,
          sessionId: session.id,
          kind: commandKind,
          expectedTurnId: session.turnId || undefined,
          input,
          policy: state.policy,
          expectedPolicyVersion: activeSession
            ? state.policyBasis || activeSession.policyVersion
            : session.policyVersion,
        },
        {
          text: submittedText,
          key: submittedDraftKey,
          selection: submittedSelection,
        },
      );
      if (epoch !== privateEpoch.current) return;
      if (
        result.session &&
        sameSelection(submittedSelection, useWorkspace.getState()) &&
        useWorkspace.getState().sessionId === result.session.id
      )
        state.loadPolicy(result.session.policy, result.session.policyVersion);
      drafts.complete(submittedDraftKey, submittedText, submittedAttachments);
    } catch (e) {
      if (epoch === privateEpoch.current && !isUnconfirmedCommand(e))
        failure = (e as Error).message;
    } finally {
      composerActivity.finish(submission, failure);
    }
  }
  async function control(kind: string) {
    if (!activeSession) return;
    const key = `${activeSession.id}:${kind}`;
    if (controls.current.has(key)) return;
    controls.current.add(key);
    setError("");
    try {
      await execute({
        id: commandID(),
        deviceId: activeSession.deviceId,
        sessionId: activeSession.id,
        kind,
        requestId:
          kind === "execution.retire"
            ? activeSession.executionRecovery?.id
            : undefined,
        expectedTurnId: activeSession.turnId || undefined,
      });
    } catch (e) {
      if (!isUnconfirmedCommand(e)) setError((e as Error).message);
    } finally {
      controls.current.delete(key);
    }
  }
  function upload(files: FileList | null) {
    const selected = Array.from(files ?? []);
    if (!selected.length) return;
    if (!canAttach || !activeDevice || !target.project) {
      setError(attachmentUnavailable);
      return;
    }
    setError("");
    attachmentUploads.add(selected, {
      draftKey: drafts.key,
      deviceId: activeDevice.id,
      projectId: target.project.id,
      sessionId: state.sessionId,
    });
  }

  async function cancelQueued(item: import("@mewdex/sdk").QueueItem) {
    await execute({
      id: commandID(),
      deviceId: item.deviceId,
      sessionId: item.sessionId,
      kind: "queue.cancel",
      requestId: item.id,
      expectedRevision: item.revision,
    });
  }

  function focusComposer() {
    requestAnimationFrame(composerPosition.focus);
  }
  function openNetworkSettings(modelDeviceId?: string, setupExecutor = false) {
    setModelSettingsDevice(modelDeviceId);
    setExecutorSetup(setupExecutor);
    setNetworkSettings(true);
  }
  function openReceiptHistory() {
    setReceiptHistory(true);
    if (network.ready) recovery.refresh();
  }
  async function updateSession(session: Session, patch: SessionPatch) {
    const epoch = privateEpoch.current;
    setSessionSaving(true);
    try {
      const saved = await client.updateSession(session.id, patch);
      if (epoch !== privateEpoch.current) return;
      cache.setQueryData<Session[]>(["sessions"], (current) =>
        current?.map((item) =>
          item.id === saved.id
            ? {
                ...item,
                title: saved.title,
                pinned: saved.pinned,
                archived: saved.archived,
              }
            : item,
        ),
      );
      void cache.invalidateQueries({ queryKey: ["sessions"] });
    } finally {
      setSessionSaving(false);
    }
  }
  function openRenameSession(
    session: Session,
    origin: HTMLElement | null = null,
  ) {
    renameOrigin.current = origin;
    setRenameSession(session);
  }
  function changeCurrentSession(patch: SessionPatch) {
    if (!activeSession) return;
    setError("");
    void updateSession(activeSession, patch).catch((error: Error) =>
      setError(error.message),
    );
  }
  function newConversation() {
    if (activeSession)
      view.selectProject(activeSession.deviceId, activeSession.projectId);
    state.selectSession("");
    setSidebar(false);
    focusComposer();
  }
  function selectConversation(session: Session, focusInput = true) {
    if (session.deviceId !== state.deviceId)
      state.selectDevice(session.deviceId);
    if (session.id !== useWorkspace.getState().sessionId)
      state.selectSession(session.id);
    if (!useWorkspace.getState().policyDirty)
      state.loadPolicy(session.policy, session.policyVersion);
    setSidebar(false);
    if (focusInput) focusComposer();
  }
  function selectDevice(id: string) {
    if (id !== state.deviceId) state.selectDevice(id);
    setSidebar(false);
    focusComposer();
  }
  function selectProject(device: Device, projectId: string) {
    const current = useWorkspace.getState();
    if (current.deviceId !== device.id) state.selectDevice(device.id);
    else if (current.sessionId || view.projectFor(device) !== projectId)
      state.selectSession("");
    view.selectProject(device.id, projectId);
    setSidebar(false);
    focusComposer();
  }
  function continueDraft(key: string): string | undefined {
    const entry = localDrafts.find((item) => item.key === key);
    if (!entry) return "这份草稿已更新，请重新选择。";
    const target = draftDestination(
      key,
      cache.getQueryData<Device[]>(["devices"]) ?? [],
      cache.getQueryData<Session[]>(["sessions"]) ?? [],
    );
    if (!target.canOpen || !target.device)
      return target.notice || "原目标暂不可用，草稿仍保留。";
    if (target.session) selectConversation(target.session);
    else selectProject(target.device, target.projectId);
    setNetworkSettings(false);
  }
  function openActivityConversation(session: Session, focusRequests: boolean) {
    const current = cache.getQueryData<Session[]>(["sessions"]);
    const known = current?.find((item) => item.id === session.id);
    if (!known) cache.setQueryData(["sessions"], [...(current ?? []), session]);
    selectConversation(known ?? session, !focusRequests);
    if (focusRequests) interactionFocus.show();
  }
  function locateMessage(match: MessageMatch) {
    setConversationSearch(false);
    historyView.locate(match);
  }
  function toggleNavigation() {
    if (window.matchMedia("(max-width: 640px)").matches)
      setSidebar((value) => !value);
    else view.toggleNavigation();
  }
  useEffect(() => {
    if (network.unauthorized) return;
    function shortcut(event: KeyboardEvent) {
      if (
        event.isComposing ||
        event.altKey ||
        (event.target instanceof Element &&
          event.target.closest('[role="dialog"], [role="menu"]'))
      )
        return;
      if (!(event.ctrlKey || event.metaKey)) return;
      const key = event.key.toLowerCase();
      if (key === "k") {
        event.preventDefault();
        setSwitcher(true);
      } else if (key === "f" && !event.shiftKey && activeSession) {
        event.preventDefault();
        setConversationSearch(true);
      } else if (key === "n") {
        event.preventDefault();
        newConversation();
      } else if (isSettingsShortcut(event)) {
        event.preventDefault();
        openNetworkSettings();
      } else if (key === "l") {
        event.preventDefault();
        focusComposer();
      } else if (key === "b") {
        event.preventDefault();
        toggleNavigation();
      } else if (key === "j" && event.shiftKey) {
        event.preventDefault();
        setActivityOpen(true);
      } else if (key === "j" && activeSession) {
        event.preventDefault();
        setOutputPanel((value) => (value === "terminal" ? null : "terminal"));
      }
    }
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, [activeSession?.id, state.deviceId, network.unauthorized]);

  if (network.unauthorized)
    return (
      <div className="connect-screen">
        <main className="connect-card">
          <Brand />
          <h1>配对授权已失效</h1>
          <p>重新连接后再读取设备和对话。</p>
          <button className="primary connect-button" onClick={disconnectInput}>
            重新配对
          </button>
          <button className="reconnect-button" onClick={network.retry}>
            重试连接
          </button>
        </main>
      </div>
    );
  return (
    <div
      className={`workspace ${navigationVisible ? "" : "navigation-hidden"} ${fileTarget ? "has-project-files" : ""}`}
    >
      <WorkspaceSidebar
        open={sidebar}
        brand={<Brand compact />}
        width={view.sidebarWidth}
        onWidthChange={view.setSidebarWidth}
        devices={devices.data ?? []}
        sessions={sessions.data ?? []}
        deviceId={state.deviceId}
        sessionId={state.sessionId}
        connected={state.connectionState}
        draftSessions={draftSessionIds}
        unreadSessions={conversationReads.unread}
        activeTasks={
          activity.data?.filter((item) =>
            currentActivity(item, devices.data ?? []),
          ).length ?? 0
        }
        attentionTasks={
          activity.data?.filter(
            (item) =>
              currentActivity(item, devices.data ?? []) && item.needsAttention,
          ).length ?? 0
        }
        onActivity={() => {
          setSidebar(false);
          setActivityOpen(true);
        }}
        onSearch={() => {
          setSidebar(false);
          setSwitcher(true);
        }}
        onSettings={() => openNetworkSettings()}
        onLocalDevice={onLocalDevice}
        onClose={() => setSidebar(false)}
        onDevice={selectDevice}
        onSession={selectConversation}
        onNew={newConversation}
        onProject={selectProject}
        projectId={activeSession?.projectId ?? project}
        onEditProject={(device, selected, category) =>
          setWorkspaceManager({
            epoch: privateEpoch.current,
            deviceId: device.id,
            project: selected,
            category,
          })
        }
        onProjectCategory={async (device, selected, category) => {
          const epoch = privateEpoch.current;
          const result = await execute({
            id: commandID(),
            deviceId: device.id,
            projectId: selected.id,
            kind: "workspace.update",
            workspace: { path: selected.path, name: selected.name, category },
          });
          if (epoch !== privateEpoch.current) return;
          const updated = result.data as Project;
          cache.setQueryData<Device[]>(["devices"], (current) =>
            current?.map((item) =>
              item.id === device.id
                ? {
                    ...item,
                    projects: item.projects.map((entry) =>
                      entry.id === updated.id ? updated : entry,
                    ),
                  }
                : item,
            ),
          );
          await cache.invalidateQueries({ queryKey: ["devices"] });
        }}
        onWorkspaces={() =>
          setWorkspaceManager({
            epoch: privateEpoch.current,
            deviceId: state.deviceId,
          })
        }
        account={
          <AccountMenu
            client={client}
            devices={devices.data ?? []}
            selectedDeviceId={state.deviceId}
            localAvailable={!!onLocalDevice}
            connected={network.connected}
            admin={identity.data.role === "admin"}
            identity={identity.data}
            mewdexSupported={network.capabilities.includes("mewdex-accounts")}
            onLogout={disconnectInput}
            onLogin={(id) => openNetworkSettings(id)}
          />
        }
        canManageSessions={network.ready && !sessionSaving}
        onUpdateSession={updateSession}
        onRenameSession={openRenameSession}
      />
      <main
        ref={mainPanel}
        style={
          {
            "--mewdex-output-width": `${view.outputWidth}%`,
          } as React.CSSProperties
        }
        className={`main-panel ${outputPanel ? "has-output" : ""}`}
        onDragEnter={(event) => {
          if (!event.dataTransfer.types.includes("Files")) return;
          event.preventDefault();
          dragDepth.current++;
          setDroppingFiles(true);
        }}
        onDragOver={(event) => {
          if (event.dataTransfer.types.includes("Files")) {
            event.preventDefault();
            event.dataTransfer.dropEffect = canAttach ? "copy" : "none";
          }
        }}
        onDragLeave={() => {
          dragDepth.current = Math.max(0, dragDepth.current - 1);
          if (dragDepth.current === 0) setDroppingFiles(false);
        }}
        onDrop={(event) => {
          dragDepth.current = 0;
          setDroppingFiles(false);
          if (!event.dataTransfer.files.length) return;
          event.preventDefault();
          void upload(event.dataTransfer.files);
        }}
      >
        {droppingFiles && (
          <div className={`file-drop-target ${canAttach ? "" : "unavailable"}`}>
            <Paperclip size={30} />
            <strong>{canAttach ? "松开以添加附件" : "暂时无法添加附件"}</strong>
            <span>
              {canAttach
                ? `${activeDevice?.name} · ${activeSession?.title ?? target.project?.name}`
                : attachmentUnavailable}
            </span>
          </div>
        )}
        <header className="workspace-header">
          <button
            className="icon-button navigation-toggle"
            aria-label="切换对话导航"
            title="切换导航 · Ctrl+B"
            onClick={toggleNavigation}
          >
            <Menu size={20} />
          </button>
          <div className="breadcrumb">
            <div className="workspace-context">
              <FolderOpen size={13} />
              <span>{target.project?.name ?? "工作台"}</span>
              <span className="workspace-device-name">
                <Monitor size={12} />
                {activeDevice?.name ?? "选择设备"}
              </span>
            </div>
            <strong>{activeSession?.title ?? "新对话"}</strong>
          </div>
          <div className="header-actions">
            {activeSession && (
              <>
                <button
                  className="header-action-button"
                  aria-label="项目文件"
                  title="项目文件"
                  aria-expanded={!!fileTarget}
                  onClick={() =>
                    setFileTarget(
                      fileTarget
                        ? null
                        : {
                            session: activeSession,
                            epoch: privateEpoch.current,
                          },
                    )
                  }
                >
                  <FolderOpen size={16} />
                  <span>项目文件</span>
                </button>
                <button
                  className="header-action-button"
                  aria-label="查看输出"
                  title="查看输出"
                  aria-pressed={!!outputPanel}
                  onClick={() => setOutputPanel(outputPanel ? null : "diff")}
                >
                  <PanelRight size={16} />
                  <span>查看输出</span>
                </button>
              </>
            )}
            <button
              className="icon-button"
              aria-label="搜索设备、项目与对话"
              title="快速切换 · Ctrl+K"
              onClick={() => setSwitcher(true)}
            >
              <Search size={18} />
            </button>
            <span
              className={`state-pill ${waitingForReply ? "waiting" : activeSession?.state === "running" ? "active" : ""}`}
            >
              <Circle size={7} fill="currentColor" />
              {state.connectionState !== "connected"
                ? state.connectionState === "connecting"
                  ? "正在连接"
                  : "连接已断开"
                : target.issue
                  ? target.label
                  : activeDevice && !activeDevice.online
                    ? "设备离线"
                    : waitingForReply
                      ? "等待回复"
                      : !activeSession && modelAccountMissing
                        ? "待连接模型账号"
                        : !activeSession &&
                            modelStatusEnabled &&
                            modelAccount.isPending
                          ? "读取账号状态"
                          : !activeSession &&
                              modelStatusEnabled &&
                              modelAccount.isError
                            ? "账号状态待确认"
                            : stateLabel(activeSession)}
            </span>
            <ConversationActions
              open={conversationOptions}
              onOpenChange={setConversationOptions}
              session={activeSession}
              canManage={network.ready && !sessionSaving}
              canStop={!!activeSession?.engineStarted && !executionRetired}
              onPolicy={() => setSettings(true)}
              onOutput={setOutputPanel}
              onQueue={() => setQueueHistory(true)}
              onReceipts={openReceiptHistory}
              onStop={() => void control("stop")}
              onRename={(origin) =>
                activeSession && openRenameSession(activeSession, origin)
              }
              onPin={() =>
                changeCurrentSession({ pinned: !activeSession?.pinned })
              }
              onArchive={() =>
                changeCurrentSession({ archived: !activeSession?.archived })
              }
              onLibrary={() => setArchiveLibrary(true)}
              onActivity={() => setActivityOpen(true)}
              onFiles={() =>
                activeSession &&
                setFileTarget({
                  session: activeSession,
                  epoch: privateEpoch.current,
                })
              }
              onSearch={() => setConversationSearch(true)}
            />
          </div>
        </header>
        <div className="feed" ref={feed} onScroll={historyView.onScroll}>
          {!activeSession && (
            <ConversationWelcome
              device={activeDevice}
              target={target}
              connected={network.connected}
              loading={devices.isPending}
              hasExecutors={
                !!devices.data?.some((item) => item.roles.includes("executor"))
              }
              canPair={identity.data.role === "admin"}
              onChoose={() => setSwitcher(true)}
              onPair={() => openNetworkSettings(undefined, true)}
              onLocalDevice={onLocalDevice}
              onCreateProject={
                identity.data.role === "admin" &&
                activeDevice?.roles.includes("executor")
                  ? () =>
                      setWorkspaceManager({
                        epoch: privateEpoch.current,
                        deviceId: activeDevice.id,
                      })
                  : undefined
              }
            />
          )}
          {!activeSession && busy && <AssistantActivity sending />}
          {activeSession && (
            <div className="message-list">
              <div className="history-controls" aria-live="polite">
                {historyView.loadingOlder ? (
                  <span>正在加载更早的消息…</span>
                ) : historyView.hasOlder ? (
                  <button onClick={() => historyView.earlier()}>
                    向上滚动，读取更早消息
                  </button>
                ) : (
                  !history.isPending && <span>对话起点</span>
                )}
                {historyView.target && <span>已定位搜索结果</span>}
                {history.error && (
                  <button onClick={() => void history.refetch()}>
                    读取失败，点击重试
                  </button>
                )}
              </div>
              {messages.length === 0 && (
                <div className="quiet-state">
                  {history.isFetching
                    ? "正在读取对话…"
                    : history.error
                      ? "暂时无法读取对话记录。"
                      : "当前记录没有可展示的消息。"}
                </div>
              )}
              <MessageList
                messages={messages}
                focusedMessageId={historyView.target?.id}
                historical={!historyView.live}
                onCommandOutput={openCommandOutput}
                client={client}
                onFileReference={activeSession ? openFileReference : undefined}
              />
              {historyView.live &&
                !waitingForReply &&
                network.connected &&
                activeDevice?.online &&
                (busy || activeSession.state === "running") && (
                  <AssistantActivity sending={busy} />
                )}
              {taskPlansAvailable && (
                <TaskPlanCard
                  key={`plan:${state.sessionId}:${taskPlan.data?.turnId ?? ""}`}
                  plan={taskPlan.data}
                  session={activeSession}
                  historical={!historyView.live}
                  connected={network.connected}
                  online={activeDevice?.online ?? false}
                  unavailable={taskPlan.isError}
                  refreshing={taskPlan.isFetching}
                  onRetry={() => void taskPlan.refetch()}
                />
              )}
              {historyView.live &&
                (!!interactions.data?.length || !!interactions.error) && (
                  <InteractionPanel
                    responses={responses}
                    session={activeSession}
                    online={
                      network.connected && (activeDevice?.online ?? false)
                    }
                    requests={interactions.data ?? []}
                    error={interactions.error}
                    onRefresh={() => interactions.refetch()}
                    containerRef={interactionList}
                  />
                )}
            </div>
          )}
        </div>
        {outputPanel && (
          <Suspense
            fallback={<div className="terminal-panel">正在打开输出面板…</div>}
          >
            <OutputPanel
              key={historyView.contextKey}
              view={outputReading.get(historyView.contextKey)}
              loading={historyView.restoring}
              kind={outputPanel}
              onKindChange={setOutputPanel}
              onClose={() => setOutputPanel(null)}
              commands={commandOutputs}
              diff={turnDiff}
              focusCommand={
                outputCommand?.contextKey === historyView.contextKey
                  ? outputCommand
                  : null
              }
              historical={!historyView.live}
              connected={network.connected}
              onFileReference={activeSession ? openFileReference : undefined}
            />
          </Suspense>
        )}
        {outputPanel && (
          <OutputResizeHandle
            container={mainPanel}
            width={view.outputWidth}
            onCommit={view.setOutputWidth}
          />
        )}
        <div className="composer-area">
          <NetworkConnectionNotice
            connected={network.connected}
            reconnecting={network.reconnecting}
            error={network.error}
            address={connection.baseURL}
            localAvailable={!!onLocalDevice}
            onRetry={network.retry}
            onLocalDevice={onLocalDevice}
          />
          {!!interactions.data?.length && (
            <button
              className="pending-interaction-jump"
              onClick={interactionFocus.show}
              aria-label={
                historyView.live
                  ? "查看当前待处理请求"
                  : "返回最新对话并查看待处理请求"
              }
            >
              <MessageCircleQuestion size={15} />
              <span>{interactions.data.length} 项当前请求待处理</span>
              <strong>{historyView.live ? "查看" : "返回当前"}</strong>
            </button>
          )}
          {activeSession && (readingEarlier || !historyView.live) && (
            <button
              className="latest-message-button"
              onClick={() => historyView.latest()}
            >
              <ArrowDown size={14} />
              {conversationReads.unread.has(activeSession.id)
                ? "查看新回复"
                : "回到最新"}
            </button>
          )}
          {activeSession && (
            <QueuePanel
              key={activeSession.id}
              client={client}
              session={activeSession}
              online={!!activeDevice?.online}
              onCancel={cancelQueued}
            />
          )}
          <CommandRecoveryNotice
            view={recoveryView}
            hasMore={recovery.hasMore}
            error={recovery.error}
            onOpen={openReceiptHistory}
          />
          {error && (
            <div className="error" role="alert">
              {error}
              {error && (
                <button aria-label="关闭错误" onClick={() => setError("")}>
                  <X size={14} />
                </button>
              )}
            </div>
          )}
          {drafts.persistenceError && (
            <div className="error" role="alert">
              {drafts.persistenceError}
              <button onClick={() => openNetworkSettings()}>管理草稿</button>
            </div>
          )}
          {activeSession?.archived && (
            <div className="archived-conversation-banner" role="status">
              <span>
                <strong>此对话已归档</strong>
                {activeSession.state === "running"
                  ? "任务继续执行；恢复对话后可追加要求。"
                  : "恢复后可以继续发送消息。"}
              </span>
              <button
                disabled={!network.ready || sessionSaving}
                onClick={() => changeCurrentSession({ archived: false })}
              >
                {sessionSaving ? "正在保存…" : "恢复对话"}
              </button>
            </div>
          )}
          {activeSession && target.issue && !executionRetired && (
            <div className="execution-target-notice" role="status">
              <span>{target.message}</span>
              <button onClick={() => setSwitcher(true)}>选择设备或项目</button>
            </div>
          )}
          {modelStatusEnabled && !activeSession?.archived && (
            <ModelConnectionNotice
              account={modelAccount.data}
              pending={!!pendingModelLogin}
              error={modelAccount.isError}
              refreshing={modelAccount.isFetching}
              onManage={() => openNetworkSettings(state.deviceId)}
              onRefresh={() => {
                void modelAccount.refetch();
              }}
            />
          )}
          {activeSession?.stopStatus === "background-running" &&
            !activeSession.frozen && (
              <div className="paused-banner" role="status">
                <span>后台任务仍在运行，排队消息会等待它退出。</span>
                <button onClick={() => setOutputPanel("terminal")}>
                  查看输出
                </button>
                <button onClick={() => void control("stop")}>
                  停止后台任务
                </button>
              </div>
            )}
          {executionRetired && (
            <div className="paused-banner" role="status">
              <span>
                旧执行已结束，未确认的结果仍保持未知。原输入和队列保留供查阅。
              </span>
              <button onClick={() => state.selectSession("")}>
                新建对话继续
              </button>
            </div>
          )}
          {activeSession?.frozen && !executionRetired && (
            <div className="paused-banner">
              {activeSession.state === "unknown"
                ? "执行状态待核实，队列保持冻结。"
                : activeSession.state === "running"
                  ? "后续队列已冻结，当前回合仍在执行。"
                  : activeSession.stopStatus === "processes-unchecked"
                    ? "回合已结束，需核实后台进程。"
                    : "任务队列已冻结。"}
              {activeSession.state === "unknown" && (
                <button onClick={() => void control("reconcile")}>
                  核实状态
                </button>
              )}
              {activeSession.stopStatus === "processes-unchecked" && (
                <button onClick={() => void control("stop")}>
                  停止并清理后台进程
                </button>
              )}
              {activeSession.executionRecovery?.state === "eligible" && (
                <div>
                  <p>
                    已核对执行设备在旧任务之后重新启动。可结束旧执行并解除项目占用，未确认的结果会继续保留为未知，原输入不会重发。
                  </p>
                  <button
                    disabled={!activeDevice?.online}
                    onClick={() => void control("execution.retire")}
                  >
                    确认结束旧执行
                  </button>
                </div>
              )}
              <button
                disabled={
                  ["unknown", "running", "stopping"].includes(
                    activeSession.state,
                  ) ||
                  ["unconfirmed", "processes-unchecked"].includes(
                    activeSession.stopStatus,
                  )
                }
                onClick={() => void control("resume")}
              >
                恢复任务
              </button>
            </div>
          )}
          <ConversationComposer
            projectLabel={target.project?.name ?? "选择项目"}
            editor={{
              ref: composerInput,
              draftKey: drafts.key,
              text,
              readOnly: !target.canEdit || executionRetired,
              onChange: setText,
              onRemember: composerPosition.remember,
              placeholder: executionRetired
                ? "旧执行已结束，原输入保留供复制。"
                : !target.canEdit
                  ? target.message
                  : activeSession?.archived
                    ? "此对话已归档，恢复后可发送…"
                    : activeDevice && !activeDevice.online
                      ? "设备离线，可先写下要求…"
                      : modelAccountMissing
                        ? "先连接模型账号，草稿会保留在这里…"
                        : modelStatusEnabled && modelAccount.isPending
                          ? "正在读取模型账号状态，可先写下要求…"
                          : activeSession?.state === "running"
                            ? "追加要求，调整当前任务…"
                            : "描述你的目标，或继续已有的工作…",
            }}
            files={{
              canAdd: canAttach,
              unavailable: attachmentUnavailable,
              onAdd: upload,
            }}
            actions={{
              intent: submissionIntent,
              busy,
              canSend,
              canQueue,
              canStop:
                !!activeSession?.engineStarted &&
                !executionRetired &&
                (activeSession.state === "running" ||
                  activeSession.state === "stopping" ||
                  activeSession.stopStatus === "background-running"),
              onSend: (kind) => {
                void send(kind);
              },
              onStop: () => {
                void control("stop");
              },
            }}
            sendKey={view.sendKey}
            context={[
              activeDevice?.name ?? "未选择执行端",
              target.project?.name ?? activeSession?.projectId,
              uploading
                ? `${attachmentUploads.pending} 个附件准备中`
                : attachmentUploads.blocked
                  ? "附件待处理"
                  : "",
            ]
              .filter(Boolean)
              .join(" · ")}
            status={
              executionRetired
                ? "旧执行已结束"
                : !target.canEdit
                  ? text
                    ? "原输入保留供复制"
                    : target.label
                  : target.issue
                    ? target.label
                    : ""
            }
            planMode={state.policy.collaborationMode === "plan"}
            controls={
              <ComposerControls
                client={client}
                device={activeDevice}
                projectId={activeSession?.projectId ?? project}
                session={activeSession}
                value={state.policy}
                onChange={state.setPolicy}
                disabled={busy || !target.canEdit || executionRetired}
                connected={network.connected}
                dirty={state.policyDirty}
                onSettings={() => setSettings(true)}
              />
            }
            projectControl={
              !activeSession && (
                <select
                  aria-label="选择项目"
                  value={project}
                  disabled={!activeDevice?.projects.length}
                  onChange={(event) => {
                    if (activeDevice)
                      selectProject(activeDevice, event.target.value);
                  }}
                >
                  {!activeDevice?.projects.length && (
                    <option value="">
                      {activeDevice ? "暂无可用项目" : "请先选择执行设备"}
                    </option>
                  )}
                  {activeDevice?.projects.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                      {draftKeys.has(composerKey(state.deviceId, "", item.id))
                        ? " · 草稿"
                        : ""}
                    </option>
                  ))}
                </select>
              )
            }
          >
            {(attachmentUploads.items.length > 0 || uploads.length > 0) && (
              <div className="composer-file-area">
                <AttachmentUploadList
                  items={attachmentUploads.items}
                  onRetry={attachmentUploads.retry}
                  onRemove={attachmentUploads.remove}
                />
                {uploads.length > 0 && (
                  <AttachmentList
                    client={client}
                    items={uploads.map((file) => ({
                      id: file.id,
                      metadata: file,
                    }))}
                    onRemove={(id) =>
                      setUploads((files) =>
                        files.filter((file) => file.id !== id),
                      )
                    }
                  />
                )}
              </div>
            )}
          </ConversationComposer>
        </div>
      </main>
      <QuickSwitcher
        open={switcher}
        onOpenChange={setSwitcher}
        devices={devices.data ?? []}
        sessions={sessions.data ?? []}
        draftSessions={draftSessionIds}
        unreadSessions={conversationReads.unread}
        connected={network.connected}
        draftKeys={draftKeys}
        deviceId={state.deviceId}
        onDevice={selectDevice}
        onSession={selectConversation}
        onProject={selectProject}
      />
      <ActivityDialog
        open={activityOpen}
        onOpenChange={setActivityOpen}
        items={activity.data}
        devices={devices.data ?? []}
        ready={network.ready}
        loading={network.ready && activity.isPending}
        refreshing={activity.isFetching}
        error={activity.error?.message ?? ""}
        onRefresh={() => {
          void activity.refetch();
        }}
        onSession={openActivityConversation}
      />
      <CommandRecoveryDialog
        key={`receipts:${privateEpoch.current}`}
        open={receiptHistory}
        onOpenChange={setReceiptHistory}
        client={client}
        view={recoveryView}
        after={recovery.after}
        hasMore={recovery.hasMore}
        busy={recovery.busy}
        ready={network.ready}
        connected={network.connected}
        error={recovery.error}
        canAdvance={recovery.canAdvance}
        onRefresh={recovery.refresh}
        onFirst={recovery.first}
        onNext={recovery.next}
        onResolved={resolvePending}
        onDismiss={dismissPending}
        onSession={selectConversation}
      />
      {renameSession && (
        <RenameSessionDialog
          key={`rename:${renameSession.id}`}
          session={renameSession}
          returnFocus={renameOrigin.current}
          onClose={() => {
            setRenameSession(null);
            renameOrigin.current = null;
          }}
          onUpdate={updateSession}
          busy={sessionSaving}
        />
      )}
      {conversationSearch && activeSession && (
        <ConversationSearchDialog
          key={`search:${activeSession.id}`}
          client={client}
          session={activeSession}
          initialQuery={searchTerm}
          onSearch={setSearchTerm}
          onLocate={locateMessage}
          onClose={() => setConversationSearch(false)}
        />
      )}
      <ArchivedSessionsDialog
        open={archiveLibrary}
        onOpenChange={setArchiveLibrary}
        devices={devices.data ?? []}
        sessions={sessions.data ?? []}
        onUpdate={updateSession}
        onSession={selectConversation}
        canManage={network.ready && !sessionSaving}
      />
      {fileTarget && (
        <ProjectFilesDialog
          key={`files:${fileTarget.session.id}:${fileTarget.file ?? ""}`}
          client={client}
          session={fileTarget.session}
          device={devices.data?.find(
            (device) => device.id === fileTarget.session.deviceId,
          )}
          initialFile={fileTarget.file}
          initialError={fileTarget.error}
          localAvailable={!!onLocalDevice}
          onClose={() => setFileTarget(null)}
          onUnconfirmed={(command) => {
            if (fileTarget.epoch === privateEpoch.current)
              setPendingCommands((items) =>
                items.some((item) => item.command.id === command.id)
                  ? items
                  : [...items, { command, draft: "" }],
              );
          }}
        />
      )}
      {workspaceManager && (
        <WorkspaceManagerDialog
          centralProjects={network.capabilities.includes("central-projects")}
          client={client}
          devices={devices.data ?? []}
          initialDeviceId={workspaceManager.deviceId}
          initialProject={workspaceManager.project}
          initialCategory={workspaceManager.category}
          localAvailable={!!onLocalDevice}
          onClose={() => setWorkspaceManager(null)}
          onSelect={(device, selected) => selectProject(device, selected.id)}
          onUnconfirmed={(command) => {
            if (workspaceManager.epoch === privateEpoch.current)
              setPendingCommands((items) =>
                items.some((item) => item.command.id === command.id)
                  ? items
                  : [...items, { command, draft: "" }],
              );
          }}
        />
      )}
      <NetworkSettings
        client={client}
        devices={devices.data ?? []}
        identity={identity.data}
        open={networkSettings}
        onOpenChange={setNetworkSettings}
        initialModelDeviceId={modelSettingsDevice}
        initialExecutorSetup={executorSetup}
        alerts={alerts}
        baseURL={connection.baseURL}
        loginKey={connection.credentials.token}
        accountSupported={network.capabilities.includes("mewdex-accounts")}
        onLocalDevice={onLocalDevice}
        onDisconnect={disconnectInput}
        draftSettings={{
          entries: localDrafts,
          error: drafts.persistenceError,
          onClear: discardComposerDrafts,
          onOpen: continueDraft,
        }}
        workspaceView={{
          restoreLastLocation: view.restoreLastLocation,
          onRestoreLastLocationChange: view.setRestoreLastLocation,
          sendKey: view.sendKey,
          onSendKeyChange: view.setSendKey,
          error: view.error,
        }}
      />
      {activeSession && (
        <QueueHistoryDialog
          key={`queue:${activeSession.id}`}
          open={queueHistory}
          onOpenChange={setQueueHistory}
          client={client}
          session={activeSession}
          online={!!activeDevice?.online}
          onCancel={cancelQueued}
        />
      )}
      <PolicySettings
        client={client}
        open={settings}
        onOpenChange={setSettings}
        deviceId={state.deviceId}
        deviceName={activeDevice?.name}
        projectId={project}
        session={activeSession}
        online={activeDevice?.online ?? false}
        capabilities={(activeDevice?.capabilities ?? []).filter(
          (capability) =>
            capability !== "collaboration-modes" ||
            network.capabilities.includes("collaboration-modes"),
        )}
        editor={policyEditor}
        onApply={(policy, version) => {
          if (!policyEditor.isCurrent()) return;
          if (version) state.loadPolicy(policy, version);
          else state.setPolicy(policy);
        }}
      />
    </div>
  );
}

function stateLabel(session?: Session): string {
  if (!session) return "就绪";
  if (session.executionRecovery?.state === "retired") return "旧执行已结束";
  return (
    (
      {
        idle: "就绪",
        running: "执行中",
        stopping: "停止中",
        paused: "已暂停",
        failed: "执行失败",
        unknown: "状态待确认",
      } as Record<string, string>
    )[session.state] ?? session.state
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queries}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
