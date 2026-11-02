import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Gauge,
  LogIn,
  LogOut,
  Settings,
  ChevronUp,
  RefreshCw,
} from "lucide-react";
import type {
  Device,
  MewDexClient,
  RateLimitSnapshot,
  RateLimitWindow,
  Identity,
} from "@mewdex/sdk";
import { deviceRequest } from "../localDevice";
import { useModelAccount } from "../useModelAccount";

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
      ? `${Math.round(minutes / 1440)} 天`
      : minutes >= 60
        ? `${Math.round(minutes / 60)} 小时`
        : `${minutes} 分钟`
    : fallback;
  return (
    <div className="quota-window">
      <div>
        <span>{label}额度</span>
        <strong>剩余 {remaining}%</strong>
      </div>
      <progress
        max={100}
        value={remaining}
        aria-label={`${label}额度剩余 ${remaining}%`}
      />
      {value.resetsAt && (
        <small>
          {new Date(value.resetsAt * 1000).toLocaleString(undefined, {
            month: "numeric",
            day: "numeric",
            hour: "2-digit",
            minute: "2-digit",
          })}{" "}
          重置
        </small>
      )}
    </div>
  );
}
export function AccountMenu({
  client,
  devices,
  selectedDeviceId,
  localAvailable,
  connected,
  admin,
  onSettings,
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
  onSettings: () => void;
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
  const local = useQuery({
    queryKey: ["account-local-device"],
    queryFn: async () =>
      (await deviceRequest({ operation: "status" })).snapshot,
    enabled: localAvailable,
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const localId = local.data?.deviceId;
  const device =
    devices.find((item) => item.id === selectedDeviceId) ??
    devices.find((item) => item.id === localId);
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
      setError(cause instanceof Error ? cause.message : "退出登录失败");
    } finally {
      setBusy(false);
    }
  }
  const provider =
    value?.type === "chatgpt"
      ? "ChatGPT"
      : value?.type === "apiKey"
        ? "API"
        : "模型服务";
  const modelTitle =
    value?.email ||
    (value?.signedIn
      ? `${provider}${value.planType ? ` ${value.planType}` : ""}`
      : "连接模型账号");
  const title =
    mewdex.data?.name ||
    mewdex.data?.username ||
    identity.name ||
    "MewDex 账号";
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
        aria-label="MewDex 账号、节点订阅与设置"
      >
        <span className="avatar account-avatar">
          {title.slice(0, 1).toUpperCase()}
        </span>
        <span>
          <strong>{title}</strong>
          <small>
            <i className={connected ? "online" : "offline"} />
            {!connected ? "网络已断开" : "MewDex · 已登录"}
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
            <small>MewDex · {mewdex.data?.username || "中央后端连接"}</small>
          </span>
        </div>
        <div className="account-profile">
          <span>
            <strong>
              {device?.name || "当前节点"} · {provider}
            </strong>
            <small>
              {value?.signedIn
                ? `${modelTitle}${value.planType ? ` · ${value.planType}` : ""}`
                : "此节点未登录模型服务"}
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
              使用情况<small>{showUsage ? "收起" : "查看额度"}</small>
            </button>
            {showUsage && (
              <div className="account-usage">
                {limits.isPending &&
                !limits.isFetching &&
                !device?.capabilities?.includes("account-limits") ? (
                  <p>更新此设备的执行端后可读取额度。</p>
                ) : limits.isFetching ? (
                  <p role="status">正在读取额度…</p>
                ) : limits.error ? (
                  <p>
                    额度暂时无法读取。
                    <button onClick={() => void limits.refetch()}>
                      <RefreshCw size={13} />
                      重试
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
                        <LimitWindow value={snapshot.primary} fallback="主要" />
                      )}
                      {snapshot.secondary && (
                        <LimitWindow
                          value={snapshot.secondary}
                          fallback="次要"
                        />
                      )}
                    </div>
                  ))
                ) : (
                  <p>此模型服务暂未提供额度数据。</p>
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
              onLogin(device?.id ?? selectedDeviceId);
            }}
          >
            <LogIn size={16} />
            登录此节点的 ChatGPT
          </button>
        )}
        <button
          onClick={() => {
            if (menu.current) menu.current.open = false;
            onSettings();
          }}
        >
          <Settings size={16} />
          设置<small>Ctrl+,</small>
        </button>
        {value?.signedIn && admin && (
          <button
            disabled={!enabled || busy || !value.logoutSupported}
            onClick={() => void logout()}
          >
            <LogOut size={16} />
            {busy ? "正在退出…" : "退出此节点的 ChatGPT"}
          </button>
        )}
        {(error || account.error) && (
          <p className="error" role="alert">
            {error || "无法读取模型账号状态"}
          </p>
        )}
        <button
          onClick={() => {
            if (menu.current) menu.current.open = false;
            onLogout();
          }}
        >
          <LogOut size={16} />
          退出 MewDex
        </button>
      </div>
    </details>
  );
}
