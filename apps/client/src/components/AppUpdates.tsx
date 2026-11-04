import { useEffect, useState } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { Download, ExternalLink, RefreshCw, X } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  startAppUpdateChecks,
  useAppUpdates,
  type UpdateChannel,
} from "../appUpdates";
import { openExternalLink } from "../platform";
import { formatBytes } from "../formatBytes";
import { SettingsFrame } from "./SettingsFrame";
import "./app-updates.css";

export function AppUpdateSettings() {
  const updates = useAppUpdates();
  const [linkError, setLinkError] = useState("");
  const { info, release, phase, preferences } = updates;
  const busy = ["checking", "downloading", "installing"].includes(phase);
  async function open(url: string) {
    setLinkError("");
    try {
      await openExternalLink(url);
    } catch (error) {
      setLinkError(String(error));
    }
  }
  if (!isTauri()) {
    return (
      <section className="network-section">
        <h3>应用更新</h3>
        <p className="muted">网页版随提供此工作台的服务更新。</p>
        <button
          onClick={() =>
            void open("https://github.com/MewMewGull/MewDex/releases")
          }
        >
          <ExternalLink size={15} /> 查看 GitHub Release
        </button>
        {linkError && (
          <p className="error" role="alert">
            {linkError}
          </p>
        )}
      </section>
    );
  }
  const windowsInstaller =
    info?.platform === "windows" &&
    release?.package?.updateMode === "windows-nsis";
  return (
    <section
      className="network-section app-update-settings"
      aria-label="应用更新"
    >
      <h3>
        <Download size={15} /> 应用更新
      </h3>
      <p>
        当前版本 {info?.currentVersion ?? "读取中…"}
        {info ? " · " + info.platform + " " + info.arch : ""}
      </p>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={preferences.automatic}
          disabled={busy}
          onChange={(event) =>
            updates.configure({ automatic: event.target.checked })
          }
        />
        自动检查更新
      </label>
      <p className="muted">
        开启后，启动和恢复使用时自动检查 GitHub
        Release，每天至多一次。发现新版后提醒，确认后下载和安装。
      </p>
      <label>
        更新通道
        <select
          value={preferences.channel}
          disabled={busy}
          onChange={(event) =>
            updates.configure({ channel: event.target.value as UpdateChannel })
          }
        >
          <option value="stable">稳定版</option>
          <option value="preview">预览版与稳定版</option>
        </select>
      </label>
      <div className="local-device-actions">
        <button
          disabled={busy || !info}
          onClick={() => void updates.check(true)}
        >
          <RefreshCw size={15} />{" "}
          {phase === "checking" ? "正在检查…" : "检查更新"}
        </button>
        <button
          onClick={() =>
            void open("https://github.com/MewMewGull/MewDex/releases")
          }
        >
          <ExternalLink size={15} /> 所有版本
        </button>
      </div>
      {phase === "current" && <p role="status">当前已是此通道的最新版本。</p>}
      {release && (
        <article className="app-update-release">
          <h4>
            发现 {release.tag}
            {release.prerelease ? " · 预览版" : ""}
          </h4>
          {release.publishedAt && (
            <p className="muted">
              发布于 {new Date(release.publishedAt).toLocaleString()}
            </p>
          )}
          <button
            className="settings-link"
            onClick={() => void open(release.url)}
          >
            在 GitHub 查看发布说明
          </button>
          {release.notes && (
            <div className="app-update-notes">
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                  a: ({ href, children }) => (
                    <button
                      className="settings-link"
                      onClick={() => href && void open(href)}
                    >
                      {children}
                    </button>
                  ),
                  img: ({ alt }) => <span>{alt}</span>,
                }}
              >
                {release.notes}
              </ReactMarkdown>
            </div>
          )}
          {release.package ? (
            <>
              <p>
                {release.package.name} · {formatBytes(release.package.bytes)}
              </p>
              <p className="muted">{release.package.installation}</p>
              {info?.canDownload ? (
                <>
                  {phase === "available" && (
                    <button
                      className="primary"
                      onClick={() => void updates.download()}
                    >
                      <Download size={15} /> 下载更新
                    </button>
                  )}
                  {phase === "downloading" && (
                    <div role="status">
                      <progress
                        aria-label="更新包下载进度"
                        value={updates.progress?.downloaded ?? 0}
                        max={release.package.bytes}
                      />
                      <p>
                        {formatBytes(updates.progress?.downloaded ?? 0)} /{" "}
                        {formatBytes(release.package.bytes)}
                      </p>
                      <button onClick={() => void updates.cancel()}>
                        取消下载
                      </button>
                    </div>
                  )}
                  {phase === "ready" && (
                    <>
                      <p role="status">下载完成，SHA-256 校验通过。</p>
                      {windowsInstaller && (
                        <p className="muted">
                          安装时关闭并重新打开工作台。本机有执行中或待确认任务时，安装器会提示处理后重试。
                        </p>
                      )}
                      <button
                        className="primary"
                        onClick={() => void updates.install()}
                      >
                        {windowsInstaller ? "退出并安装更新" : "打开更新包"}
                      </button>
                    </>
                  )}
                  {phase === "installing" && (
                    <p role="status">正在启动安装程序…</p>
                  )}
                </>
              ) : (
                <button
                  className="primary"
                  onClick={() =>
                    void open(
                      info?.platform === "ios"
                        ? release.url
                        : "https://github.com/MewMewGull/MewDex/releases/download/" +
                            encodeURIComponent(release.tag) +
                            "/" +
                            encodeURIComponent(release.package!.name),
                    )
                  }
                >
                  <ExternalLink size={15} />{" "}
                  {info?.platform === "ios"
                    ? "查看签名安装方式"
                    : "下载并安装新版"}
                </button>
              )}
              <details>
                <summary>SHA-256</summary>
                <code className="app-update-hash">
                  {release.package.sha256}
                </code>
              </details>
            </>
          ) : (
            <p className="muted">
              此版本尚未提供适合本机架构的更新包或发布清单，可在 GitHub
              查看安装方式。
            </p>
          )}
          <button
            className="settings-link"
            disabled={busy}
            onClick={() =>
              updates.configure({
                ignoredTag:
                  preferences.ignoredTag === release.tag ? "" : release.tag,
              })
            }
          >
            {preferences.ignoredTag === release.tag
              ? "恢复此版本的更新提醒"
              : "忽略此版本的提醒"}
          </button>
        </article>
      )}
      {(updates.error || linkError) && (
        <p className="error" role="alert">
          {updates.error || linkError}
        </p>
      )}
    </section>
  );
}

export function AppUpdates() {
  const updates = useAppUpdates();
  useEffect(startAppUpdateChecks, []);
  const showNotice =
    updates.release &&
    updates.release.tag !== updates.preferences.ignoredTag &&
    updates.release.tag !== updates.dismissedTag &&
    !updates.open;
  return (
    <>
      {showNotice && (
        <aside
          className="app-update-notice"
          aria-label="发现应用更新"
          role="status"
        >
          <Download size={18} />
          <span>有新版本可下载 · {updates.release!.tag}</span>
          <button onClick={updates.show}>查看并下载</button>
          <button
            className="icon-button"
            aria-label="稍后提醒"
            onClick={updates.dismiss}
          >
            <X size={15} />
          </button>
        </aside>
      )}
      <SettingsFrame
        open={updates.open}
        onOpenChange={(open) => useAppUpdates.setState({ open })}
        title="应用更新"
        description="从 MewDex 的 GitHub Release 检查和安装更新。"
        sections={[
          { id: "updates", label: "应用更新", icon: <Download size={16} /> },
        ]}
        selected="updates"
        onSelect={() => {}}
        sizeKey="app-updates"
      >
        <AppUpdateSettings />
      </SettingsFrame>
    </>
  );
}
