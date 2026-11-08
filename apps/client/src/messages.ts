import { t } from "./localization.ts";
import type { Event, ExecutionTarget, Input } from "@mewdex/sdk";
import { executionTargetFrom } from "./executionTarget.ts";

export interface Message {
  id: string;
  sequence: number;
  role: "user" | "assistant" | "tool" | "system";
  text: string;
  status?: string;
  partial?: boolean;
  toolKind?: "commandExecution" | "fileChange" | "mcpToolCall" | "computerUse";
  executionTarget?: ExecutionTarget;
  attachmentIds?: string[];
}
type Payload = Record<string, unknown>;
const object = (v: unknown): Payload =>
  v && typeof v === "object" ? (v as Payload) : {};

export function messagesFrom(events: Event[], sessionId: string): Message[] {
  const messages: Message[] = [];
  const items = new Map<string, Message>();
  const add = (message: Message) => {
    messages.push(message);
    items.set(message.id, message);
    return message;
  };
  for (const event of events) {
    if (event.sessionId !== sessionId) continue;
    const p = object(event.payload);
    if (event.kind === "message.user") {
      const input = Array.isArray(p.input) ? (p.input as Input[]) : [];
      add({
        id: event.id,
        sequence: event.sequence,
        role: "user",
        text: input
          .filter((item) => item.type === "text")
          .map((item) => item.text ?? "")
          .join("\n"),
        attachmentIds: input.flatMap((item) =>
          item.attachmentId ? [item.attachmentId] : [],
        ),
      });
    } else if (event.kind === "engine.item/agentMessage/delta") {
      const id = String(p.itemId ?? event.id);
      const message =
        items.get(id) ??
        add({ id, sequence: event.sequence, role: "assistant", text: "" });
      message.text += String(p.delta ?? "");
      message.partial = true;
    } else if (
      event.kind === "engine.item/started" ||
      event.kind === "engine.item/completed"
    ) {
      const item = object(p.item);
      const id = String(item.id ?? event.id);
      const type = String(item.type ?? "");
      if (type === "agentMessage") {
        const message =
          items.get(id) ??
          add({ id, sequence: event.sequence, role: "assistant", text: "" });
        if (typeof item.text === "string") {
          message.text = item.text;
          message.sequence = event.sequence;
        }
        message.partial =
          event.kind !== "engine.item/completed" ||
          typeof item.text !== "string";
      } else if (
        type === "commandExecution" ||
        type === "fileChange" ||
        type === "mcpToolCall" ||
        type === "computerUse"
      ) {
        const message =
          items.get(id) ??
          add({ id, sequence: event.sequence, role: "tool", text: "" });
        message.sequence = event.sequence;
        message.text = String(
          item.command ??
            item.tool ??
            (type === "fileChange" ? t("文件变更") : type),
        );
        if (type === "computerUse") {
          message.text = [
            t("窗口控制"),
            item.operation,
            item.window,
            item.error,
          ]
            .filter((value) => typeof value === "string" && value)
            .join(" · ");
        }
        message.status = String(item.status ?? "inProgress");
        message.toolKind = type;
        message.executionTarget =
          executionTargetFrom(item.executionTarget) ?? message.executionTarget;
      }
    } else if (event.kind === "engine.error" || event.kind === "queue.failed") {
      add({
        id: event.id,
        sequence: event.sequence,
        role: "system",
        text: String(
          object(p.error).message ?? p.error ?? p.message ?? t("执行出现错误"),
        ),
      });
    }
  }
  return messages;
}
