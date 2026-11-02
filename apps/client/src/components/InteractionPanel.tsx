import { useRef, type Ref } from "react";
import {
  ExternalLink,
  MessageCircleQuestion,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import type { Interaction, Session } from "@mewdex/sdk";
import { openExternalLink } from "../platform";
import { QuestionField } from "./QuestionField";
import type {
  InteractionResponse,
  QuestionValue,
} from "../interactionResponses";
import type { useInteractionResponses } from "../useInteractionResponses";
import { keyboardModifierLabel } from "../inputKeys";

export function InteractionPanel({
  responses,
  session,
  online,
  requests,
  error,
  onRefresh,
  containerRef,
}: {
  responses: ReturnType<typeof useInteractionResponses>;
  session: Session;
  online: boolean;
  requests: Interaction[];
  error?: Error | null;
  onRefresh: () => Promise<unknown>;
  containerRef?: Ref<HTMLDivElement>;
}) {
  return (
    <div
      className="interaction-list"
      ref={containerRef}
      role="region"
      aria-label="当前待处理请求"
      tabIndex={-1}
    >
      {error && (
        <div className="error" role="alert">
          无法读取待处理请求。
          <button onClick={() => void onRefresh()}>重试</button>
        </div>
      )}
      {!!requests.length && (
        <div className="interaction-heading">
          <MessageCircleQuestion size={16} />
          <strong>需要你处理</strong>
          <span>{requests.length} 项</span>
        </div>
      )}
      {requests.map((request) => (
        <InteractionCard
          key={request.id}
          request={request}
          response={responses.view(session, request)}
          disabled={
            !online ||
            session.frozen ||
            (session.state !== "running" &&
              !(session.state === "idle" && !request.turnId))
          }
          onObserve={() => responses.observe(session, request)}
          onReply={(action) => responses.reply(session, request, action)}
          onChange={(field, value) =>
            responses.change(session, request, field, value)
          }
          onError={(message) => responses.report(session, request, message)}
        />
      ))}
    </div>
  );
}

function InteractionCard({
  request,
  response,
  disabled,
  onReply,
  onObserve,
  onChange,
  onError,
}: {
  request: Interaction;
  response: InteractionResponse;
  disabled: boolean;
  onReply: (action: string) => Promise<void>;
  onObserve: () => Promise<void>;
  onChange: (field: string, value: QuestionValue | undefined) => void;
  onError: (message: string) => void;
}) {
  const { values, state, error, reading } = response;
  const form = useRef<HTMLFormElement>(null);
  const locked = disabled || state !== "idle";
  const isQuestion = request.kind === "questions";
  function reply(action: string) {
    if (locked || (action === "accept" && !request.canAccept)) return;
    void onReply(action).then(() => {
      requestAnimationFrame(() =>
        form.current
          ?.querySelector<HTMLElement>('[aria-invalid="true"]')
          ?.focus(),
      );
    });
  }
  return (
    <section className="interaction-card" aria-label={request.title}>
      <header>
        {isQuestion ? (
          <MessageCircleQuestion size={17} />
        ) : (
          <ShieldCheck size={17} />
        )}
        <strong>{request.title}</strong>
        <small>
          {state === "submitting"
            ? "正在发送"
            : state === "sent"
              ? "回复已提交"
              : state === "unconfirmed"
                ? "结果待确认"
                : request.blocking
                  ? "等待处理"
                  : "可继续工作"}
        </small>
      </header>
      {request.message && <p>{request.message}</p>}
      {request.command && <pre>{request.command}</pre>}
      {request.filePatch && (
        <pre aria-label="本次文件修改">{request.filePatch}</pre>
      )}
      {request.cwd && (
        <div className="request-location">执行目录：{request.cwd}</div>
      )}
      {request.permissions && (
        <div className="request-permissions">
          <p>
            {request.kind === "permissions"
              ? "以下额外权限仅在当前回合生效："
              : "以下额外权限仅用于这一次命令："}
          </p>
          <ul>
            {request.permissions.readPaths.map((path) => (
              <li key={`read:${path}`}>
                读取：<code>{path}</code>
              </li>
            ))}
            {request.permissions.writePaths.map((path) => (
              <li key={`write:${path}`}>
                读写：<code>{path}</code>
              </li>
            ))}
            {request.permissions.network && <li>允许工具访问网络</li>}
          </ul>
        </div>
      )}
      {request.details && (
        <details>
          <summary>查看请求内容</summary>
          <pre>{request.details}</pre>
        </details>
      )}
      {request.url && (
        <div className="request-link">
          <code>{new URL(request.url).hostname}</code>
          <button
            disabled={locked}
            onClick={() =>
              void openExternalLink(request.url!).catch((cause: Error) =>
                onError(cause.message),
              )
            }
          >
            打开请求网页
            <ExternalLink size={13} />
          </button>
          <p>在网页完成操作后，再选择继续。</p>
        </div>
      )}
      <form
        ref={form}
        onSubmit={(event) => {
          event.preventDefault();
          void reply("accept");
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
          if (
            (event.ctrlKey || event.metaKey) &&
            !event.altKey &&
            !event.shiftKey &&
            !locked &&
            request.canAccept
          ) {
            event.preventDefault();
            event.currentTarget.requestSubmit();
          } else if (event.target instanceof HTMLInputElement)
            event.preventDefault();
        }}
      >
        {request.questions.map((question) => (
          <QuestionField
            key={question.id}
            question={question}
            value={values[question.id]}
            error={response.fieldErrors[question.id]}
            disabled={locked || !request.canAccept}
            onChange={(value) => onChange(question.id, value)}
          />
        ))}
        {error && (
          <div className="error" role="alert">
            {error}
          </div>
        )}
        {state === "sent" ? (
          <p role="status">回复已提交，正在同步请求状态。</p>
        ) : state === "unconfirmed" ? (
          <div className="request-result-status" role="status">
            <span>回复结果待确认，请读取当前状态。</span>
            <button
              onClick={() => void onObserve()}
              disabled={reading}
              type="button"
            >
              <RefreshCw size={14} />
              {reading ? "正在读取…" : "刷新状态"}
            </button>
          </div>
        ) : (
          <div className="request-actions">
            {!locked &&
              (isQuestion || request.kind === "form") &&
              request.canAccept && (
                <span className="request-submit-hint">
                  {keyboardModifierLabel()}+Enter 发送回复
                </span>
              )}
            {!isQuestion && (
              <button
                type="button"
                disabled={locked}
                onClick={() => void reply("decline")}
              >
                拒绝本次请求
              </button>
            )}
            {request.canAccept && (
              <button className="primary" type="submit" disabled={locked}>
                {state === "submitting"
                  ? "正在发送…"
                  : isQuestion || request.kind === "form"
                    ? "发送回复"
                    : request.kind === "link"
                      ? "继续任务"
                      : request.kind === "permissions"
                        ? "允许本回合权限"
                        : "允许本次操作"}
              </button>
            )}
          </div>
        )}
        {!request.canAccept && (
          <p className="request-note">
            当前无法接受此请求，可查看请求内容了解原因。
          </p>
        )}
        {disabled && (
          <p className="request-note">设备未连接或任务已停止，暂时不能回复。</p>
        )}
      </form>
    </section>
  );
}
