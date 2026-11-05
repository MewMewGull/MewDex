import { t } from "../localization.ts";
import { useQuery } from "@tanstack/react-query";
import { Globe2, Network, Sparkles } from "lucide-react";
import type { Device, Identity, MewDexClient } from "@mewdex/sdk";
import { CopyButton } from "../components/CopyButton";
import { ModelAccountCard } from "./ModelAccountCard";

export function HubConnectionPanel({
  baseURL,
  identity,
  locallyHosted,
  onChangeHub,
}: {
  baseURL: string;
  identity: Identity;
  locallyHosted: boolean;
  onChangeHub?: () => void;
}) {
  const address = new URL(baseURL);
  const localAddress = ["localhost", "127.0.0.1", "[::1]"].includes(
    address.hostname,
  );
  return (
    <section className="hub-connection-panel network-section">
      <div className="hub-connection-heading">
        <span className="hub-connection-symbol">
          <Globe2 size={25} />
        </span>
        <div>
          <span className="hub-connection-eyebrow">{t("当前 Hub")}</span>
          <h3>{address.host}</h3>
          <p>
            {locallyHosted
              ? t("由这台电脑托管")
              : localAddress
                ? t("通过本机 Hub 入口访问")
                : t("通过远端 Hub 管理项目与对话")}
          </p>
        </div>
        <span className="hub-connection-badge">{t("已连接")}</span>
      </div>
      <div className="hub-connection-address">
        <code>{baseURL}</code>
        <CopyButton text={baseURL} label={t("复制 Hub 地址")} />
      </div>
      <dl className="connection-details hub-connection-facts">
        <div>
          <dt>{t("连接方式")}</dt>
          <dd>
            {locallyHosted || localAddress ? t("本机 Hub") : t("远端 Hub")}
          </dd>
        </div>
        <div>
          <dt>{t("输入身份")}</dt>
          <dd>{identity.name}</dd>
        </div>
        <div>
          <dt>{t("访问权限")}</dt>
          <dd>
            {identity.role === "admin"
              ? t("Hub 管理员")
              : t("已授权的项目与对话")}
          </dd>
        </div>
      </dl>
      <p className="muted">
        {t("项目目录组、对话和 GPT 账号由此 Hub 统一管理。")}
      </p>
      {onChangeHub && (
        <button className="settings-link" onClick={onChangeHub}>
          {t("切换 Hub")}
        </button>
      )}
    </section>
  );
}

export function HubGPTPanel({
  client,
  baseURL,
  devices,
  canManage,
  onNetworkSettings,
}: {
  client: MewDexClient;
  baseURL: string;
  devices?: Device[];
  canManage: boolean;
  onNetworkSettings?: () => void;
}) {
  const nodes = useQuery({
    queryKey: ["hub-settings-devices", baseURL],
    queryFn: () => client.devices(),
    enabled: !devices,
  });
  const hub = (devices ?? nodes.data ?? []).find((device) =>
    device.roles.includes("coordinator"),
  );
  return (
    <div className="hub-gpt-settings">
      <section className="network-section">
        <h3>
          <Sparkles size={17} />
          {t(" Hub 共用 GPT 账号")}
        </h3>
        <p className="muted">
          {t("所有输入端和执行端共用当前 Hub 的模型连接。")}
        </p>
        <div className="hub-model-owner">
          <Globe2 size={15} />
          <code>{baseURL}</code>
        </div>
        {canManage && hub ? (
          <ModelAccountCard
            key={hub.id}
            client={client}
            deviceId={hub.id}
            online={hub.online}
          />
        ) : (
          <p className="muted">
            {nodes.isPending && !devices
              ? t("正在读取 Hub 模型连接…")
              : canManage
                ? t("请更新此 Hub 以管理共用模型账号。")
                : t("模型账号由 Hub 管理员连接。")}
          </p>
        )}
        {nodes.error && !devices && (
          <p className="error" role="alert">
            {nodes.error.message}
          </p>
        )}
      </section>
      <section className="network-section">
        <h3>
          <Network size={17} />
          {t(" OpenAI 网络连接")}
        </h3>
        <div className="hub-connection-flow" aria-label={t("GPT 连接路径")}>
          <span>{t("此 App")}</span>
          <span>{t("当前 Hub")}</span>
          <span>OpenAI</span>
        </div>
        <p className="muted">
          {t("Hub 负责访问 OpenAI，执行指令与结果通过 Hub 同步。")}
        </p>
        {onNetworkSettings ? (
          <button className="settings-link" onClick={onNetworkSettings}>
            {t("配置此 Hub 的 OpenAI 网络")}
          </button>
        ) : (
          <p className="muted">{t("代理与出站网络在 Hub 所在机器上配置。")}</p>
        )}
      </section>
    </div>
  );
}
