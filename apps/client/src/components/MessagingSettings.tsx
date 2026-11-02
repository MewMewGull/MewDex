import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageCircle, RefreshCw } from "lucide-react";
import QRCode from "qrcode";
import {
  type Device,
  type MessagingChannel,
  type MessagingLogin,
  type MessagingUpdate,
  MewDexClient,
} from "@mewdex/sdk";
import { CopyButton } from "./CopyButton";
import "../messaging.css";

const names: Record<string, string> = {
  weixin: "微信",
  qq: "QQ",
  feishu: "飞书",
};
const states: Record<string, string> = {
  disabled: "已关闭",
  needs_credentials: "等待连接账号",
  needs_authorization: "等待重新授权",
  connecting: "正在连接",
  connected: "已连接",
  retrying: "正在重连",
  delivery_error: "回复送达异常",
  storage_error: "消息保存异常",
};
const splitIDs = (text: string) => [
  ...new Set(
    text
      .split(/[\n,，;；]+/)
      .map((id) => id.trim())
      .filter(Boolean),
  ),
];

type Draft = {
  enabled: boolean;
  deviceId: string;
  projectId: string;
  appId: string;
  botUserId: string;
  users: string;
  chats: string;
  secret: string;
  forgetSecret: boolean;
};
const draftFor = (channel: MessagingChannel): Draft => ({
  enabled: channel.enabled,
  deviceId: channel.deviceId,
  projectId: channel.projectId,
  appId: channel.appId,
  botUserId: channel.botUserId,
  users: channel.allowedUsers.join("\n"),
  chats: channel.allowedChats.join("\n"),
  secret: "",
  forgetSecret: false,
});

function ChannelEditor({
  channel,
  client,
  devices,
  onSaved,
}: {
  channel: MessagingChannel;
  client: MewDexClient;
  devices: Device[];
  onSaved: (channels: MessagingChannel[]) => void;
}) {
  const [draft, setDraft] = useState(() => draftFor(channel));
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const [login, setLogin] = useState<MessagingLogin | null>(null);
  const [qrImage, setQRImage] = useState("");
  const [loggingIn, setLoggingIn] = useState(false);
  const weixin = channel.platform === "weixin";
  const feishu = channel.platform === "feishu";
  const executors = devices.filter((device) =>
    device.roles.includes("executor"),
  );
  const selectedDevice = executors.find(
    (device) => device.id === draft.deviceId,
  );
  const configuration = JSON.stringify(draftFor(channel));

  useEffect(() => {
    if (!dirty) setDraft(JSON.parse(configuration) as Draft);
  }, [configuration, dirty]);

  function change<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
    setDirty(true);
    setSaved(false);
  }

  useEffect(() => {
    if (!login || ["confirmed", "expired"].includes(login.state)) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      try {
        const result = await client.pollWeixinLogin(controller.signal);
        if (controller.signal.aborted) return;
        setLogin(result);
        if (result.state === "confirmed") {
          const channels = await client.messagingChannels(controller.signal);
          if (controller.signal.aborted) return;
          onSaved(channels);
          const connected = channels.find((item) => item.platform === "weixin");
          if (connected)
            setDraft((current) => ({
              ...current,
              users: current.users || connected.allowedUsers.join("\n"),
            }));
          setLoggingIn(false);
          setQRImage("");
          return;
        }
        if (result.state === "expired") {
          setLoggingIn(false);
          return;
        }
        timer = setTimeout(() => void poll(), 1500);
      } catch (value) {
        if (!controller.signal.aborted) {
          setError((value as Error).message);
          setLoggingIn(false);
        }
      }
    }
    timer = setTimeout(() => void poll(), 1000);
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [login?.url, client, onSaved]);

  async function beginLogin() {
    setError("");
    setLoggingIn(true);
    setQRImage("");
    setLogin(null);
    try {
      const result = await client.beginWeixinLogin();
      const image = await QRCode.toDataURL(result.url, {
        width: 240,
        margin: 3,
        errorCorrectionLevel: "M",
      });
      setQRImage(image);
      setLogin(result);
    } catch (value) {
      setError((value as Error).message);
      setLoggingIn(false);
    }
  }

  async function save() {
    setSaving(true);
    setError("");
    setSaved(false);
    try {
      const update: MessagingUpdate = {
        enabled: draft.enabled,
        deviceId: draft.deviceId,
        projectId: draft.projectId,
        appId: draft.appId.trim(),
        botUserId: draft.botUserId.trim(),
        allowedUsers: splitIDs(draft.users),
        allowedChats: splitIDs(draft.chats),
        ...(draft.forgetSecret
          ? { secret: "" }
          : draft.secret.trim()
            ? { secret: draft.secret.trim() }
            : {}),
      };
      const channels = await client.updateMessagingChannel(
        channel.platform,
        update,
      );
      onSaved(channels);
      const next = channels.find((item) => item.platform === channel.platform);
      if (next) setDraft(draftFor(next));
      setDirty(false);
      setSaved(true);
    } catch (value) {
      setError((value as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="network-section messaging-channel">
      <div className="messaging-heading">
        <h3>{names[channel.platform]}消息</h3>
        <span className={`messaging-status messaging-status-${channel.state}`}>
          {states[channel.state] ?? "等待连接"}
        </span>
      </div>
      <p className="muted" role="status">
        {channel.detail}
      </p>
      <fieldset disabled={saving}>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={draft.enabled}
            onChange={(event) => change("enabled", event.target.checked)}
          />
          启用{names[channel.platform]}消息
        </label>
        <div className="messaging-targets">
          <label>
            执行设备
            <select
              value={draft.deviceId}
              onChange={(event) => {
                change("deviceId", event.target.value);
                change("projectId", "");
              }}
            >
              <option value="">选择设备</option>
              {executors.map((device) => (
                <option key={device.id} value={device.id}>
                  {device.name}
                  {device.online ? "" : "（离线）"}
                </option>
              ))}
            </select>
          </label>
          <label>
            工作项目
            <select
              value={draft.projectId}
              onChange={(event) => change("projectId", event.target.value)}
            >
              <option value="">选择项目</option>
              {selectedDevice?.projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          </label>
        </div>

        {weixin ? (
          <div className="messaging-login">
            <p>
              使用微信扫描二维码连接 iLink
              机器人。允许用户列表为空时，会自动加入扫码账号。
            </p>
            {channel.accountId && (
              <p className="messaging-id">
                已连接账号：<code>{channel.accountId}</code>
              </p>
            )}
            <button
              type="button"
              onClick={() => void beginLogin()}
              disabled={loggingIn}
            >
              {loggingIn
                ? "等待微信扫码…"
                : channel.credentialConfigured
                  ? "重新扫码连接"
                  : "生成微信登录二维码"}
            </button>
            {qrImage && login?.state !== "confirmed" && (
              <div className="messaging-qr">
                <img
                  src={qrImage}
                  alt="用微信扫描此二维码连接 MewDex"
                  width={240}
                  height={240}
                />
                <p role="status">
                  {login?.state === "expired"
                    ? "二维码已过期，请重新生成。"
                    : ["scaned", "scaned_but_redirect"].includes(
                          login?.state ?? "",
                        )
                      ? "已扫码，请在微信中确认。"
                      : "打开微信扫一扫，确认连接。"}
                </p>
              </div>
            )}
            {login?.state === "confirmed" && (
              <p role="status">微信账号已连接。保存渠道设置后即可使用。</p>
            )}
          </div>
        ) : (
          <>
            <p className="muted">
              {feishu
                ? "在飞书开放平台创建企业自建应用、开启机器人能力，并使用长连接订阅接收消息事件。"
                : "在 QQ 开放平台创建机器人，开启单聊和群聊 @消息权限；测试账号需要加入平台沙箱。"}
            </p>
            <label>
              App ID
              <input
                value={draft.appId}
                autoComplete="off"
                spellCheck={false}
                onChange={(event) => change("appId", event.target.value)}
              />
            </label>
            <label>
              App Secret
              <input
                type="password"
                value={draft.secret}
                autoComplete="new-password"
                placeholder={
                  channel.credentialConfigured
                    ? "已保存；留空保留原密钥"
                    : "输入应用密钥"
                }
                onChange={(event) => change("secret", event.target.value)}
              />
            </label>
            {feishu && (
              <label>
                机器人 Open ID（可选）
                <input
                  value={draft.botUserId}
                  placeholder="留空自动识别"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => change("botUserId", event.target.value)}
                />
              </label>
            )}
          </>
        )}

        <label>
          允许访问的用户 ID
          <textarea
            value={draft.users}
            rows={3}
            placeholder={feishu ? "ou_…，每行一个" : "平台用户 ID，每行一个"}
            spellCheck={false}
            onChange={(event) => change("users", event.target.value)}
          />
        </label>
        <p className="muted">
          留空时不处理消息。连接后先向机器人发一条消息，可在下方找到你的平台用户
          ID。
        </p>
        {!weixin && (
          <label>
            允许访问的群聊 ID
            <textarea
              value={draft.chats}
              rows={2}
              placeholder="留空时不处理群聊；每行一个 ID"
              spellCheck={false}
              onChange={(event) => change("chats", event.target.value)}
            />
          </label>
        )}
        {channel.lastSenderId && (
          <div className="messaging-discovery">
            <p>
              最近收到的用户：<code>{channel.lastSenderId}</code>{" "}
              <CopyButton text={channel.lastSenderId} label="复制用户 ID" />
            </p>
            <button
              type="button"
              onClick={() =>
                change(
                  "users",
                  [
                    ...new Set([
                      ...splitIDs(draft.users),
                      channel.lastSenderId,
                    ]),
                  ].join("\n"),
                )
              }
            >
              加入允许用户
            </button>
            {channel.lastChatId &&
              channel.lastChatId !== channel.lastSenderId && (
                <p>
                  所在聊天：<code>{channel.lastChatId}</code>{" "}
                  <CopyButton text={channel.lastChatId} label="复制聊天 ID" />
                </p>
              )}
          </div>
        )}
        {channel.credentialConfigured && (
          <label className="checkbox">
            <input
              type="checkbox"
              checked={draft.forgetSecret}
              onChange={(event) => change("forgetSecret", event.target.checked)}
            />
            清除已保存的{weixin ? "微信登录凭据" : "应用密钥"}
          </label>
        )}
        <div className="messaging-actions">
          <button
            className="primary"
            type="button"
            disabled={!dirty || saving}
            onClick={() => void save()}
          >
            {saving ? "正在保存…" : "保存渠道设置"}
          </button>
          {saved && <span role="status">已保存</span>}
          {dirty && <span className="muted">有未保存更改</span>}
        </div>
      </fieldset>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <p className="muted">
        发送文字开始任务；/help 查看指令，/status 查看进度，/new 新建对话，/stop
        停止任务。群聊只响应
        @机器人，并为每位用户保留独立会话。图片、文件和语音附件请在 MewDex
        中处理。
      </p>
    </section>
  );
}

export function MessagingSettings({
  client,
  devices,
}: {
  client: MewDexClient;
  devices: Device[];
}) {
  const cache = useQueryClient();
  const [platform, setPlatform] = useState("weixin");
  const channels = useQuery({
    queryKey: ["messaging-channels", client],
    queryFn: ({ signal }) => client.messagingChannels(signal),
    refetchInterval: 4000,
    retry: false,
  });
  // Keep the login polling effect stable while channel status refreshes.
  const onSaved = useCallback(
    (value: MessagingChannel[]) => {
      cache.setQueryData(["messaging-channels", client], value);
    },
    [cache, client],
  );
  const selected = channels.data?.find(
    (channel) => channel.platform === platform,
  );
  return (
    <div className="messaging-settings">
      <div className="messaging-heading">
        <h3>
          <MessageCircle size={16} /> 消息渠道
        </h3>
        <button
          className="icon-button"
          type="button"
          aria-label="刷新渠道状态"
          onClick={() => void channels.refetch()}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      <p className="muted">
        在微信、QQ 或飞书中使用你的 MewDex 项目。由中央后端保持连接。
      </p>
      <div className="messaging-platforms" role="group" aria-label="消息平台">
        {Object.entries(names).map(([id, name]) => (
          <button
            type="button"
            key={id}
            aria-pressed={platform === id}
            onClick={() => setPlatform(id)}
          >
            {name}
          </button>
        ))}
      </div>
      {channels.isPending && <p role="status">正在读取消息渠道…</p>}
      {channels.error && (
        <p className="error" role="alert">
          {channels.error.message}。请确认中央后端已更新并支持消息渠道。
        </p>
      )}
      {selected && (
        <ChannelEditor
          key={platform}
          channel={selected}
          client={client}
          devices={devices}
          onSaved={onSaved}
        />
      )}
    </div>
  );
}
