import { t } from "./localization.ts";
import type { Event, ExecutionTarget } from "@mewdex/sdk";
import { executionTargetFrom } from "./executionTarget.ts";

export interface CommandOutput {
  id: string;
  command: string;
  cwd: string;
  output: string;
  status: string;
  partial: boolean;
  exitCode?: number;
  durationMs?: number;
  executionTarget?: ExecutionTarget;
}

const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === "object" ? (value as Record<string, unknown>) : {};

// A complete engine snapshot replaces streamed fragments; it is not another
// output chunk. Item IDs keep simultaneous commands from mixing their output.
export function commandOutputsFrom(
  events: Event[],
  sessionId: string,
): CommandOutput[] {
  const commands = new Map<string, CommandOutput>();
  function command(id: string): CommandOutput {
    let current = commands.get(id);
    if (!current) {
      current = {
        id,
        command: "",
        cwd: "",
        output: "",
        status: "",
        partial: true,
      };
      commands.set(id, current);
    }
    return current;
  }
  for (const event of events) {
    if (event.sessionId !== sessionId) continue;
    const payload = object(event.payload);
    if (event.kind === "engine.item/commandExecution/outputDelta") {
      command(String(payload.itemId ?? event.id)).output += String(
        payload.delta ?? "",
      );
    } else if (
      event.kind === "engine.item/started" ||
      event.kind === "engine.item/completed"
    ) {
      const item = object(payload.item);
      if (item.type !== "commandExecution") continue;
      const current = command(String(item.id ?? event.id));
      current.command =
        typeof item.command === "string" ? item.command : current.command;
      current.cwd = typeof item.cwd === "string" ? item.cwd : current.cwd;
      current.status =
        typeof item.status === "string" ? item.status : current.status;
      current.executionTarget =
        executionTargetFrom(item.executionTarget) ?? current.executionTarget;
      if (typeof item.aggregatedOutput === "string") {
        current.output = item.aggregatedOutput;
        current.partial = false;
      } else if (event.kind === "engine.item/started") current.partial = false;
      if (typeof item.exitCode === "number") current.exitCode = item.exitCode;
      if (typeof item.durationMs === "number")
        current.durationMs = item.durationMs;
    }
  }
  return [...commands.values()];
}

export function combinedCommandOutput(commands: CommandOutput[]): string {
  return commands
    .map(
      (command) =>
        `${command.command ? "$ " + command.command : t("[历史输出片段]")}\n${command.output}`,
    )
    .join("\n\n");
}

export function terminalCopyText(output: string): string {
  return output
    .replace(/\u001b\][\s\S]*?(?:\u0007|\u001b\\)/g, "")
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\r\n?/g, "\n");
}

export function latestDiffFrom(events: Event[], sessionId: string): string {
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i];
    if (
      event.sessionId === sessionId &&
      event.kind === "engine.turn/diff/updated"
    ) {
      const diff = object(event.payload).diff;
      return typeof diff === "string" ? diff : "";
    }
  }
  return "";
}
