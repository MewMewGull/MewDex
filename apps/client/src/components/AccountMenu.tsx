import { getLanguage, t } from "../localization.ts";
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge, LogIn, LogOut, ChevronUp, RefreshCw } from "lucide-react";
import type {
  Device,
  MewDexClient,
  RateLimitSnapshot,
  RateLimitWindow,
  Identity,
} from "@mewdex/sdk";
import { useModelAccount } from "../hub-controller/useModelAccount";

function LimitWindow({
  value,
  fallback,
}: {
  value: RateLimitWindow;
  fallback: string;
}) {
  const remaining = Math.max(0, Math.min(100, 100 - value.usedPercent));
  const minutes = value.windowDurationMins;
  const label = minutes
    ? minutes >= 1440
      ? t("{0} 天", Math.round(minutes / 1440))
      : minutes >= 60
        ? t("{0} 小时", Math.round(minutes / 60))
        : t("{0} 分钟", minutes)
    : fallback;
  return (
    <div className="quota-window">
      <div>
        <span>{t("{0}额度", label)}</span>
        <strong>{t("剩余 {0}%", remaining)}</strong>
      </div>
      <progress
        max={100}
        value={remaining}
        aria-label={t("{0}额度剩余 {1}%", label, remaining)}
      />
      {value.resetsAt && (
        <small>
          {t(
            "{0} 重置",
            new Date(value.resetsAt * 1000).toLocaleString(getLanguage(), {
              month: "numeric",
              day: "numeric",
              hour: "2-digit",
              minute: "2-digit",
            }),
          )}
        </small>
      )}
    </div>
  );
}
export function AccountMenu({
  client,
  devices,
  connected,
  admin,
  onLogin,
  identity,
  mewdexSupported,
  onLogout,
}: {
  client: MewDexClient;
  devices: Device[];
  selectedDeviceId: string;
  localAvailable: boolean;
  connected: boolean;
  admin: boolean;
  onLogin: (deviceId: string) => void;
  identity: Identity;
  mewdexSupported: boolean;
  onLogout: () => void;
}) {
  const menu = useRef<HTMLDetailsElement>(null);
  const [open, setOpen] = useState(false);
  const [showUsage, setShowUsage] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const cache = useQueryClient();
  const mewdex = useQuery({
    queryKey: ["mewdex-account"],
    queryFn: () => client.mewdexAccount(),
    enabled: mewdexSupported,
    retry: false,
  });
  const device = devices.find((item) => item.roles.includes("coordinator"));
  const enabled = admin && connected && !!device?.online;
  const { account } = useModelAccount(client, device?.id ?? "", enabled);
  const value = account.data;
  const limits = useQuery({
    queryKey: ["account-limits", device?.id],
    queryFn: () => client.modelRateLimits(device!.id),
    enabled:
      enabled &&
      open &&
      showUsage &&
      !!value?.signedIn &&
      !!device?.capabilities?.includes("account-limits"),
    staleTime: 30000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (!open) return;
    const close = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && menu.current)
        menu.current.open = false;
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && menu.current) {
        menu.current.open = false;
        menu.current.querySelector("summary")?.focus();
      }
    };
    window.addEventListener("pointerdown", close);
    window.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("pointerdown", close);
      window.removeEventListener("keydown", escape);
    };
  }, [open]);
  async function logout() {
    if (!device || busy) return;
    setBusy(true);
    setError("");
    try {
      await client.logoutModelAccount(device.id);
      await cache.invalidateQueries({ queryKey: ["account", device.id] });
      cache.removeQueries({ queryKey: ["account-limits", device.id] });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("退出登录失败"));
    } finally {
      setBusy(false);
    }
  }
  const provider =
    value?.type === "chatgpt"
      ? "ChatGPT"
      : value?.type === "apiKey"
        ? "API"
        : t("模型服务");
  const modelTitle =
    value?.email ||
    (value?.signedIn
      ? `${provider}${value.planType ? ` ${value.planType}` : ""}`
      : t("连接模型账号"));
  const title =
    mewdex.data?.name ||
    mewdex.data?.username ||
    identity.name ||
    t("MewDex 账号");
  const snapshots: RateLimitSnapshot[] = Object.values(
    limits.data?.rateLimitsByLimitId ?? {},
  );
  if (!snapshots.length && limits.data?.rateLimits)
    snapshots.push(limits.data.rateLimits);
  return (
    <details
      className="account-menu"
      ref={menu}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary
        className="workspace-settings-entry"
        aria-label={t("MewDex 账号与 Hub 订阅")}
      >
        <span className="avatar account-avatar">
          {title.slice(0, 1).toUpperCase()}
        </span>
        <span>
          <strong>{title}</strong>
          <small>
            <i className={connected ? "online" : "offline"} />
            {!connected ? t("网络已断开") : t("MewDex · 已登录")}
          </small>
        </span>
        <ChevronUp size={14} />
      </summary>
      <div className="account-popover">
        <div className="account-profile">
          <span className="avatar account-avatar">
            {title.slice(0, 1).toUpperCase()}
          </span>
          <span>
            <strong>{title}</strong>
            <small>MewDex · {mewdex.data?.username || t("中央后端连接")}</small>
          </span>
        </div>
        <div className="account-profile">
          <span>
            <strong>
              {device?.name || "Hub"} · {provider}
            </strong>
            <small>
              {value?.signedIn
                ? `${modelTitle}${value.planType ? ` · ${value.planType}` : ""}`
                : t("Hub 尚未登录模型服务")}
            </small>
          </span>
        </div>
        {admin && value?.signedIn && (
          <>
            <button
              onClick={() => setShowUsage((current) => !current)}
              aria-expanded={showUsage}
            >
              <Gauge size={16} />
              {t("使用情况")}
              <small>{showUsage ? t("收起") : t("查看额度")}</small>
            </button>
            {showUsage && (
              <div className="account-usage">
                {limits.isPending &&
                !limits.isFetching &&
                !device?.capabilities?.includes("account-limits") ? (
                  <p>{t("更新 Hub 后可读取额度。")}</p>
                ) : limits.isFetching ? (
                  <p role="status">{t("正在读取额度…")}</p>
                ) : limits.error ? (
                  <p>
                    {t("额度暂时无法读取。")}
                    <button onClick={() => void limits.refetch()}>
                      <RefreshCw size={13} />
                      {t("重试")}
                    </button>
                  </p>
                ) : snapshots.length ? (
                  snapshots.map((snapshot, index) => (
                    <div key={snapshot.limitId ?? index}>
                      {snapshots.length > 1 && (
                        <strong>
                          {snapshot.limitName || snapshot.limitId}
                        </strong>
                      )}
                      {snapshot.primary && (
                        <LimitWindow
                          value={snapshot.primary}
                          fallback={t("主要")}
                        />
                      )}
                      {snapshot.secondary && (
                        <LimitWindow
                          value={snapshot.secondary}
                          fallback={t("次要")}
                        />
                      )}
                    </div>
                  ))
                ) : (
                  <p>{t("此模型服务暂未提供额度数据。")}</p>
                )}
              </div>
            )}
          </>
        )}
        {!value?.signedIn && admin && (
          <button
            disabled={!enabled}
            onClick={() => {
              if (menu.current) menu.current.open = false;
              if (device) onLogin(device.id);
            }}
          >
            <LogIn size={16} />
            {t("登录 Hub 的 ChatGPT")}
          </button>
        )}
        {value?.signedIn && admin && (
          <button
            disabled={!enabled || busy || !value.logoutSupported}
            onClick={() => void logout()}
          >
            <LogOut size={16} />
            {busy ? t("正在退出…") : t("退出 Hub 的 ChatGPT")}
          </button>
        )}
        {(error || account.error) && (
          <p className="error" role="alert">
            {error || t("无法读取模型账号状态")}
          </p>
        )}
        <button
          onClick={() => {
            if (menu.current) menu.current.open = false;
            onLogout();
          }}
        >
          <LogOut size={16} />
          {t("退出 MewDex")}
        </button>
      </div>
    </details>
  );
}
