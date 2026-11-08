import { t } from "../localization.ts";
import { Command, Layers3, Terminal, ChevronRight } from "lucide-react";
import { memo } from "react";
import type { Message } from "../messages";
import { MarkdownBody } from "../components/MarkdownBody";
import { CopyButton } from "../components/CopyButton";
import type { MewDexClient } from "@mewdex/sdk";
import { AttachmentList } from "../components/AttachmentList";
import { ExecutionTargetDetails } from "../components/ExecutionTargetDetails";

const toolStatus: Record<string, string> = {
  unknown: t("状态待确认"),
  get inProgress() {
    return t("执行中");
  },
  get completed() {
    return t("已完成");
  },
  get failed() {
    return t("失败");
  },
  get declined() {
    return t("已拒绝");
  },
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
              ? t("你")
              : message.role === "tool"
                ? t("工具")
                : message.role === "system"
                  ? t("状态")
                  : "MewDex"}
            {message.status && (
              <small>
                {historical && message.status === "inProgress"
                  ? t("当时执行中")
                  : (toolStatus[message.status] ?? message.status)}
              </small>
            )}
          </div>
          <ExecutionTargetDetails target={message.executionTarget} />
          <div className="message-text">
            {message.role === "assistant" && message.text ? (
              <MarkdownBody
                text={message.text}
                onFileReference={onFileReference}
              />
            ) : (
              message.text ||
              (message.attachmentIds?.length ? "" : t("正在处理…"))
            )}
          </div>
          {historical && message.role === "assistant" && message.partial && (
            <p className="message-fragment-note">
              {t("当前页只包含回复片段，可查看相邻记录。")}
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
                label={
                  historical && message.partial ? t("复制片段") : t("复制消息")
                }
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
                {t("查看输出")}
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
    JSON.stringify(before.message.executionTarget) ===
      JSON.stringify(after.message.executionTarget) &&
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
        const targets = new Map(
          group.flatMap((message) =>
            message.executionTarget
              ? [
                  [
                    message.executionTarget.deviceId,
                    message.executionTarget,
                  ] as const,
                ]
              : [],
          ),
        );
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
                    ? t("当时的工具活动")
                    : t("正在使用工具")
                  : failed
                    ? t("工具执行有错误")
                    : t("工具活动")}
              </span>
              <small>{t("{0} 项", group.length)}</small>
              {!!targets.size && (
                <span
                  className="tool-execution-nodes"
                  title={[...targets.values()]
                    .map(
                      (target) =>
                        `${target.deviceName || target.deviceId} · ${target.deviceId}`,
                    )
                    .join("\n")}
                >
                  {[...targets.values()]
                    .map((target) => target.deviceName || target.deviceId)
                    .join(" · ")}
                </span>
              )}
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
