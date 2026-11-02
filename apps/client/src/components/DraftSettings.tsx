import { useMemo, useState } from "react";
import { ArrowUpRight, FilePenLine, Search, Trash2, X } from "lucide-react";
import { filterDraftEntries, type DraftEntry } from "../composerDrafts";
import { CopyButton } from "./CopyButton";

export interface DraftSettingsProps {
  entries: readonly DraftEntry[];
  error: string;
  onClear: () => void;
  onOpen: (key: string) => string | undefined;
}

export function DraftSettings({
  entries,
  error,
  onClear,
  onOpen,
}: DraftSettingsProps) {
  const [search, setSearch] = useState("");
  const [openError, setOpenError] = useState("");
  const visible = useMemo(
    () => filterDraftEntries(entries, search),
    [entries, search],
  );
  return (
    <section className="network-section draft-library">
      <h3>
        <FilePenLine size={15} />
        本机草稿
      </h3>
      <p className="muted">
        未发送的文字和已上传附件引用保存在这台设备，重开后可继续编辑。未完成上传的文件保留在本次应用中，退出应用后需重新选择。按网络与输入身份分别保存，恢复草稿不会自动发送。
      </p>
      <div className="draft-settings-actions">
        <span>
          {entries.length
            ? `${entries.length} 份未发送的草稿`
            : "暂无未发送的草稿"}
        </span>
        <button disabled={!entries.length && !error} onClick={onClear}>
          <Trash2 size={14} />
          清空本机草稿
        </button>
      </div>
      {entries.length > 0 && (
        <>
          <div className="search-field draft-library-search">
            <Search size={14} />
            <input
              aria-label="搜索本机草稿"
              placeholder="搜索内容、附件、设备或项目"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
            {search && (
              <button aria-label="清除草稿搜索" onClick={() => setSearch("")}>
                <X size={13} />
              </button>
            )}
          </div>
          <div
            className="draft-library-items"
            role="list"
            aria-label="本机未发送草稿"
          >
            {visible.map((entry) => (
              <article
                key={entry.key}
                role="listitem"
                className="draft-library-item"
              >
                <div className="draft-library-item-heading">
                  <strong title={entry.title}>{entry.title}</strong>
                </div>
                <p className="draft-library-target">
                  {entry.deviceName} · {entry.projectName}
                </p>
                <p className="draft-library-preview">
                  {Array.from(
                    (
                      entry.text.trim() || entry.attachmentNames.join("、")
                    ).replace(/\s+/g, " "),
                  )
                    .slice(0, 160)
                    .join("")}
                </p>
                {(entry.uploadedCount > 0 || entry.pendingCount > 0) && (
                  <p className="draft-library-files">
                    {entry.uploadedCount > 0
                      ? `${entry.uploadedCount} 个已上传附件`
                      : ""}
                    {entry.uploadedCount > 0 && entry.pendingCount > 0
                      ? " · "
                      : ""}
                    {entry.pendingCount > 0
                      ? `${entry.pendingCount} 个待处理附件`
                      : ""}
                    {entry.failedCount > 0 && (
                      <span>{` · ${entry.failedCount} 个失败`}</span>
                    )}
                  </p>
                )}
                {entry.notice && (
                  <p className="draft-library-notice">{entry.notice}</p>
                )}
                <div className="draft-library-item-actions">
                  <button
                    disabled={!entry.canOpen}
                    onClick={() => setOpenError(onOpen(entry.key) ?? "")}
                  >
                    <ArrowUpRight size={13} />
                    {entry.canOpen
                      ? entry.canEdit
                        ? "继续编辑"
                        : "查看对话"
                      : "暂不可继续"}
                  </button>
                  {entry.text && (
                    <CopyButton text={entry.text} label="复制草稿文字" />
                  )}
                </div>
              </article>
            ))}
            {!visible.length && <p className="empty-list">没有找到匹配草稿</p>}
          </div>
        </>
      )}
      {(error || openError) && (
        <p className="error" role="alert">
          {error || openError}
        </p>
      )}
    </section>
  );
}
