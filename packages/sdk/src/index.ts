export * from "./protocol.ts";
export { EventWindow } from "./timeline.ts";
import {
  PROTOCOL_VERSION,
  type Attachment,
  type Access,
  type Command,
  type CommandResult,
  type Device,
  type DeviceLogin,
  type Event,
  type EventPage,
  type MessageSearchPage,
  type ExecutionObservation,
  type Identity,
  type NetworkSnapshot,
  type ModelAccount,
  type FileListing,
  type OutputFile,
  type PendingCommandPage,
  type Interaction,
  type InteractionReply,
  type Policy,
  type PolicySnapshot,
  type PromptInspection,
  type QueueItem,
  type Session,
  type SessionPatch,
  type SessionActivity,
  type ModelPage,
  type TaskPlan,
  type ModelRateLimits,
  type WorkspaceListing,
  type WorkspaceRequest,
  type Project,
  type MewDexAccount,
  type ServerInfo,
} from "./protocol.ts";

export class MewDexError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "MewDexError";
  }
}

export class CommandOutcomeError extends MewDexError {
  constructor(
    readonly commandId: string,
    readonly state: string,
    message: string,
  ) {
    super(
      `${message}（指令 ${commandId}）`,
      state === "accepted" || state === "unknown" ? 202 : 409,
    );
    this.name = "CommandOutcomeError";
  }
}

export interface Credentials {
  token: string;
  identity: Identity;
  protocolVersion: number;
}
export interface SDKOptions {
  baseURL: string;
  allowPrivateHTTP?: boolean;
  token?: string;
  fetch?: typeof globalThis.fetch;
  createWebSocket?: (url: string) => WebSocket;
}

/** No React, Tauri, storage or executor dependencies. Platform adapters own credentials. */
export class MewDexClient {
  private readonly base: string;
  private readonly fetcher: typeof globalThis.fetch;
  private readonly socket: (url: string) => WebSocket;
  private token: string;

  constructor(options: SDKOptions) {
    const parsed = new URL(options.baseURL);
    if (
      !["https:", "http:"].includes(parsed.protocol) ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash
    )
      throw new Error("Invalid Hub URL");
    const hostname = parsed.hostname.replace(/^\[|\]$/g, "");
    const ipv4 = hostname.split(".").map(Number);
    const numericIPv4 =
      ipv4.length === 4 &&
      ipv4.every((part) => Number.isInteger(part) && part >= 0 && part <= 255);
    const loopback =
      hostname === "localhost" ||
      hostname === "::1" ||
      (numericIPv4 && ipv4[0] === 127);
    const privateIP =
      (numericIPv4 &&
        (ipv4[0] === 10 ||
          (ipv4[0] === 192 && ipv4[1] === 168) ||
          (ipv4[0] === 172 && ipv4[1] >= 16 && ipv4[1] <= 31))) ||
      /^(fc|fd)[0-9a-f]{2}:/i.test(hostname);
    if (
      parsed.protocol === "http:" &&
      !loopback &&
      !(options.allowPrivateHTTP && privateIP)
    )
      throw new Error("Hub 地址需要 HTTPS；局域网 HTTP 仅可显式启用调试选项。");
    this.base = parsed.toString().replace(/\/$/, "");
    this.token = options.token ?? "";
    this.fetcher = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.socket = options.createWebSocket ?? ((url) => new WebSocket(url));
  }

  private async response(
    path: string,
    init: RequestInit = {},
  ): Promise<Response> {
    const headers = new Headers(init.headers);
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`);
    if (init.body && !headers.has("Content-Type"))
      headers.set("Content-Type", "application/json");
    const response = await this.fetcher(this.base + path, {
      ...init,
      headers,
      redirect: "error",
    });
    if (!response.ok) {
      const body: unknown = response.headers
        .get("Content-Type")
        ?.includes("application/json")
        ? await response.json().catch(() => null)
        : null;
      const message =
        typeof body === "object" && body && "error" in body
          ? String(body.error)
          : response.statusText || `HTTP ${response.status}`;
      throw new MewDexError(message, response.status);
    }
    return response;
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    return (await this.response(path, init)).json() as Promise<T>;
  }

  async pair(
    code: string,
    name: string,
    deviceId?: string,
  ): Promise<Credentials> {
    const result = await this.request<Credentials>("/v1/pair", {
      method: "POST",
      body: JSON.stringify({ code, name, deviceId }),
    });
    if (result.protocolVersion !== PROTOCOL_VERSION)
      throw new Error("Hub protocol version is not supported");
    this.token = result.token;
    return result;
  }
  /** Reuse an existing input identity without consuming a pairing invitation. */
  async loginWithKey(signal?: AbortSignal): Promise<Credentials> {
    if (!this.token.trim()) throw new Error("请输入登录密钥。");
    const identity = await this.identity(signal);
    if (identity.role !== "client" && identity.role !== "admin")
      throw new Error("此密钥不是输入端登录密钥。");
    return { token: this.token, identity, protocolVersion: PROTOCOL_VERSION };
  }
  async checkServer(signal?: AbortSignal): Promise<ServerInfo> {
    const info = await this.request<ServerInfo>("/health", { signal });
    if (info.name !== "MewDex" || info.protocolVersion !== PROTOCOL_VERSION)
      throw new Error("此地址不是兼容的 MewDex 中央后端。");
    if (!info.authentication)
      return {
        ...info,
        authentication: {
          methods: ["key", "pair"],
          defaultMethod: "key",
          accountConfigured: false,
        },
      };
    const methods =
      info.authentication.methods?.filter((method) =>
        ["password", "key", "pair"].includes(method),
      ) ?? [];
    if (!methods.length)
      throw new Error("此中央后端没有提供当前终端支持的认证方式。");
    return {
      ...info,
      authentication: {
        ...info.authentication,
        methods,
        defaultMethod: methods.includes(info.authentication.defaultMethod)
          ? info.authentication.defaultMethod
          : methods[0],
      },
    };
  }
  async login(
    username: string,
    password: string,
    signal?: AbortSignal,
  ): Promise<Credentials> {
    const result = await this.request<Credentials>("/v1/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
      signal,
    });
    if (
      result.protocolVersion !== PROTOCOL_VERSION ||
      !result.token ||
      result.identity?.role !== "admin" ||
      !result.identity.accountId
    )
      throw new Error("中央后端没有返回有效的 MewDex 账号登录信息。");
    this.token = result.token;
    return result;
  }
  mewdexAccount(): Promise<MewDexAccount | null> {
    return this.request("/v1/mewdex/account");
  }
  configureMewDexAccount(
    username: string,
    name: string,
    password: string,
  ): Promise<MewDexAccount> {
    return this.request("/v1/mewdex/account", {
      method: "PUT",
      body: JSON.stringify({ username, name, password }),
    });
  }
  devices(): Promise<Device[]> {
    return this.request("/v1/devices");
  }
  bootstrap(signal?: AbortSignal): Promise<NetworkSnapshot> {
    return this.request("/v1/bootstrap", { signal });
  }
  pendingCommands(
    deviceId: string,
    after = 0,
    signal?: AbortSignal,
  ): Promise<PendingCommandPage> {
    return this.request(
      `/v1/pending-commands?${new URLSearchParams({ deviceId, after: String(after), limit: "20" })}`,
      { signal },
    );
  }
  historyPage(
    sessionId: string,
    before = 0,
    limit = 200,
    signal?: AbortSignal,
  ): Promise<EventPage> {
    return this.request(
      `/v1/session-events?${new URLSearchParams({ sessionId, before: String(before), limit: String(limit) })}`,
      { signal },
    );
  }

  historyAfter(
    sessionId: string,
    after: number,
    limit = 200,
    signal?: AbortSignal,
  ): Promise<EventPage> {
    return this.request(
      `/v1/session-events?${new URLSearchParams({ sessionId, after: String(after), limit: String(limit) })}`,
      { signal },
    );
  }

  searchMessages(
    sessionId: string,
    query: string,
    before = 0,
    limit = 30,
    signal?: AbortSignal,
  ): Promise<MessageSearchPage> {
    return this.request(
      `/v1/message-search?${new URLSearchParams({ sessionId, q: query, before: String(before), limit: String(limit) })}`,
      { signal },
    );
  }

  taskPlan(
    sessionId: string,
    before = 0,
    signal?: AbortSignal,
  ): Promise<TaskPlan | null> {
    return this.request(
      `/v1/task-plan?${new URLSearchParams({ sessionId, before: String(before) })}`,
      { signal },
    );
  }

  identity(signal?: AbortSignal): Promise<Identity> {
    return this.request("/v1/me", { signal });
  }
  sessions(deviceId?: string): Promise<Session[]> {
    return this.request(
      "/v1/sessions" +
        (deviceId ? `?deviceId=${encodeURIComponent(deviceId)}` : ""),
    );
  }
  activity(signal?: AbortSignal): Promise<SessionActivity[]> {
    return this.request("/v1/activity", { signal });
  }
  async projectFiles(
    session: Pick<Session, "id" | "deviceId">,
    path = ".",
    after = "",
  ): Promise<FileListing> {
    const result = await this.run({
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "file.list",
      file: { path, after },
    });
    return result.data as FileListing;
  }
  async exportFile(
    session: Pick<Session, "id" | "deviceId">,
    path: string,
    id = commandID(),
  ): Promise<OutputFile> {
    const result = await this.run({
      id,
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "file.export",
      file: { path },
    });
    return result.data as OutputFile;
  }
  outputs(sessionId: string, signal?: AbortSignal): Promise<OutputFile[]> {
    return this.request(
      `/v1/file-exports?${new URLSearchParams({ sessionId })}`,
      { signal },
    );
  }
  async modelAccount(deviceId: string): Promise<ModelAccount> {
    const result = await this.run({
      id: commandID(),
      deviceId,
      kind: "account.read",
    });
    return result.data as ModelAccount;
  }
  async modelRateLimits(deviceId: string): Promise<ModelRateLimits> {
    const result = await this.run({
      id: commandID(),
      deviceId,
      kind: "account.limits",
    });
    return result.data as ModelRateLimits;
  }
  async logoutModelAccount(deviceId: string): Promise<void> {
    await this.run({ id: commandID(), deviceId, kind: "account.logout" });
  }
  async browseWorkspaces(
    deviceId: string,
    path = "",
  ): Promise<WorkspaceListing> {
    const result = await this.run({
      id: commandID(),
      deviceId,
      kind: "workspace.browse",
      workspace: { path },
    });
    return result.data as WorkspaceListing;
  }
  async createWorkspace(
    deviceId: string,
    workspace: WorkspaceRequest,
    id = commandID(),
  ): Promise<Project> {
    const result = await this.run({
      id,
      deviceId,
      kind: "workspace.create",
      workspace,
    });
    return result.data as Project;
  }
  async updateWorkspace(
    deviceId: string,
    projectId: string,
    workspace: WorkspaceRequest,
    id = commandID(),
  ): Promise<Project> {
    const result = await this.run({
      id,
      deviceId,
      projectId,
      kind: "workspace.update",
      workspace,
    });
    return result.data as Project;
  }
  async models(
    target: { deviceId: string; projectId?: string; sessionId?: string },
    cursor?: string,
  ): Promise<ModelPage> {
    const result = await this.run({
      id: commandID(),
      ...target,
      kind: "model.list",
      models: { cursor, limit: 30 },
    });
    return result.data as ModelPage;
  }
  async startModelLogin(deviceId: string): Promise<DeviceLogin> {
    const result = await this.run({
      id: commandID(),
      deviceId,
      kind: "account.login",
    });
    return result.data as DeviceLogin;
  }
  async cancelModelLogin(deviceId: string, loginId: string): Promise<void> {
    await this.run({
      id: commandID(),
      deviceId,
      kind: "account.cancel",
      requestId: loginId,
    });
  }
  updateSession(
    sessionId: string,
    patch: SessionPatch,
    signal?: AbortSignal,
  ): Promise<Session> {
    return this.request(`/v1/sessions/${encodeURIComponent(sessionId)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
      signal,
    });
  }
  interactions(
    sessionId: string,
    signal?: AbortSignal,
  ): Promise<Interaction[]> {
    return this.request(
      `/v1/interactions?${new URLSearchParams({ sessionId })}`,
      { signal },
    );
  }
  policies(sessionId: string): Promise<PolicySnapshot[]> {
    return this.request(`/v1/policies?${new URLSearchParams({ sessionId })}`);
  }
  queue(sessionId: string): Promise<QueueItem[]> {
    return this.request(`/v1/queue?${new URLSearchParams({ sessionId })}`);
  }
  async cancelQueued(item: QueueItem): Promise<QueueItem> {
    const result = await this.run({
      id: commandID(),
      deviceId: item.deviceId,
      sessionId: item.sessionId,
      kind: "queue.cancel",
      requestId: item.id,
      expectedRevision: item.revision,
    });
    return result.data as QueueItem;
  }
  async reconcile(
    session: Session,
  ): Promise<{ session: Session; observation: ExecutionObservation }> {
    const result = await this.run({
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "reconcile",
    });
    if (!result.session)
      throw new Error("Execution observation has no session");
    return {
      session: result.session,
      observation: result.data as ExecutionObservation,
    };
  }
  async retireExecution(session: Session): Promise<Session> {
    if (!session.executionRecovery?.id)
      throw new Error("This execution has no verified recovery review");
    const result = await this.run({
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "execution.retire",
      requestId: session.executionRecovery.id,
      expectedTurnId: session.turnId || undefined,
    });
    if (!result.session) throw new Error("Execution retirement has no session");
    return result.session;
  }
  async inspectPolicy(
    target: { deviceId: string; projectId?: string; sessionId?: string },
    policy?: Policy,
  ): Promise<PromptInspection> {
    const result = await this.run({
      id: commandID(),
      ...target,
      kind: "policy.inspect",
      policy,
    });
    return result.data as PromptInspection;
  }
  async savePolicy(
    session: Session,
    policy: Policy,
    expectedVersion = session.policyVersion,
  ): Promise<Session> {
    const result = await this.run({
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "policy.save",
      expectedPolicyVersion: expectedVersion,
      policy,
    });
    if (!result.session) throw new Error("Policy result has no session");
    return result.session;
  }
  async restorePolicy(
    session: Session,
    version: string,
    expectedVersion = session.policyVersion,
  ): Promise<Session> {
    const result = await this.run({
      id: commandID(),
      deviceId: session.deviceId,
      sessionId: session.id,
      kind: "policy.restore",
      requestId: version,
      expectedPolicyVersion: expectedVersion,
    });
    if (!result.session) throw new Error("Policy result has no session");
    return result.session;
  }
  respond(
    target: Pick<Session, "deviceId" | "id" | "turnId">,
    requestId: string,
    response: InteractionReply,
  ): Promise<CommandResult> {
    return this.run({
      id: commandID(),
      deviceId: target.deviceId,
      sessionId: target.id,
      expectedTurnId: target.turnId || undefined,
      kind: "respond",
      requestId,
      response,
    });
  }
  events(after = 0, sessionId?: string): Promise<Event[]> {
    const query = new URLSearchParams({ after: String(after) });
    if (sessionId) query.set("sessionId", sessionId);
    return this.request(`/v1/events?${query}`);
  }
  command(
    command: Omit<Command, "sourceId" | "createdAt">,
  ): Promise<CommandResult> {
    return this.request("/v1/commands", {
      method: "POST",
      body: JSON.stringify(command),
    });
  }
  result(id: string): Promise<CommandResult> {
    return this.request(`/v1/commands/${encodeURIComponent(id)}`);
  }

  /** Sends once and polls only the receipt. An observation timeout never resubmits a task. */
  async run(
    command: Omit<Command, "sourceId" | "createdAt">,
    timeoutMs = 55000,
  ): Promise<CommandResult> {
    let result: CommandResult;
    try {
      result = await this.command(command);
    } catch (error) {
      if (
        error instanceof MewDexError &&
        error.status < 500 &&
        error.status !== 408
      )
        throw error;
      throw new CommandOutcomeError(
        command.id,
        "unknown",
        "发送后未取得可靠回执，结果尚未确认；请按指令编号查询，不要重复发送。",
      );
    }
    const deadline = Date.now() + timeoutMs;
    while (result.state === "accepted" && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 250));
      try {
        result = await this.result(command.id);
      } catch (error) {
        if (error instanceof MewDexError && [401, 403].includes(error.status))
          throw error;
        throw new CommandOutcomeError(
          command.id,
          "unknown",
          "指令已发送，暂时无法查询结果；不会自动重发。",
        );
      }
    }
    if (result.state !== "completed")
      throw new CommandOutcomeError(
        command.id,
        result.state,
        result.error ?? "指令已发送，结果尚未确认；不会自动重发。",
      );
    return result;
  }
  upload(
    data: Blob,
    name: string,
    target: { deviceId: string; projectId: string },
    signal?: AbortSignal,
  ): Promise<Attachment> {
    return this.request(
      `/v1/attachments?${new URLSearchParams({ name, deviceId: target.deviceId, projectId: target.projectId })}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: data,
        signal,
      },
    );
  }
  attachment(id: string, signal?: AbortSignal): Promise<Attachment> {
    return this.request(`/v1/attachments/${encodeURIComponent(id)}`, {
      signal,
    });
  }
  async attachmentContent(id: string, signal?: AbortSignal): Promise<Blob> {
    return (
      await this.response(`/v1/attachments/${encodeURIComponent(id)}/content`, {
        signal,
      })
    ).blob();
  }
  invite(
    role: "client" | "executor" | "admin",
    access?: Access,
  ): Promise<{ code: string }> {
    return this.request("/v1/invitations", {
      method: "POST",
      body: JSON.stringify({ role, access }),
    });
  }

  identities(): Promise<Identity[]> {
    return this.request("/v1/identities");
  }
  updateAccess(id: string, access: Access): Promise<{ updated: boolean }> {
    return this.request(`/v1/identities/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(access),
    });
  }
  revoke(id: string): Promise<{ revoked: boolean }> {
    return this.request(`/v1/identities/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  /** Read requests may reconnect; user commands are never automatically replayed. */
  async subscribe(options: {
    after?: number;
    sessionId?: string;
    /** Optional exact event kinds; checkpoints still advance across skipped events. */
    kinds?: string[];
    signal: AbortSignal;
    onEvent: (event: Event) => void;
    onState?: (state: string) => void;
    onCursor?: (cursor: number) => void;
    /** A reconnect may replace missed history with a refreshed snapshot and cursor. */
    onReconnect?: () => Promise<number | undefined>;
  }): Promise<void> {
    const kinds = options.kinds && new Set(options.kinds);
    if (
      options.kinds &&
      (options.kinds.length === 0 ||
        options.kinds.length > 16 ||
        options.kinds.some(
          (kind) =>
            typeof kind !== "string" || !/^[A-Za-z0-9_./-]{1,128}$/.test(kind),
        ))
    )
      throw new Error("Invalid event kind filter");
    let cursor = options.after ?? 0;
    let delay = 500;
    let reconnecting = false;
    const { signal, onState, onEvent } = options;
    while (!signal.aborted) {
      try {
        onState?.("connecting");
        if (reconnecting && options.onReconnect) {
          const refreshed = await options.onReconnect();
          if (refreshed !== undefined) {
            if (!Number.isSafeInteger(refreshed) || refreshed < 0)
              throw new Error("Invalid refreshed event cursor");
            cursor = refreshed;
            options.onCursor?.(cursor);
          }
        }
        reconnecting = true;
        if (signal.aborted) break;
        const { ticket } = await this.request<{ ticket: string }>(
          "/v1/events/ticket",
          { method: "POST", signal },
        );
        const url = new URL(this.base + "/v1/events/ws");
        url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
        url.searchParams.set("ticket", ticket);
        url.searchParams.set("after", String(cursor));
        if (options.sessionId !== undefined)
          url.searchParams.set("sessionId", options.sessionId);
        kinds?.forEach((kind) => url.searchParams.append("kind", kind));
        await new Promise<void>((resolve, reject) => {
          const ws = this.socket(url.toString());
          const abort = () => {
            ws.close();
            resolve();
          };
          signal.addEventListener("abort", abort, { once: true });
          ws.onopen = () => {
            delay = 500;
            onState?.("connected");
            if (signal.aborted) abort();
          };
          ws.onmessage = (message) => {
            if (signal.aborted) return;
            try {
              const event = JSON.parse(String(message.data)) as Event;
              if (
                !Number.isSafeInteger(event.sequence) ||
                event.sequence <= cursor
              )
                return;
              if (event.kind === "stream.checkpoint") {
                cursor = event.sequence;
                options.onCursor?.(cursor);
                return;
              }
              // Older Hubs ignore optional query filters. Preserve the same
              // semantics locally, without losing their checkpoint progress.
              if (!kinds || kinds.has(event.kind)) onEvent(event);
              cursor = event.sequence;
              options.onCursor?.(cursor);
            } catch (error) {
              ws.close();
              reject(error);
            }
          };
          ws.onerror = () => ws.close();
          ws.onclose = () => {
            signal.removeEventListener("abort", abort);
            resolve();
          };
        });
      } catch (error) {
        if (signal.aborted) break;
        if (
          error instanceof MewDexError &&
          (error.status === 401 || error.status === 403)
        ) {
          onState?.("unauthorized");
          throw error;
        }
      }
      if (signal.aborted) break;
      onState?.("disconnected");
      await new Promise<void>((resolve) => {
        const abort = () => {
          clearTimeout(timer);
          resolve();
        };
        const timer = setTimeout(
          () => {
            signal.removeEventListener("abort", abort);
            resolve();
          },
          delay + Math.random() * 200,
        );
        signal.addEventListener("abort", abort, { once: true });
      });
      delay = Math.min(delay * 2, 15000);
    }
    onState?.("closed");
  }
}

export function commandID(): string {
  const provider = globalThis.crypto;
  if (typeof provider?.randomUUID === "function") return provider.randomUUID();
  if (typeof provider?.getRandomValues !== "function")
    throw new Error("Secure random generation is unavailable on this input");
  const bytes = provider.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (value) =>
    value.toString(16).padStart(2, "0"),
  ).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
