import {
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";
import { FolderOpen, WrapText } from "lucide-react";
import { parseUnifiedDiff } from "../unifiedDiff";
import { CopyButton } from "./CopyButton";
import { OutputFindBar } from "./OutputFindBar";
import { useOutputView, type DiffReading } from "../useOutputReading";

function highlighted(text: string, pattern: RegExp | null): ReactNode {
  if (!pattern) return text || " ";
  return text
    .split(pattern)
    .map((part, index) => (index % 2 ? <mark key={index}>{part}</mark> : part));
}
const statusLabel = {
  added: "新增",
  deleted: "删除",
  renamed: "重命名",
  modified: "修改",
};

export const DiffViewer = memo(function DiffViewer({
  view,
  output,
  findOpen,
  autoFocusFind,
  onCloseFind,
  onFileReference,
}: {
  view: DiffReading;
  output: string;
  findOpen: boolean;
  autoFocusFind: boolean;
  onCloseFind: () => void;
  onFileReference?: (path: string) => void;
}) {
  const files = useMemo(() => parseUnifiedDiff(output), [output]);
  const [reading, update] = useOutputView(view);
  const { selection, raw, wrap, query, index } = reading;
  const viewport = useRef<HTMLDivElement>(null);
  const firstSearch = useRef(true);
  const selected =
    selection === null
      ? files[0]
      : files.find((file) => file.key === selection);
  const all = selection === "all";
  const missing = selection !== null && !all && !selected;
  const visible = useMemo(
    () => (all ? files : selected ? [selected] : []),
    [all, files, selected],
  );
  const text = missing ? "" : all || !selected ? output : selected.raw;
  const rawMode = raw || !files.length;
  const pattern = useMemo(
    () =>
      findOpen && query
        ? new RegExp(
            "(" + query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")",
            "iu",
          )
        : null,
    [query, findOpen],
  );
  const rows = useMemo(
    () =>
      rawMode
        ? text.split("\n").map((text, i) => ({ id: `raw:${i}`, text }))
        : visible.flatMap((file) =>
            file.hunks.flatMap((hunk, hi) =>
              hunk.lines.map((line, li) => ({
                id: `${file.key}:${hi}:${li}`,
                text: line.text,
              })),
            ),
          ),
    [rawMode, text, visible],
  );
  const matches = useMemo(
    () => (pattern ? rows.filter((row) => pattern.test(row.text)) : []),
    [rows, pattern],
  );
  const active = matches[Math.min(index, matches.length - 1)];
  useLayoutEffect(() => {
    const area = viewport.current!;
    area.scrollTop = view.top;
    area.scrollLeft = view.left;
    return () => {
      view.top = area.scrollTop;
      view.left = area.scrollLeft;
    };
  }, []);
  useEffect(() => {
    if (firstSearch.current) {
      firstSearch.current = false;
      return;
    }
    if (active)
      viewport.current
        ?.querySelector<HTMLElement>(
          `[data-diff-row="${CSS.escape(active.id)}"]`,
        )
        ?.scrollIntoView({ block: "center" });
  }, [active?.id, query]);
  function move(delta: number) {
    if (matches.length)
      update({ index: (index + delta + matches.length) % matches.length });
  }
  return (
    <div className={`diff-viewer ${wrap ? "wrap-diff" : ""}`}>
      {files.length > 0 && (
        <div className="diff-view-toolbar">
          <select
            aria-label="选择差异文件"
            value={all ? "all" : (selection ?? selected?.key)}
            onChange={(event) => {
              update({
                selection: event.target.value,
                index: 0,
                top: 0,
                left: 0,
              });
              if (viewport.current) {
                viewport.current.scrollTop = 0;
                viewport.current.scrollLeft = 0;
              }
            }}
          >
            <option value="all">全部文件 · {files.length}</option>
            {missing && (
              <option value={selection!}>所选文件不在本次差异</option>
            )}
            {files.map((file) => (
              <option key={file.key} value={file.key}>
                {file.newPath ?? file.oldPath ?? "差异片段"} · +{file.added} −
                {file.removed}
              </option>
            ))}
          </select>
          <button
            className={`icon-button ${wrap ? "selected" : ""}`}
            aria-label="切换差异自动换行"
            aria-pressed={wrap}
            onClick={() => update({ wrap: !wrap })}
          >
            <WrapText size={15} />
          </button>
          <button
            aria-pressed={raw}
            onClick={() => update({ raw: !raw, index: 0 })}
          >
            {raw ? "排版视图" : "原始差异"}
          </button>
        </div>
      )}
      {findOpen && (
        <OutputFindBar
          query={query}
          onQuery={(query) => update({ query, index: 0 })}
          autoFocus={autoFocusFind}
          label="查找当前显示的差异"
          status={
            query
              ? `${matches.length ? Math.min(index, matches.length - 1) + 1 : 0}/${matches.length} 行`
              : ""
          }
          hasResults={matches.length > 0}
          onPrevious={() => move(-1)}
          onNext={() => move(1)}
          onClose={onCloseFind}
        />
      )}
      <div
        className="diff-viewport"
        ref={viewport}
        onScroll={(event) => {
          view.top = event.currentTarget.scrollTop;
          view.left = event.currentTarget.scrollLeft;
        }}
      >
        {missing ? (
          <div className="output-empty" role="status">
            所选文件不在本次差异中，原选择仍然保留。
            <button
              onClick={() =>
                update({ selection: "all", index: 0, top: 0, left: 0 })
              }
            >
              查看本次全部差异
            </button>
          </div>
        ) : !output ? (
          <p className="output-empty">当前记录还没有代码差异。</p>
        ) : rawMode ? (
          <div className="diff-raw">
            {rows.map((row) => (
              <div
                key={row.id}
                data-diff-row={row.id}
                className={active?.id === row.id ? "active-diff-match" : ""}
              >
                {highlighted(row.text, pattern)}
              </div>
            ))}
          </div>
        ) : (
          visible.map((file) => (
            <section className="diff-file" key={file.key}>
              <div className="diff-file-heading">
                <div>
                  <strong>{file.newPath ?? file.oldPath ?? "差异片段"}</strong>
                  <small>
                    {statusLabel[file.status]}
                    {file.status === "renamed" && ` · ${file.oldPath}`}{" "}
                    <span className="diff-added-count">+{file.added}</span>{" "}
                    <span className="diff-removed-count">−{file.removed}</span>
                  </small>
                </div>
                <CopyButton text={file.raw} label="复制文件差异" />
                {file.newPath && onFileReference && (
                  <button
                    className="icon-button"
                    aria-label={`查看当前文件 ${file.newPath}`}
                    title="查看项目中的当前文件"
                    onClick={() => onFileReference(file.newPath!)}
                  >
                    <FolderOpen size={15} />
                  </button>
                )}
              </div>
              {file.binary && (
                <p className="diff-file-notice">
                  二进制文件已改变，可查看当前文件或复制原始差异。
                </p>
              )}
              {!file.binary && file.hunks.length === 0 && (
                <p className="diff-file-notice">
                  {file.status === "renamed"
                    ? "文件位置已改变。"
                    : "此文件只有属性变更。"}
                </p>
              )}
              {file.hunks.map((hunk, hi) => (
                <div className="diff-hunk" key={hi}>
                  <div className="diff-hunk-heading">{hunk.header}</div>
                  {hunk.lines.map((line, li) => {
                    const id = `${file.key}:${hi}:${li}`;
                    return (
                      <div
                        key={li}
                        data-diff-row={id}
                        className={`diff-line ${line.kind} ${active?.id === id ? "active-diff-match" : ""}`}
                      >
                        <span className="diff-line-number" aria-hidden>
                          {line.oldLine ?? ""}
                        </span>
                        <span className="diff-line-number" aria-hidden>
                          {line.newLine ?? ""}
                        </span>
                        <span className="diff-line-sign" aria-hidden>
                          {line.kind === "added"
                            ? "+"
                            : line.kind === "removed"
                              ? "−"
                              : ""}
                        </span>
                        <span className="diff-line-text">
                          {highlighted(line.text, pattern)}
                        </span>
                      </div>
                    );
                  })}
                </div>
              ))}
            </section>
          ))
        )}
      </div>
      {output && !missing && (
        <div className="diff-view-footer">
          <span>
            {files.length ? `${files.length} 个文件 · 回合差异` : "原始差异"}
          </span>
          <CopyButton
            text={text}
            label={all || !selected ? "复制全部差异" : "复制当前差异"}
          />
        </div>
      )}
    </div>
  );
});
