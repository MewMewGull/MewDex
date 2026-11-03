import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Monitor } from "lucide-react";
import { MewDexClient, MewDexError, type ServerInfo } from "@mewdex/sdk";
import type { Connection } from "../platform";
import { readConnection } from "../connectionRead";
import { Brand } from "./Brand";

export function ConnectScreen({
  onConnect,
  onLocalDevice,
  initialURL,
  joining,
  startupError,
  onRetryStartup,
}: {
  onConnect: (connection: Connection) => Promise<void>;
  onLocalDevice?: () => void;
  initialURL?: string;
  joining: boolean;
  onJoiningChange: (value: boolean) => void;
  startupError?: string;
  onRetryStartup?: () => void;
}) {
  const [url, setURL] = useState(initialURL ?? "");
  const [server, setServer] = useState<ServerInfo | null>(null);
  const [mode, setMode] = useState("password");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [paired, setPaired] = useState<Connection | null>(null);
  const request = useRef<AbortController | null>(null);
  const userInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (initialURL) {
      request.current?.abort();
      const controller = new AbortController();
      request.current = controller;
      setURL(initialURL);
      setServer(null);
      setPassword("");
      setKey("");
      setPaired(null);
      setError("");
      if (!joining) {
        setBusy(false);
        return () => controller.abort();
      }
      setBusy(true);
      void readConnection(
        (signal) =>
          new MewDexClient({ baseURL: initialURL }).checkServer(signal),
        controller.signal,
      )
        .then((info) => {
          if (controller.signal.aborted) return;
          setMode(info.authentication.defaultMethod);
          setServer(info);
        })
        .catch((cause) => {
          if (!controller.signal.aborted)
            setError(cause instanceof Error ? cause.message : String(cause));
        })
        .finally(() => {
          if (request.current === controller) setBusy(false);
        });
      return () => controller.abort();
    }
  }, [initialURL, joining]);
  useEffect(() => () => request.current?.abort(), []);
  useEffect(() => {
    if (server) userInput.current?.focus();
  }, [server, mode]);
  function reset() {
    request.current?.abort();
    setServer(null);
    setPaired(null);
    setPassword("");
    setKey("");
    setError("");
  }
  async function submit() {
    if (busy) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError("");
    try {
      if (!server) {
        const info = await readConnection(
          (signal) =>
            new MewDexClient({ baseURL: url.trim() }).checkServer(signal),
          controller.signal,
        );
        if (controller.signal.aborted) return;
        setURL(url.trim());
        setMode(info.authentication.defaultMethod);
        setServer(info);
        return;
      }
      const client = new MewDexClient({
        baseURL: url.trim(),
        token: mode === "key" ? key.trim() : undefined,
      });
      const value = paired ?? {
        baseURL: url.trim(),
        credentials:
          mode === "password"
            ? await readConnection(
                (signal) => client.login(username.trim(), password, signal),
                controller.signal,
              )
            : mode === "key"
              ? await readConnection(
                  (signal) => client.loginWithKey(signal),
                  controller.signal,
                )
              : await client.pair(key.trim(), "我的输入端"),
      };
      if (controller.signal.aborted) return;
      setPaired(value);
      setPassword("");
      setKey("");
      await onConnect(value);
    } catch (cause) {
      if (controller.signal.aborted) return;
      setError(
        cause instanceof MewDexError && [401, 403].includes(cause.status)
          ? mode === "password"
            ? "MewDex 用户名或密码不正确。"
            : "登录密钥或配对码无效，或已被撤销。"
          : cause instanceof Error
            ? cause.message
            : String(cause),
      );
    } finally {
      if (request.current === controller) setBusy(false);
    }
  }
  const labels: Record<string, string> = {
    password: "账号密码登录",
    key: "密钥登录",
    pair: "配对码连接",
  };
  return (
    <div className="connect-screen">
      <header className="connect-header">
        <Brand />
      </header>
      <main className="connect-card connection-onboarding">
        {server && (
          <button className="connection-back" disabled={busy} onClick={reset}>
            <ArrowLeft size={15} />
            更改中央后端
          </button>
        )}
        <h1>{server ? "登录 MewDex" : "连接中央后端"}</h1>
        <p>
          {server
            ? "使用此中央后端的 MewDex 账号，管理你的执行设备与项目。"
            : "先检查中央后端是否可用，并获取它支持的登录方式。"}
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          {startupError && (
            <div className="error" role="alert">
              <p>{startupError}</p>
              <button type="button" onClick={onRetryStartup}>
                重新启动中央后端
              </button>
            </div>
          )}
          {!server ? (
            <label>
              中央后端地址
              <input
                type="url"
                value={url}
                disabled={busy}
                onChange={(event) => {
                  setURL(event.target.value);
                  setError("");
                }}
                placeholder="https://mewdex.example.com"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                required
              />
            </label>
          ) : (
            <>
              <p className="connection-server-address">已连接：{url}</p>
              {server.authentication.methods.length > 1 && (
                <label>
                  登录方式
                  <select
                    value={mode}
                    disabled={busy || !!paired}
                    onChange={(event) => {
                      setMode(event.target.value);
                      setPassword("");
                      setKey("");
                      setError("");
                    }}
                  >
                    {server.authentication.methods.map((method) => (
                      <option key={method} value={method}>
                        {labels[method]}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {server.authentication.accountConfigured === false &&
                server.capabilities?.includes("mewdex-accounts") && (
                  <p className="muted">
                    此中央后端尚未设置 MewDex
                    账号。先使用已有管理员密钥或管理员配对码登录，即可在账号设置中创建用户名和密码。
                  </p>
                )}
              {mode === "password" ? (
                <>
                  <label>
                    MewDex 用户名
                    <input
                      ref={userInput}
                      value={username}
                      autoComplete="username"
                      autoCapitalize="none"
                      spellCheck={false}
                      disabled={busy || !!paired}
                      onChange={(event) => {
                        setUsername(event.target.value);
                        setError("");
                      }}
                      required={!paired}
                    />
                  </label>
                  <label>
                    MewDex 密码
                    <input
                      type="password"
                      value={password}
                      autoComplete="current-password"
                      disabled={busy || !!paired}
                      onChange={(event) => {
                        setPassword(event.target.value);
                        setError("");
                      }}
                      required={!paired}
                    />
                  </label>
                </>
              ) : (
                <label>
                  {mode === "key" ? "登录密钥" : "输入端配对码"}
                  <input
                    ref={userInput}
                    type="password"
                    value={key}
                    autoComplete="off"
                    autoCapitalize="none"
                    spellCheck={false}
                    disabled={busy || !!paired}
                    onChange={(event) => {
                      setKey(event.target.value);
                      setError("");
                    }}
                    required={!paired}
                  />
                </label>
              )}
              <p className="muted">节点的 ChatGPT 订阅在登录后单独管理。</p>
            </>
          )}
          {error && (
            <div className="error" role="alert">
              {error}
            </div>
          )}
          <button className="primary connect-button" disabled={busy}>
            {busy
              ? server
                ? "正在登录…"
                : "正在检查…"
              : !server
                ? "检查并继续"
                : paired
                  ? "保存登录"
                  : "登录 MewDex"}
            <ArrowRight size={17} />
          </button>
          {paired && (
            <button
              type="button"
              className="connection-reset"
              disabled={busy}
              onClick={() => {
                setPaired(null);
                setPassword("");
                setKey("");
                setError("");
              }}
            >
              使用其他登录信息
            </button>
          )}
        </form>
        {!server && onLocalDevice && (
          <button
            className="connection-back"
            disabled={busy}
            onClick={onLocalDevice}
          >
            <Monitor size={15} />
            在这台电脑开始
          </button>
        )}
      </main>
    </div>
  );
}
