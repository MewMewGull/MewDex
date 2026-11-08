import { t } from "../localization.ts";
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
import { CopyButton } from "../components/CopyButton";
import "../messaging.css";

const names: Record<string, string> = {
  get weixin() {
    return t("微信");
  },
  qq: "QQ",
  get feishu() {
    return t("飞书");
  },
};
const states: Record<string, string> = {
  get disabled() {
    return t("已关闭");
  },
  get needs_credentials() {
    return t("等待连接账号");
  },
  get needs_authorization() {
    return t("等待重新授权");
  },
  get connecting() {
    return t("正在连接");
  },
  get connected() {
    return t("已连接");
  },
  get retrying() {
    return t("正在重连");
  },
  get delivery_error() {
    return t("回复送达异常");
  },
  get storage_error() {
    return t("消息保存异常");
  },
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
  bridgeId,
}: {
  channel: MessagingChannel;
  client: MewDexClient;
  devices: Device[];
  onSaved: (channels: MessagingChannel[]) => void;
  bridgeId: string;
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
    device.roles.includes("coordinator"),
  );
  const selectedDevice = executors[0];
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
        const result = await client.pollWeixinLogin(
          controller.signal,
          bridgeId,
        );
        if (controller.signal.aborted) return;
        setLogin(result);
        if (result.state === "confirmed") {
          const channels = await client.messagingChannels(
            controller.signal,
            bridgeId,
          );
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
  }, [login?.url, client, onSaved, bridgeId]);

  async function beginLogin() {
    setError("");
    setLoggingIn(true);
    setQRImage("");
    setLogin(null);
    try {
      const result = await client.beginWeixinLogin(undefined, bridgeId);
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
        deviceId: selectedDevice?.id ?? "",
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
        bridgeId,
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
        <h3>{t("{0}消息", names[channel.platform])}</h3>
        <span className={`messaging-status messaging-status-${channel.state}`}>
          {states[channel.state] ?? t("等待连接")}
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
          {t("启用{0}消息", names[channel.platform])}
        </label>
        <div className="messaging-targets">
          <label>
            {t("工作项目")}
            <select
              value={draft.projectId}
              onChange={(event) => change("projectId", event.target.value)}
            >
              <option value="">{t("选择项目")}</option>
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
              {t(
                "使用微信扫描二维码连接 iLink 机器人。允许用户列表为空时，会自动加入扫码账号。",
              )}
            </p>
            {channel.accountId && (
              <p className="messaging-id">
                {t("已连接账号：")}
                <code>{channel.accountId}</code>
              </p>
            )}
            <button
              type="button"
              onClick={() => void beginLogin()}
              disabled={loggingIn}
            >
              {loggingIn
                ? t("等待微信扫码…")
                : channel.credentialConfigured
                  ? t("重新扫码连接")
                  : t("生成微信登录二维码")}
            </button>
            {qrImage && login?.state !== "confirmed" && (
              <div className="messaging-qr">
                <img
                  src={qrImage}
                  alt={t("用微信扫描此二维码连接 MewDex")}
                  width={240}
                  height={240}
                />
                <p role="status">
                  {login?.state === "expired"
                    ? t("二维码已过期，请重新生成。")
                    : ["scaned", "scaned_but_redirect"].includes(
                          login?.state ?? "",
                        )
                      ? t("已扫码，请在微信中确认。")
                      : t("打开微信扫一扫，确认连接。")}
                </p>
              </div>
            )}
            {login?.state === "confirmed" && (
              <p role="status">
                {t("微信账号已连接。保存渠道设置后即可使用。")}
              </p>
            )}
          </div>
        ) : (
          <>
            <p className="muted">
              {feishu
                ? t(
                    "在飞书开放平台创建企业自建应用、开启机器人能力，并使用长连接订阅接收消息事件。",
                  )
                : t(
                    "在 QQ 开放平台创建机器人，开启单聊和群聊 @消息权限；测试账号需要加入平台沙箱。",
                  )}
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
                    ? t("已保存；留空保留原密钥")
                    : t("输入应用密钥")
                }
                onChange={(event) => change("secret", event.target.value)}
              />
            </label>
            {feishu && (
              <label>
                {t("机器人 Open ID（可选）")}
                <input
                  value={draft.botUserId}
                  placeholder={t("留空自动识别")}
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => change("botUserId", event.target.value)}
                />
              </label>
            )}
          </>
        )}

        <label>
          {t("允许访问的用户 ID")}
          <textarea
            value={draft.users}
            rows={3}
            placeholder={
              feishu ? t("ou_…，每行一个") : t("平台用户 ID，每行一个")
            }
            spellCheck={false}
            onChange={(event) => change("users", event.target.value)}
          />
        </label>
        <p className="muted">
          {t(
            "留空时不处理消息。连接后先向机器人发一条消息，可在下方找到你的平台用户 ID。",
          )}
        </p>
        {!weixin && (
          <label>
            {t("允许访问的群聊 ID")}
            <textarea
              value={draft.chats}
              rows={2}
              placeholder={t("留空时不处理群聊；每行一个 ID")}
              spellCheck={false}
              onChange={(event) => change("chats", event.target.value)}
            />
          </label>
        )}
        {channel.lastSenderId && (
          <div className="messaging-discovery">
            <p>
              {t("最近收到的用户：")}
              <code>{channel.lastSenderId}</code>{" "}
              <CopyButton
                text={channel.lastSenderId}
                label={t("复制用户 ID")}
              />
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
              {t("加入允许用户")}
            </button>
            {channel.lastChatId &&
              channel.lastChatId !== channel.lastSenderId && (
                <p>
                  {t("所在聊天：")}
                  <code>{channel.lastChatId}</code>{" "}
                  <CopyButton
                    text={channel.lastChatId}
                    label={t("复制聊天 ID")}
                  />
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
            {t("清除已保存的{0}", weixin ? t("微信登录凭据") : t("应用密钥"))}
          </label>
        )}
        <div className="messaging-actions">
          <button
            className="primary"
            type="button"
            disabled={!dirty || saving}
            onClick={() => void save()}
          >
            {saving ? t("正在保存…") : t("保存渠道设置")}
          </button>
          {saved && <span role="status">{t("已保存")}</span>}
          {dirty && <span className="muted">{t("有未保存更改")}</span>}
        </div>
      </fieldset>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <p className="muted">
        {t(
          "发送文字开始任务；/help 查看指令，/status 查看进度，/new 新建对话，/stop 停止任务。群聊只响应 @机器人，并为每位用户保留独立会话。图片、文件和语音附件请在 MewDex 中处理。",
        )}
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
  const [bridgeId, setBridgeId] = useState("");
  const bridges = useQuery({
    queryKey: ["input-bridges", client],
    queryFn: ({ signal }) => client.inputBridges(signal),
    refetchInterval: 4000,
    retry: false,
  });
  useEffect(() => {
    if (!bridgeId && bridges.data?.length === 1)
      setBridgeId(bridges.data[0].id);
  }, [bridgeId, bridges.data]);
  const channels = useQuery({
    queryKey: ["messaging-channels", client, bridgeId],
    queryFn: ({ signal }) => client.messagingChannels(signal, bridgeId),
    enabled: !!bridgeId,
    refetchInterval: 4000,
    retry: false,
  });
  // Keep the login polling effect stable while channel status refreshes.
  const onSaved = useCallback(
    (value: MessagingChannel[]) => {
      cache.setQueryData(["messaging-channels", client, bridgeId], value);
    },
    [cache, client, bridgeId],
  );
  const selected = channels.data?.find(
    (channel) => channel.platform === platform,
  );
  return (
    <div className="messaging-settings">
      <div className="messaging-heading">
        <h3>
          <MessageCircle size={16} />
          {t(" 消息渠道")}
        </h3>
        <button
          className="icon-button"
          type="button"
          aria-label={t("刷新渠道状态")}
          onClick={() => void channels.refetch()}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      <p className="muted">
        {t(
          "由所选输入端连接微信、QQ 和飞书，消息经输入端处理后进入 Hub。请保持该输入端服务运行。",
        )}
      </p>
      <label>
        {t("渠道所在输入端")}
        <select
          value={bridgeId}
          onChange={(event) => setBridgeId(event.target.value)}
        >
          <option value="">{t("选择在线输入端")}</option>
          {bridgeId &&
            !bridges.data?.some((bridge) => bridge.id === bridgeId) && (
              <option value={bridgeId}>{t("所选输入端已离线")}</option>
            )}
          {bridges.data?.map((bridge) => (
            <option key={bridge.id} value={bridge.id}>
              {bridge.name} · {bridge.id.slice(0, 8)}
            </option>
          ))}
        </select>
      </label>
      {!bridges.isPending && !bridges.data?.length && (
        <p role="status">
          {t("请运行已登录此账号的 APP 或网页输入端服务，以连接消息渠道。")}
        </p>
      )}
      {bridges.error && (
        <p className="error" role="alert">
          {bridges.error.message}
        </p>
      )}
      <div
        className="messaging-platforms"
        role="group"
        aria-label={t("消息平台")}
      >
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
      {bridgeId && channels.isPending && (
        <p role="status">{t("正在读取消息渠道…")}</p>
      )}
      {channels.error && (
        <p className="error" role="alert">
          {channels.error.message}
        </p>
      )}
      {selected && (
        <ChannelEditor
          key={`${bridgeId}/${platform}`}
          channel={selected}
          client={client}
          devices={devices}
          onSaved={onSaved}
          bridgeId={bridgeId}
        />
      )}
    </div>
  );
}
