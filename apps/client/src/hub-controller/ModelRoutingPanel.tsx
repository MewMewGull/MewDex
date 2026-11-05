import { t } from "../localization.ts";
import { useEffect, useState } from "react";
import type { ModelRoutingSettings, Result, Snapshot } from "./localProtocol";
import { useSettingAutosave } from "../useSettingAutosave";

function validProxy(value = "") {
  try {
    const url = new URL(value.trim());
    return (
      ["http:", "https:"].includes(url.protocol) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      url.pathname === "/"
    );
  } catch {
    return false;
  }
}

export function ModelRoutingPanel({
  snapshot,
  busy,
  onChange,
  onPendingChange,
}: {
  snapshot: Snapshot;
  busy: boolean;
  onChange: (value: ModelRoutingSettings) => Promise<Result>;
  onPendingChange: (pending: boolean) => void;
}) {
  const current = snapshot.modelRouting ?? {
    route: snapshot.hub ? "local" : "hub",
  };
  const [draft, setDraft] = useState(current);
  const { queue, state } = useSettingAutosave(
    current,
    onChange,
    onPendingChange,
  );
  const currentKey = JSON.stringify(current);
  useEffect(() => {
    const value = JSON.parse(currentKey) as ModelRoutingSettings;
    if (queue.adopt(value)) setDraft(value);
  }, [currentKey, queue]);
  const supported =
    !!snapshot.modelRouting &&
    snapshot.operations?.includes("model-routing.apply");
  const stopped = snapshot.state === "stopped";
  function edit(patch: Partial<ModelRoutingSettings>) {
    const next = { ...draft, ...patch };
    if (next.route !== "proxy") delete next.proxyUrl;
    setDraft(next);
    queue.change(next, next.route !== "proxy" || validProxy(next.proxyUrl));
  }
  return (
    <section
      className="device-data model-routing-settings"
      aria-label={t("Hub 的 OpenAI 连接")}
    >
      <h3>{t("Hub 的 OpenAI 连接")}</h3>
      <p className="muted">
        {t(
          "Hub 统一登录 ChatGPT 并访问 OpenAI，输入端和执行端通过连接 Hub 参与同一项目。",
        )}
      </p>
      {snapshot.hub ? (
        <>
          {!stopped && (
            <p className="muted">{t("先停止 Hub 再修改，重新启动后生效。")}</p>
          )}
          <fieldset disabled={busy || !stopped || !supported}>
            <label>
              {t("连接方式")}
              <select
                value={draft.route}
                onChange={(event) => edit({ route: event.target.value })}
              >
                <option value="local">{t("Hub 本机网络")}</option>
                <option value="proxy">{t("Hub 可访问的 HTTP 代理")}</option>
                {draft.route === "custom" && (
                  <option value="custom">{t("已有认证代理")}</option>
                )}
              </select>
            </label>
            {draft.route === "proxy" && (
              <label>
                {t("HTTP / HTTPS 代理地址")}
                <input
                  type="url"
                  value={draft.proxyUrl ?? ""}
                  placeholder="http://127.0.0.1:7890"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => edit({ proxyUrl: event.target.value })}
                  onBlur={() => void queue.flush()}
                />
              </label>
            )}
            {draft.route === "custom" && (
              <p className="muted">{t("认证代理的凭据在 Hub 配置中管理。")}</p>
            )}
          </fieldset>
          {state.error && (
            <p className="error" role="alert">
              {state.error}
            </p>
          )}
          <p className="muted" role="status">
            {state.pending
              ? t("正在自动保存…")
              : state.phase === "invalid"
                ? t("请填写有效的 HTTP / HTTPS 代理地址。")
                : t("修改后自动保存，启动 Hub 后生效。")}
          </p>
        </>
      ) : (
        <p className="muted">{t("此机器通过已配对的 Hub 接收执行指令。")}</p>
      )}
    </section>
  );
}
