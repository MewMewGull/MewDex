import { t } from "./localization.ts";
import type {
  CommandResult,
  Interaction,
  InteractionReply,
  Session,
} from "@mewdex/sdk";
import { isUnconfirmedCommand } from "./commandRecovery.ts";
import { formQuestionValues, type QuestionValue } from "./questionValues.ts";

export type { QuestionValue } from "./questionValues.ts";
const invalidFieldsMessage = "请检查标记的回答。";
export type InteractionResponse = {
  values: Record<string, QuestionValue>;
  fieldErrors: Record<string, string>;
  state: "idle" | "submitting" | "sent" | "unconfirmed";
  error: string;
  commandId: string;
  reading: boolean;
};

type ResponseTransport = {
  respond: (
    session: Session,
    requestId: string,
    reply: InteractionReply,
  ) => Promise<unknown>;
  result: (commandId: string) => Promise<CommandResult>;
};

export const interactionResponseKey = (
  session: Pick<Session, "deviceId" | "id">,
  request: Pick<Interaction, "id">,
) => JSON.stringify([session.deviceId, session.id, request.id]);

export function initialInteractionResponse(
  request: Interaction,
): InteractionResponse {
  return {
    values: Object.fromEntries(
      request.questions
        .filter(
          (question) =>
            question.default !== undefined && question.default !== null,
        )
        .map((question) => [question.id, question.default as QuestionValue]),
    ),
    fieldErrors: {},
    state: "idle",
    error: "",
    commandId: "",
    reading: false,
  };
}

function receiptResponse(
  current: InteractionResponse,
  result: CommandResult,
): InteractionResponse {
  const next = { ...current, reading: false, error: "" };
  if (result.state === "completed")
    return { ...next, state: "sent", commandId: "" };
  if (result.state === "failed")
    return {
      ...next,
      state: "idle",
      commandId: "",
      error: result.error || t("回复没有完成，请重新填写后提交。"),
    };
  return next;
}

// Window-local response state survives conversation navigation. Answer values
// never enter storage and are discarded after submission or scope removal.
export class InteractionResponses {
  private entries = new Map<string, InteractionResponse>();
  private active = new Map<string, object>();
  private listeners = new Set<() => void>();
  private snapshot = {
    epoch: 0,
    entries: new Map<string, InteractionResponse>() as ReadonlyMap<
      string,
      InteractionResponse
    >,
  };

  constructor(
    private transport: ResponseTransport,
    private onSettled: (sessionId: string) => void,
  ) {}

  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  change(
    session: Session,
    request: Interaction,
    field: string,
    value: QuestionValue | undefined,
  ) {
    const key = interactionResponseKey(session, request);
    const current =
      this.entries.get(key) ?? initialInteractionResponse(request);
    if (current.state !== "idle") return;
    const values = { ...current.values };
    if (value === undefined) delete values[field];
    else values[field] = value;
    const fieldErrors = { ...current.fieldErrors };
    delete fieldErrors[field];
    this.write(key, {
      ...current,
      values,
      fieldErrors,
      error:
        current.error === invalidFieldsMessage &&
        !Object.keys(fieldErrors).length
          ? ""
          : current.error,
    });
  }

  report(session: Session, request: Interaction, error: string, epoch: number) {
    if (epoch !== this.snapshot.epoch) return;
    const key = interactionResponseKey(session, request);
    this.write(key, {
      ...(this.entries.get(key) ?? initialInteractionResponse(request)),
      error,
    });
  }

  async reply(session: Session, request: Interaction, action: string) {
    const key = interactionResponseKey(session, request);
    const current =
      this.entries.get(key) ?? initialInteractionResponse(request);
    if (current.state !== "idle" || (action === "accept" && !request.canAccept))
      return;
    const reply: InteractionReply = { action };
    if (request.kind === "questions") {
      reply.answers = Object.fromEntries(
        request.questions.map((question) => {
          const value = current.values[question.id];
          return [
            question.id,
            Array.isArray(value) ? value : [String(value ?? "")],
          ];
        }),
      );
    }
    if (request.kind === "form" && action === "accept") {
      const prepared = formQuestionValues(request.questions, current.values);
      if (Object.keys(prepared.errors).length) {
        this.write(key, {
          ...current,
          fieldErrors: prepared.errors,
          error: invalidFieldsMessage,
        });
        return;
      }
      reply.values = prepared.values;
    }
    const owner = {};
    this.active.set(key, owner);
    this.write(key, {
      ...current,
      state: "submitting",
      error: "",
      fieldErrors: {},
    });
    let next: InteractionResponse;
    try {
      await this.transport.respond(session, request.id, reply);
      next = {
        ...current,
        state: "sent",
        values: {},
        fieldErrors: {},
        error: "",
        commandId: "",
      };
    } catch (cause) {
      next = isUnconfirmedCommand(cause)
        ? {
            ...current,
            state: "unconfirmed",
            values: {},
            fieldErrors: {},
            error: "",
            commandId: cause.commandId,
          }
        : {
            ...current,
            state: "idle",
            fieldErrors: {},
            error: cause instanceof Error ? cause.message : String(cause),
          };
    }
    if (this.active.get(key) !== owner) return;
    this.active.delete(key);
    this.write(key, next);
    this.onSettled(session.id);
  }

  async observe(session: Session, request: Interaction) {
    const key = interactionResponseKey(session, request);
    const current = this.entries.get(key);
    if (!current || current.state !== "unconfirmed" || current.reading) return;
    const owner = {};
    this.active.set(key, owner);
    this.write(key, { ...current, reading: true, error: "" });
    let next: InteractionResponse = { ...current, reading: false, error: "" };
    try {
      const result = await this.transport.result(current.commandId);
      next = receiptResponse(current, result);
    } catch (cause) {
      next.error = cause instanceof Error ? cause.message : String(cause);
    }
    if (this.active.get(key) !== owner) return;
    this.active.delete(key);
    this.write(key, next);
    this.onSettled(session.id);
  }

  resolve(commandId: string, result: CommandResult, epoch: number) {
    if (epoch !== this.snapshot.epoch) return;
    for (const [key, current] of this.entries) {
      if (current.state !== "unconfirmed" || current.commandId !== commandId)
        continue;
      this.active.delete(key);
      this.write(key, receiptResponse(current, result));
      const [, sessionId] = JSON.parse(key) as string[];
      this.onSettled(sessionId);
    }
  }

  retain(session: Session, pending: readonly Interaction[], epoch: number) {
    if (epoch !== this.snapshot.epoch) return;
    const keep = new Set(
      pending.map((request) => interactionResponseKey(session, request)),
    );
    let changed = false;
    for (const key of this.entries.keys()) {
      const [deviceId, sessionId] = JSON.parse(key) as string[];
      if (
        deviceId === session.deviceId &&
        sessionId === session.id &&
        !keep.has(key)
      ) {
        this.entries.delete(key);
        this.active.delete(key);
        changed = true;
      }
    }
    if (changed) this.publish();
  }

  clear() {
    this.entries = new Map();
    this.active.clear();
    this.publish(this.snapshot.epoch + 1);
  }

  private write(key: string, value: InteractionResponse) {
    this.entries.set(key, value);
    this.publish();
  }
  private publish(epoch = this.snapshot.epoch) {
    this.snapshot = { epoch, entries: new Map(this.entries) };
    for (const listener of this.listeners) listener();
  }
}
