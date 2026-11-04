import { Command, Layers3, Terminal, ChevronRight } from "lucide-react";
import { memo } from "react";
import type { Message } from "../messages";
import { MarkdownBody } from "../components/MarkdownBody";
import { CopyButton } from "../components/CopyButton";
import type { MewDexClient } from "@mewdex/sdk";
import { AttachmentList } from "../components/AttachmentList";

const toolStatus: Record<string, string> = {
  inProgress: "执行中",
  completed: "已完成",
  failed: "失败",
  declined: "已拒绝",
};

const MessageItem = memo(
  function MessageItem({
    message,
    client,
    onFileReference,
    focused,
    historical,
    onCommandOutput,
  }: {
    message: Message;
    client: MewDexClient;
    onFileReference?: (path: string, action?: string) => void;
    focused?: boolean;
    historical?: boolean;
    onCommandOutput?: (id: string) => void;
  }) {
    return (
      <article
        className={`message ${message.role}${focused ? " located-message" : ""}`}
        data-message-id={message.id}
        data-reading-id={message.id}
        data-reading-sequence={message.sequence}
        tabIndex={-1}
      >
        <div className="message-avatar">
          {message.role === "user" ? (
            <Command size={15} />
          ) : message.role === "tool" ? (
            <Terminal size={15} />
          ) : (
            <Layers3 size={16} />
          )}
        </div>
        <div className="message-content">
          <div className="message-label">
            {message.role === "user"
              ? "你"
              : message.role === "tool"
                ? "工具"
                : message.role === "system"
                  ? "状态"
                  : "MewDex"}
            {message.status && (
              <small>
                {historical && message.status === "inProgress"
                  ? "当时执行中"
                  : (toolStatus[message.status] ?? message.status)}
              </small>
            )}
          </div>
          <div className="message-text">
            {message.role === "assistant" && message.text ? (
              <MarkdownBody
                text={message.text}
                onFileReference={onFileReference}
              />
            ) : (
              message.text || (message.attachmentIds?.length ? "" : "正在处理…")
            )}
          </div>
          {historical && message.role === "assistant" && message.partial && (
            <p className="message-fragment-note">
              当前页只包含回复片段，可查看相邻记录。
            </p>
          )}
          {!!message.attachmentIds?.length && (
            <AttachmentList
              client={client}
              items={message.attachmentIds.map((id) => ({ id }))}
            />
          )}
          {message.text && message.role !== "tool" && (
            <div className="message-actions">
              <CopyButton
                text={message.text}
                label={historical && message.partial ? "复制片段" : "复制消息"}
              />
            </div>
          )}
          {message.toolKind === "commandExecution" && onCommandOutput && (
            <div className="message-actions">
              <button
                className="message-output-link"
                onClick={() => onCommandOutput(message.id)}
              >
                <Terminal size={13} />
                查看输出
              </button>
            </div>
          )}
        </div>
      </article>
    );
  },
  (before, after) =>
    before.client === after.client &&
    before.onFileReference === after.onFileReference &&
    before.focused === after.focused &&
    before.historical === after.historical &&
    before.onCommandOutput === after.onCommandOutput &&
    before.message.id === after.message.id &&
    before.message.sequence === after.message.sequence &&
    before.message.role === after.message.role &&
    before.message.text === after.message.text &&
    before.message.status === after.message.status &&
    before.message.partial === after.message.partial &&
    before.message.toolKind === after.message.toolKind &&
    before.message.attachmentIds?.join(":") ===
      after.message.attachmentIds?.join(":"),
);

export function MessageList({
  messages,
  client,
  onFileReference,
  focusedMessageId,
  historical,
  onCommandOutput,
}: {
  messages: Message[];
  client: MewDexClient;
  onFileReference?: (path: string, action?: string) => void;
  focusedMessageId?: string;
  historical?: boolean;
  onCommandOutput?: (id: string) => void;
}) {
  const groups: (Message | Message[])[] = [];
  for (const message of messages) {
    const previous = groups.at(-1);
    if (message.role === "tool") {
      if (Array.isArray(previous)) previous.push(message);
      else groups.push([message]);
    } else groups.push(message);
  }
  return (
    <>
      {groups.map((group) => {
        if (!Array.isArray(group))
          return (
            <MessageItem
              key={group.id}
              message={group}
              focused={focusedMessageId === group.id}
              historical={historical}
              onCommandOutput={onCommandOutput}
              client={client}
              onFileReference={onFileReference}
            />
          );
        const running = group.some(
          (message) => message.status === "inProgress",
        );
        const failed = group.some((message) => message.status === "failed");
        return (
          <details
            className={`tool-activity ${running && !historical ? "active" : ""} ${failed ? "failed" : ""}`}
            key={group[0].id}
            data-tool-group={group[0].id}
          >
            <summary
              data-reading-id={`tool-group:${group[0].id}`}
              data-reading-sequence={group[0].sequence}
            >
              <Terminal size={14} />
              <span>
                {running
                  ? historical
                    ? "当时的工具活动"
                    : "正在使用工具"
                  : failed
                    ? "工具执行有错误"
                    : "工具活动"}
              </span>
              <small>{group.length} 项</small>
              <ChevronRight size={14} />
            </summary>
            <div>
              {group.map((message) => (
                <MessageItem
                  key={message.id}
                  message={message}
                  client={client}
                  onFileReference={onFileReference}
                  onCommandOutput={onCommandOutput}
                  historical={historical}
                />
              ))}
            </div>
          </details>
        );
      })}
    </>
  );
}
