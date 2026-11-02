import { useEffect, useMemo, useRef, useState } from "react";
import { Search, WrapText } from "lucide-react";
import { OutputFindBar } from "./OutputFindBar";
import { textPreviewEncodings, type TextPreviewEncoding } from "../textPreview";

export function TextFilePreview({
  text,
  initialLine,
  findOpen,
  onFindOpen,
  encoding,
  resolvedEncoding,
  onEncodingChange,
}: {
  text: string;
  initialLine?: number;
  findOpen: boolean;
  onFindOpen: (open: boolean) => void;
  encoding: TextPreviewEncoding;
  resolvedEncoding: Exclude<TextPreviewEncoding, "auto">;
  onEncodingChange: (encoding: TextPreviewEncoding) => void;
}) {
  const viewport = useRef<HTMLPreElement>(null);
  const [wrap, setWrap] = useState(false);
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const lines = useMemo(() => (text ? text.split(/\r\n|\n|\r/u) : []), [text]);
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
  const matches = useMemo(
    () =>
      pattern
        ? lines.flatMap((line, index) =>
            pattern.test(line) ? [index + 1] : [],
          )
        : [],
    [lines, pattern],
  );
  const active = matches[Math.min(index, matches.length - 1)];
  const referenced =
    initialLine && initialLine <= lines.length ? initialLine : undefined;
  const current = active ?? referenced;

  function locate(line: number | undefined) {
    if (line)
      viewport.current
        ?.querySelector<HTMLElement>(`[data-text-line="${line}"]`)
        ?.scrollIntoView({ block: "center", inline: "nearest" });
  }
  useEffect(() => locate(referenced), [text, referenced]);
  useEffect(() => locate(active), [active, query]);
  function move(delta: number) {
    if (matches.length)
      setIndex((value) => (value + delta + matches.length) % matches.length);
  }
  function closeFind() {
    onFindOpen(false);
    viewport.current?.focus({ preventScroll: true });
  }
  return (
    <div className={`text-file-viewer ${wrap ? "wrap-text-file" : ""}`}>
      <div className="text-file-toolbar">
        <span>{lines.length} 行</span>
        <select
          aria-label="文本预览编码"
          title="只改变预览解码，原文件保持不变"
          value={encoding}
          onChange={(event) =>
            onEncodingChange(event.target.value as TextPreviewEncoding)
          }
        >
          {textPreviewEncodings.map((choice) => (
            <option key={choice.value} value={choice.value}>
              {choice.value === "auto"
                ? `自动 · ${textPreviewEncodings.find((item) => item.value === resolvedEncoding)?.label}`
                : choice.label}
            </option>
          ))}
        </select>
        {initialLine &&
          (referenced ? (
            <button
              className="text-file-reference"
              onClick={() => locate(referenced)}
            >
              定位第 {initialLine} 行
            </button>
          ) : (
            <span className="text-file-reference">
              引用第 {initialLine} 行不在当前预览内
            </span>
          ))}
        <button
          className="icon-button"
          aria-label="查找文件预览"
          title="查找 · Ctrl+F"
          onClick={() => onFindOpen(true)}
        >
          <Search size={15} />
        </button>
        <button
          className="icon-button"
          aria-label="切换文件自动换行"
          title="自动换行"
          aria-pressed={wrap}
          onClick={() => setWrap((value) => !value)}
        >
          <WrapText size={15} />
        </button>
      </div>
      {findOpen && (
        <OutputFindBar
          query={query}
          onQuery={(value) => {
            setQuery(value);
            setIndex(0);
          }}
          label="查找预览内容"
          closeLabel="关闭文件查找"
          status={
            query
              ? `${matches.length ? Math.min(index, matches.length - 1) + 1 : 0}/${matches.length} 行`
              : ""
          }
          hasResults={matches.length > 0}
          onPrevious={() => move(-1)}
          onNext={() => move(1)}
          onClose={closeFind}
        />
      )}
      <pre
        className="text-file-preview"
        ref={viewport}
        tabIndex={0}
        aria-label="文件文本预览"
      >
        {lines.length ? (
          lines.map((line, index) => (
            <span
              className={`text-file-line ${current === index + 1 ? "located-text-line" : ""}`}
              data-text-line={index + 1}
              key={index}
            >
              <span className="text-file-line-number" aria-hidden="true">
                {index + 1}
              </span>
              <span className="text-file-line-content">
                {pattern
                  ? line
                      .split(pattern)
                      .map((part, i) =>
                        i % 2 ? <mark key={i}>{part}</mark> : part,
                      )
                  : line}
              </span>
            </span>
          ))
        ) : (
          <span className="empty-text-file">空文本文件</span>
        )}
      </pre>
    </div>
  );
}
