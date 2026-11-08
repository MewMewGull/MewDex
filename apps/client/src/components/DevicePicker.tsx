import { t } from "../localization.ts";
import { useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Check, ChevronDown, Search, Settings, X } from "lucide-react";
import type { Device } from "@mewdex/sdk";
import { deviceDescription, devicePresence } from "../devicePresence";
import { deviceMatches, orderedDevices } from "../workspaceNavigation";
import { useKeyboardList } from "../useKeyboardList";
import { DeviceIcon } from "./DeviceIcon";

export function DevicePicker({
  devices,
  selectedId,
  connected,
  onSelect,
  onManageHub,
}: {
  devices: Device[];
  selectedId: string;
  connected: boolean;
  onSelect: (id: string) => void;
  onManageHub: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const chosen = useRef(false);
  const selected = devices.find((device) => device.id === selectedId);
  const matches = useMemo(
    () =>
      orderedDevices(devices, selectedId).filter((device) =>
        deviceMatches(search, device),
      ),
    [devices, selectedId, search],
  );
  const navigation = useKeyboardList(matches, open, choose);
  const hub = devices.find((device) => device.roles.includes("coordinator"));
  const displayed = selected;
  function choose(device: Device) {
    chosen.current = true;
    onSelect(device.id);
    setOpen(false);
    setSearch("");
  }
  if (hub)
    return (
      <button
        type="button"
        className="device-picker-trigger"
        aria-label={t("管理 Hub 与执行端")}
        aria-haspopup="dialog"
        title={t("管理 Hub 与执行端")}
        onClick={onManageHub}
      >
        <span className="device-picker-icon">
          <DeviceIcon platform={hub.platform} />
        </span>
        <span className="device-picker-current">
          <small>{t("当前 Hub")}</small>
          <strong>{hub.name}</strong>
        </span>
        <i
          className={devicePresence(hub.online, connected).dot}
          title={devicePresence(hub.online, connected).label}
        />
        <Settings size={14} />
      </button>
    );
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(value) => {
        setOpen(value);
        if (!value) setSearch("");
      }}
    >
      <Dialog.Trigger
        className="device-picker-trigger"
        aria-label={t(
          "选择执行设备{0}",
          displayed ? t("，当前为 {0}", displayed.name) : "",
        )}
        title={
          displayed ? deviceDescription(displayed, connected) : t("选择设备")
        }
      >
        <span className="device-picker-icon">
          <DeviceIcon platform={displayed?.platform ?? ""} />
        </span>
        <span className="device-picker-current">
          <small>{t("当前设备")}</small>
          <strong>{displayed?.name ?? t("选择设备")}</strong>
        </span>
        <i
          className={devicePresence(displayed?.online, connected).dot}
          title={devicePresence(displayed?.online, connected).label}
        />
        <ChevronDown size={14} />
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog device-picker-dialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            chosen.current = false;
            input.current?.focus();
          }}
          onCloseAutoFocus={(event) => {
            if (chosen.current) event.preventDefault();
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("选择设备")}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭设备选择")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t("切换设备查看对话；新任务需要已开启执行能力并有可用项目。")}
          </Dialog.Description>
          <label className="search-field">
            <Search size={16} />
            <input
              ref={input}
              aria-label={t("搜索设备")}
              placeholder={t("搜索设备名称或系统…")}
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                navigation.setSelectedId("");
              }}
              role="combobox"
              aria-controls={navigation.listboxId}
              aria-expanded="true"
              aria-autocomplete="list"
              aria-activedescendant={
                navigation.active
                  ? navigation.optionId(navigation.active.id)
                  : undefined
              }
              onKeyDown={navigation.onKeyDown}
            />
            {search && (
              <button
                aria-label={t("清除设备搜索")}
                onClick={() => {
                  setSearch("");
                  navigation.setSelectedId("");
                  input.current?.focus();
                }}
              >
                <X size={14} />
              </button>
            )}
          </label>
          <div
            className="device-picker-list"
            role="listbox"
            id={navigation.listboxId}
            aria-label={t("设备列表")}
          >
            {matches.map((device) => (
              <button
                className={`device ${device.id === selectedId ? "selected" : ""}`}
                key={device.id}
                id={navigation.optionId(device.id)}
                role="option"
                tabIndex={-1}
                aria-selected={navigation.active?.id === device.id}
                title={`${device.name}\n${deviceDescription(device, connected)}`}
                onMouseMove={() => navigation.setSelectedId(device.id)}
                onClick={() => choose(device)}
              >
                <DeviceIcon platform={device.platform} />
                <span>
                  <strong>{device.name}</strong>
                  <small>{deviceDescription(device, connected)}</small>
                </span>
                {device.id === selectedId ? (
                  <Check size={16} />
                ) : (
                  <i className={devicePresence(device.online, connected).dot} />
                )}
              </button>
            ))}
            {!matches.length && (
              <p className="empty-list">
                {search.trim() ? t("没有找到匹配设备") : t("当前没有可见设备")}
              </p>
            )}
          </div>
          <div className="quick-switcher-caption device-picker-caption">
            <span className="keyboard-only-hint">{t("↑ ↓ 选择")}</span>
            <span className="keyboard-only-hint">{t("Enter 打开")}</span>
            <span className="keyboard-only-hint">{t("Esc 关闭")}</span>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
