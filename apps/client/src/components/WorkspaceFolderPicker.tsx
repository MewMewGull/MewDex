import { t } from "../localization.ts";
import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { ArrowUp, Folder, RefreshCw, X } from "lucide-react";
import type { Device, MewDexClient } from "@mewdex/sdk";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function WorkspaceFolderPicker({
  client,
  device,
  initialPath,
  onSelect,
  onClose,
}: {
  client: MewDexClient;
  device: Device;
  initialPath: string;
  onSelect: (path: string) => void;
  onClose: () => void;
}) {
  const [path, setPath] = useState(initialPath);
  const [address, setAddress] = useState(initialPath);
  const focus = useDialogReturnFocus();
  const input = useRef<HTMLInputElement>(null);
  const listing = useQuery({
    queryKey: ["workspace-browser", device.id, path],
    queryFn: () => client.browseWorkspaces(device.id, path),
    enabled: device.online,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const browse = (value: string) => {
    setPath(value);
    setAddress(value);
  };
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog folder-picker-dialog"
          onOpenAutoFocus={(event) => {
            focus.capture(event);
            event.preventDefault();
            input.current?.focus();
          }}
          onCloseAutoFocus={focus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>{t("选择文件夹")}</Dialog.Title>
            <Dialog.Close
              className="icon-button"
              aria-label={t("关闭文件夹选择")}
            >
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>{device.name}</Dialog.Description>
          <form
            className="folder-picker-address"
            onSubmit={(event) => {
              event.preventDefault();
              browse(address.trim());
            }}
          >
            <button
              className="icon-button"
              type="button"
              aria-label={t("上一级目录")}
              disabled={
                !listing.data?.parent ||
                listing.data.parent === listing.data.path
              }
              onClick={() => browse(listing.data!.parent)}
            >
              <ArrowUp size={16} />
            </button>
            <input
              ref={input}
              aria-label={t("设备上的文件夹路径")}
              placeholder={t("输入这台机器上的路径")}
              value={address}
              onChange={(event) => setAddress(event.target.value)}
            />
            <button type="submit">{t("前往")}</button>
            <button
              type="button"
              className="icon-button"
              aria-label={t("刷新文件夹")}
              disabled={listing.isFetching}
              onClick={() => void listing.refetch()}
            >
              <RefreshCw size={15} />
            </button>
          </form>
          <div
            className="workspace-directory-list folder-picker-list"
            aria-label={t("文件夹")}
          >
            {listing.isFetching && <p role="status">{t("正在读取文件夹…")}</p>}
            {listing.error && (
              <p className="error" role="alert">
                {listing.error.message}
              </p>
            )}
            {listing.data?.directories.map((item) => (
              <button key={item.path} onClick={() => browse(item.path)}>
                <Folder size={17} />
                <span>{item.name}</span>
              </button>
            ))}
            {listing.isSuccess && !listing.data.directories.length && (
              <p>{t("此目录没有子文件夹。")}</p>
            )}
          </div>
          <div className="project-dialog-footer">
            <Dialog.Close>{t("取消")}</Dialog.Close>
            <button
              className="primary"
              disabled={!listing.data?.path || listing.isFetching}
              onClick={() => onSelect(listing.data!.path)}
            >
              {t("使用此文件夹")}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
