import { useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useInfiniteQuery } from "@tanstack/react-query";
import { ArrowRight, LoaderCircle, Search, X } from "lucide-react";
import type { MessageMatch, MewDexClient, Session } from "@mewdex/sdk";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function ConversationSearchDialog({
  client,
  session,
  initialQuery,
  onSearch,
  onLocate,
  onClose,
}: {
  client: MewDexClient;
  session: Session;
  initialQuery: string;
  onSearch: (query: string) => void;
  onLocate: (match: MessageMatch) => void;
  onClose: () => void;
}) {
  const [text, setText] = useState(initialQuery);
  const [submitted, setSubmitted] = useState(initialQuery);
  const [error, setError] = useState("");
  const field = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const locating = useRef(false);
  const returnFocus = useDialogReturnFocus();
  const composing = useRef(false);
  const result = useInfiniteQuery({
    queryKey: ["message-search", session.id, submitted],
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      client.searchMessages(session.id, submitted, pageParam, 30, signal),
    getNextPageParam: (page) => (page.hasMore ? page.before : undefined),
    enabled: submitted !== "",
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const matches = result.data?.pages.flatMap((page) => page.matches) ?? [];
  function clearSearch() {
    setText("");
    setSubmitted("");
    setError("");
    onSearch("");
    field.current?.focus();
  }
  function search() {
    const query = text.trim();
    if ([...query].length > 200) {
      setError("搜索文字最多 200 个字符。");
      return;
    }
    setError("");
    onSearch(query);
    if (query === submitted) {
      if (query && !result.isFetching) void result.refetch();
    } else setSubmitted(query);
  }
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
          className="settings-dialog conversation-find-dialog"
          onCloseAutoFocus={(event) => {
            returnFocus.restore(event, locating.current);
          }}
          onKeyDown={(event) => {
            if (
              (event.ctrlKey || event.metaKey) &&
              event.key.toLowerCase() === "f"
            ) {
              event.preventDefault();
              field.current?.focus();
              field.current?.select();
            }
          }}
          onOpenAutoFocus={(event) => {
            returnFocus.capture(event);
            event.preventDefault();
            field.current?.focus();
            field.current?.select();
          }}
        >
          <div className="dialog-heading">
            <Dialog.Title>查找对话内容</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="关闭对话查找">
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {session.title} · 查找你的消息与已保存的完整回复。
          </Dialog.Description>
          <form
            className="conversation-find-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!composing.current) search();
            }}
          >
            <label className="search-field">
              <Search size={17} />
              <input
                ref={field}
                aria-label="查找当前对话"
                placeholder="输入关键词，按 Enter 搜索"
                value={text}
                maxLength={400}
                onCompositionStart={() => {
                  composing.current = true;
                }}
                onCompositionEnd={() => {
                  composing.current = false;
                }}
                onChange={(event) => {
                  setText(event.target.value);
                  if (!event.target.value) {
                    setSubmitted("");
                    onSearch("");
                  }
                }}
                onKeyDown={(event) => {
                  if (
                    event.key === "ArrowDown" &&
                    !event.nativeEvent.isComposing
                  ) {
                    const first =
                      list.current?.querySelector<HTMLButtonElement>(
                        "button[data-search-result]",
                      );
                    if (first) {
                      event.preventDefault();
                      first.focus();
                    }
                  }
                }}
              />
              {text && (
                <button
                  className="icon-button"
                  type="button"
                  aria-label="清除查找"
                  onClick={clearSearch}
                >
                  <X size={14} />
                </button>
              )}
            </label>
            <button className="primary" type="submit" disabled={!text.trim()}>
              搜索
            </button>
          </form>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div
            className="conversation-find-summary"
            role="status"
            aria-live="polite"
          >
            {result.isFetching && !result.isFetchingNextPage ? (
              <>
                <LoaderCircle size={14} className="spinning" /> 正在搜索…
              </>
            ) : submitted && result.isSuccess ? (
              <>
                <strong title={submitted}>“{submitted}”</strong>
                <span>
                  {matches.length} 条{result.hasNextPage ? "，可继续加载" : ""}{" "}
                  · 最新消息在前
                </span>
              </>
            ) : (
              "支持中文与词组，忽略大小写。"
            )}
          </div>
          <div
            className="conversation-find-results"
            ref={list}
            aria-label="对话搜索结果"
          >
            {result.error && (
              <div className="conversation-find-empty" role="alert">
                <p>暂时无法查找对话内容。</p>
                <button onClick={() => void result.refetch()}>重试搜索</button>
              </div>
            )}
            {!submitted && (
              <div className="conversation-find-empty">
                <Search size={28} />
                <p>从一句话或一个词找回之前的内容。</p>
                <small>查找本对话的文字消息，工具输出可在输出面板查看。</small>
              </div>
            )}
            {submitted && result.isSuccess && !matches.length && (
              <div className="conversation-find-empty">
                <p>没有找到匹配的消息。</p>
                <small>试试更短的关键词。</small>
              </div>
            )}
            {matches.map((match, index) => (
              <button
                key={match.sequence}
                data-search-result
                className="conversation-find-result"
                onClick={() => {
                  locating.current = true;
                  onLocate(match);
                }}
                onKeyDown={(event) => {
                  if (event.key !== "ArrowDown" && event.key !== "ArrowUp")
                    return;
                  event.preventDefault();
                  const buttons =
                    list.current?.querySelectorAll<HTMLButtonElement>(
                      "button[data-search-result]",
                    );
                  const next = index + (event.key === "ArrowDown" ? 1 : -1);
                  if (next < 0) field.current?.focus();
                  else buttons?.[Math.min(next, buttons.length - 1)]?.focus();
                }}
              >
                <span className="conversation-find-meta">
                  <strong>{match.role === "user" ? "你" : "MewDex"}</strong>
                  <time>{new Date(match.createdAt).toLocaleString()}</time>
                  <ArrowRight size={15} />
                </span>
                <span className="conversation-find-excerpt">
                  {match.excerpt.before}
                  <mark>{match.excerpt.match}</mark>
                  {match.excerpt.after}
                </span>
              </button>
            ))}
            {result.hasNextPage && (
              <button
                className="conversation-find-more"
                disabled={result.isFetchingNextPage}
                onClick={() => void result.fetchNextPage()}
              >
                {result.isFetchingNextPage ? "正在查找更多…" : "加载更多结果"}
              </button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
