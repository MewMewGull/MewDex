import { t } from "../localization.ts";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { MewDexClient } from "@mewdex/sdk";
import { CopyButton } from "../components/CopyButton";

export function MewDexAccountSettings({
  client,
  supported,
  baseURL,
  loginKey,
  onLogout,
}: {
  client: MewDexClient;
  supported: boolean;
  baseURL: string;
  loginKey: string;
  onLogout: () => void;
}) {
  const cache = useQueryClient();
  const account = useQuery({
    queryKey: ["mewdex-account"],
    queryFn: () => client.mewdexAccount(),
    enabled: supported,
    retry: false,
  });
  const [editing, setEditing] = useState(false);
  const [username, setUsername] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  async function save() {
    if (busy) return;
    if (password !== confirm) {
      setError(t("两次输入的 MewDex 密码不一致。"));
      return;
    }
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      const result = await client.configureMewDexAccount(
        username,
        name,
        password,
      );
      cache.setQueryData(["mewdex-account"], result);
      void cache.invalidateQueries({ queryKey: ["identity"] });
      setPassword("");
      setConfirm("");
      setEditing(false);
      setSaved(true);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : t("无法保存 MewDex 账号"),
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="network-section">
      <h3>{t("MewDex 账号")}</h3>
      <p className="muted">
        {t(
          "登录中央后端后，可统一管理所有执行节点和项目。各节点的 ChatGPT 订阅独立管理。",
        )}
      </p>
      <dl className="connection-details">
        <div>
          <dt>{t("中央后端")}</dt>
          <dd>{baseURL}</dd>
        </div>
        {account.data && (
          <>
            <div>
              <dt>{t("用户名")}</dt>
              <dd>{account.data.username}</dd>
            </div>
            <div>
              <dt>{t("账号名称")}</dt>
              <dd>{account.data.name}</dd>
            </div>
          </>
        )}
      </dl>
      {!supported ? (
        <p className="muted">
          {t(
            "此中央后端尚未提供账号密码登录，请更新中央后端；原密钥仍可使用。",
          )}
        </p>
      ) : account.isPending ? (
        <p>{t("正在读取 MewDex 账号…")}</p>
      ) : account.error ? (
        <p className="error">
          {t("无法读取 MewDex 账号。")}
          <button onClick={() => void account.refetch()}>{t("重试")}</button>
        </p>
      ) : !editing ? (
        <button
          onClick={() => {
            setUsername(account.data?.username ?? "");
            setName(account.data?.name ?? "");
            setEditing(true);
            setSaved(false);
          }}
        >
          {account.data
            ? t("修改 MewDex 账号与密码")
            : t("设置 MewDex 用户名和密码")}
        </button>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <label>
            {t("MewDex 用户名")}
            <input
              value={username}
              autoComplete="username"
              maxLength={64}
              required
              disabled={busy}
              onChange={(event) => setUsername(event.target.value)}
            />
          </label>
          <label>
            {t("账号名称")}
            <input
              value={name}
              maxLength={80}
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            {t("新的 MewDex 密码")}
            <input
              type="password"
              value={password}
              autoComplete="new-password"
              minLength={8}
              required
              disabled={busy}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          <label>
            {t("确认 MewDex 密码")}
            <input
              type="password"
              value={confirm}
              autoComplete="new-password"
              required
              disabled={busy}
              onChange={(event) => setConfirm(event.target.value)}
            />
          </label>
          <p className="muted">{t("密码至少 8 个字符；现有密钥保持可用。")}</p>
          <button className="primary" disabled={busy}>
            {busy ? t("正在保存…") : t("保存 MewDex 账号")}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              setEditing(false);
              setPassword("");
              setConfirm("");
              setError("");
            }}
          >
            {t("取消")}
          </button>
        </form>
      )}
      {saved && (
        <p role="status">
          {t("MewDex 账号已保存，可在其他终端使用此用户名和密码登录。")}
        </p>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <details className="connection-advanced">
        <summary>{t("密钥登录")}</summary>
        <p className="muted">
          {t(
            "可保存此密钥，在其他终端选择密钥登录。密钥拥有当前 MewDex 账号的管理权限。",
          )}
        </p>
        <CopyButton text={loginKey} label={t("复制 MewDex 登录密钥")} />
      </details>
      <button
        className="settings-disconnect"
        disabled={busy}
        onClick={onLogout}
      >
        {t("退出 MewDex")}
      </button>
      <p className="muted">
        {t("退出会清除当前网络的本机草稿，不退出执行节点的 ChatGPT 账号。")}
      </p>
    </section>
  );
}
