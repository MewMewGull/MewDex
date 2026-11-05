const byID = (id) => document.getElementById(id);
let snapshot;
let token = sessionStorage.getItem("mewdex.executor-panel") || "";
byID("token").value = token;

async function request(operation, extra = {}) {
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
  snapshot = body.result.snapshot;
  render();
  return body.result;
}

function render() {
  byID("device").hidden = false;
  byID("setup-section").hidden = snapshot.configured;
  byID("projects-section").hidden = !snapshot.configured;
  byID("pairing-section").hidden = !snapshot.configured;
  const values = [
    ["设备", snapshot.name || "尚未配置"],
    ["状态", snapshot.state],
    ["Hub", snapshot.hubUrl || "尚未连接"],
    ["设备编号", snapshot.deviceId || "—"],
    [
      "连接与服务",
      snapshot.message || (snapshot.ready ? "执行服务已就绪" : "等待启动"),
    ],
  ];
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
    document.querySelectorAll("button").forEach((button) => {
      button.disabled = false;
    });
    if (snapshot) render();
  }
}

byID("login").addEventListener("submit", (event) => {
  event.preventDefault();
  token = byID("token").value.trim();
  void perform(async () => {
    await request("status");
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
byID("stop").addEventListener(
  "click",
  () =>
    void perform(() =>
      request("stop", { expectedInstanceId: snapshot.instanceId }),
    ),
);
byID("setup").addEventListener("submit", (event) => {
  event.preventDefault();
  void perform(async () => {
    await request("setup", {
      setup: {
        name: byID("name").value.trim(),
        hub: false,
        executor: true,
        gateway: false,
        hubUrl: byID("hub").value.trim(),
        invitation: byID("invitation").value.trim(),
        projects: [],
      },
    });
    byID("invitation").value = "";
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
