const byID = (id) => document.getElementById(id);
let snapshot;
let busy = false;
let refreshing = false;
let requestVersion = 0;
let token =
  location.hash.slice(1) ||
  sessionStorage.getItem("mewdex.executor-panel") ||
  "";
if (location.hash) history.replaceState(null, "", location.pathname);
const panel = await fetch("/api/panel").then((response) => response.json());
const isHub = panel.role === "hub";
if (isHub) {
  document.title = "MewDex Hub";
  byID("heading").textContent = "Hub 控制面板";
  byID("description").textContent =
    "管理 Hub 服务。GPT 账号与项目由 Hub 提供给输入端。";
  byID("start").textContent = "启动 Hub";
  byID("stop").textContent = "停止 Hub";
  byID("setup-heading").textContent = "创建 Hub";
}
byID("executor-setup").hidden = isHub;
byID("hub-setup").hidden = !isHub;
for (const field of byID("executor-setup").querySelectorAll("input"))
  field.required = !isHub;
for (const field of byID("hub-setup").querySelectorAll("input"))
  field.required = isHub;
let computer;
let grantIdentity = "";
let windowListIdentity = "";
byID("token").value = token;

async function request(operation, extra = {}) {
  const version = ++requestVersion;
  const response = await fetch("/api/device", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      operation,
      expectedDeviceId: snapshot?.deviceId,
      ...extra,
    }),
  });
  if (!response.headers.get("Content-Type")?.includes("application/json"))
    throw new Error(await response.text());
  const body = await response.json();
  if (!response.ok || !body.ok) throw new Error(body.error || "操作失败");
  if (version === requestVersion) {
    snapshot = body.result.snapshot;
    if (body.result.computer) computer = body.result.computer;
    render();
    renderComputer();
  }
  return body.result;
}

function renderComputer() {
  byID("computer-section").hidden = isHub || !snapshot?.ready;
  const value = computer?.status;
  const session = value?.session;
  const identity =
    session?.state === "pending" ? `${session.id}:${session.revision}` : "";
  if (identity !== grantIdentity) {
    grantIdentity = identity;
    windowListIdentity = "";
    const placeholder = document.createElement("option");
    placeholder.value = "";
    placeholder.textContent = "请选择本次任务要授权的窗口";
    byID("computer-window").replaceChildren(placeholder);
    byID("computer-consent").checked = false;
    byID("computer-window-hint").textContent = "";
  }
  const state =
    {
      pending: "等待本机选择窗口并授权",
      approved: "已授权，等待观察",
      ready:
        session?.lastActionState === "running"
          ? "正在操作窗口"
          : "窗口授权有效",
      stopping: "正在停止",
      stopped: "已停止",
      expired: "已到期",
      unknown: "结果尚未确认，需要核对并停止",
    }[session?.state] || "暂无请求，先在对话中描述电脑操作任务";
  const rows = [
    [
      "可用性",
      value?.supported ? "可操作本机软件窗口" : value?.reason || "等待刷新",
    ],
    ["控制状态", state],
  ];
  if (session) {
    rows.push(
      ["请求原因", session.reason],
      ["所属对话", session.ownerId],
      ["窗口", session.window?.title || "尚未选择"],
      ["到期时间", new Date(session.expiresAt).toLocaleString()],
    );
    if (session.lastActionState)
      rows.push(["最近操作", session.lastActionState]);
    if (session.error) rows.push(["详情", session.error]);
  }
  byID("computer-status").replaceChildren(
    ...rows.flatMap(([name, value]) => {
      const term = document.createElement("dt");
      term.textContent = name;
      const detail = document.createElement("dd");
      detail.textContent = value;
      return [term, detail];
    }),
  );
  byID("computer-grant").hidden =
    !value?.supported || session?.state !== "pending";
  byID("computer-stop").disabled =
    busy ||
    !session ||
    ["stopped", "expired", "stopping"].includes(session.state);
  byID("computer-stop").textContent =
    session?.state === "pending" ? "拒绝本次请求" : "停止并收回授权";
  byID("computer-approve").disabled =
    busy ||
    !value?.supported ||
    session?.state !== "pending" ||
    windowListIdentity !== grantIdentity;
}

async function refreshComputer() {
  if (!isHub && snapshot?.ready) {
    await request("computer.status");
    if (
      computer?.status.supported &&
      grantIdentity &&
      windowListIdentity !== grantIdentity
    )
      await loadComputerWindows();
  }
}

async function loadComputerWindows() {
  const identity = grantIdentity;
  if (!identity) return;
  const result = await request("computer.windows");
  if (identity !== grantIdentity) return;
  const placeholder = document.createElement("option");
  placeholder.value = "";
  placeholder.textContent = "请选择本次任务要授权的窗口";
  const options = (result.computer?.windows || []).map((window) => {
    const option = document.createElement("option");
    option.value = window.id;
    option.textContent = `${window.title} · PID ${window.processId}`;
    return option;
  });
  byID("computer-window").replaceChildren(placeholder, ...options);
  byID("computer-consent").checked = false;
  byID("computer-window-hint").textContent = options.length
    ? "只选择本次任务需要的窗口。刷新列表会清除原选择。"
    : "没有可授权的窗口，请先打开目标软件并保持窗口可见，再刷新列表。";
  windowListIdentity = identity;
  renderComputer();
}

byID("computer-refresh").addEventListener(
  "click",
  () => void perform(refreshComputer),
);
byID("computer-windows").addEventListener(
  "click",
  () => void perform(loadComputerWindows),
);
byID("computer-grant").addEventListener("submit", (event) => {
  event.preventDefault();
  const session = computer?.status.session;
  if (
    !session ||
    session.state !== "pending" ||
    !byID("computer-consent").checked ||
    !windowListIdentity ||
    windowListIdentity !== `${session.id}:${session.revision}`
  )
    return;
  void perform(() =>
    request("computer.approve", {
      computer: {
        sessionId: session.id,
        expectedRevision: session.revision,
        windowId: byID("computer-window").value,
        durationSeconds: Number(byID("computer-duration").value),
      },
    }),
  );
});
byID("computer-stop").addEventListener("click", () => {
  const session = computer?.status.session;
  if (session)
    void perform(() =>
      request("computer.stop", { computer: { sessionId: session.id } }),
    );
});

function render() {
  byID("device").hidden = false;
  byID("setup-section").hidden = snapshot.configured;
  byID("projects-section").hidden = isHub || !snapshot.configured;
  byID("pairing-section").hidden = isHub || !snapshot.configured;
  const connection = snapshot.hubConnection;
  let connectionLabel = snapshot.ready
    ? "等待连接 Hub"
    : snapshot.state === "stopped"
      ? "执行服务已停止"
      : "等待执行服务就绪";
  if (snapshot.ready && connection) {
    connectionLabel =
      {
        connected: "已连接",
        connecting: "正在连接",
        reconnecting: "连接中断，正在自动重连",
        disconnected: "连接中断，正在自动重连",
        "pairing-required": "配对凭据失效，请停止执行端后重新配对",
      }[connection.state] || connection.state;
    if (connection.retryAt && connection.state !== "connected") {
      connectionLabel += ` · 下次尝试 ${new Date(connection.retryAt).toLocaleTimeString()}`;
    }
  }
  const values = [
    ["设备", snapshot.name || "尚未配置"],
    ["状态", snapshot.state],
    ["Hub", isHub ? "本机 Hub" : snapshot.hubUrl || "尚未连接"],
    [
      "连接状态",
      isHub ? (snapshot.ready ? "Hub 已就绪" : "等待启动") : connectionLabel,
    ],
    ["设备编号", snapshot.deviceId || "—"],
    [
      "连接与服务",
      snapshot.message || (snapshot.ready ? "执行服务已就绪" : "等待启动"),
    ],
  ];
  if (snapshot.ready && connection?.error) {
    values.push(["连接详情", connection.error]);
  }
  byID("status").replaceChildren(
    ...values.flatMap(([name, value]) => {
      const term = document.createElement("dt");
      term.textContent = name;
      const detail = document.createElement("dd");
      detail.textContent = value;
      return [term, detail];
    }),
  );
  byID("start").disabled =
    !snapshot.configured ||
    snapshot.ready ||
    !["stopped", "starting"].includes(snapshot.state);
  byID("stop").disabled = !snapshot.instanceId || snapshot.state === "stopped";
}

async function perform(action) {
  busy = true;
  byID("feedback").className = "";
  byID("feedback").textContent = "正在处理…";
  document.querySelectorAll("button").forEach((button) => {
    button.disabled = true;
  });
  try {
    await action();
    byID("feedback").textContent = "已完成";
  } catch (error) {
    byID("feedback").textContent = error.message;
    byID("feedback").className = "error";
  } finally {
    busy = false;
    document.querySelectorAll("button").forEach((button) => {
      button.disabled = false;
    });
    if (snapshot) render();
    renderComputer();
  }
}

byID("login").addEventListener("submit", (event) => {
  event.preventDefault();
  token = byID("token").value.trim();
  void perform(async () => {
    await request("status");
    await refreshComputer();
    sessionStorage.setItem("mewdex.executor-panel", token);
    byID("paths").value = (snapshot.projects || [])
      .map((project) => project.path)
      .join("\n");
  });
});
byID("refresh").addEventListener(
  "click",
  () => void perform(() => request("status")),
);
byID("start").addEventListener(
  "click",
  () => void perform(() => request("start")),
);
byID("stop").addEventListener("click", () => {
  if (confirm("停止服务将中断此组件进行中的任务。是否继续？"))
    void perform(() =>
      request("stop", { expectedInstanceId: snapshot.instanceId }),
    );
});
byID("setup").addEventListener("submit", (event) => {
  event.preventDefault();
  void perform(async () => {
    await request("setup", {
      setup: {
        name: byID("name").value.trim(),
        hub: isHub,
        executor: !isHub,
        gateway: false,
        hubUrl: isHub ? "" : byID("hub").value.trim(),
        invitation: isHub ? "" : byID("invitation").value.trim(),
        account: isHub
          ? {
              username: byID("account").value.trim(),
              password: byID("password").value,
            }
          : undefined,
        projects: [],
      },
    });
    byID("invitation").value = "";
    byID("password").value = "";
  });
});
byID("projects").addEventListener("submit", (event) => {
  event.preventDefault();
  const projects = [
    ...new Set(
      byID("paths")
        .value.split(/\r?\n/)
        .map((path) => path.trim())
        .filter(Boolean),
    ),
  ].map((path) => {
    const existing = snapshot.projects?.find(
      (project) => project.path === path,
    );
    return (
      existing || {
        id: "",
        name: path.split(/[\\/]/).filter(Boolean).at(-1) || path,
        path,
      }
    );
  });
  void perform(() => request("projects", { projects }));
});
byID("pairing").addEventListener("submit", (event) => {
  event.preventDefault();
  void perform(async () => {
    await request("repair.executor", {
      pairing: { invitation: byID("new-invitation").value.trim() },
    });
    byID("new-invitation").value = "";
  });
});
if (token) byID("login").requestSubmit();
setInterval(() => {
  if (
    token &&
    snapshot &&
    !busy &&
    !refreshing &&
    document.visibilityState !== "hidden"
  ) {
    refreshing = true;
    void request("status")
      .then(refreshComputer)
      .catch((error) => {
        if (!busy) {
          byID("feedback").textContent = error.message;
          byID("feedback").className = "error";
        }
      })
      .finally(() => {
        refreshing = false;
      });
  }
}, 5000);
