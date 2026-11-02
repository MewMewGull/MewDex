import { useId } from "react";
import type { InputQuestion } from "@mewdex/sdk";
import type { QuestionValue } from "../interactionResponses";
import { isNumericQuestion } from "../questionValues";

export function QuestionField({
  question: q,
  value,
  disabled,
  onChange,
  error,
}: {
  question: InputQuestion;
  value: QuestionValue | undefined;
  disabled: boolean;
  onChange: (value: QuestionValue | undefined) => void;
  error?: string;
}) {
  const inputID = useId();
  const numeric = isNumericQuestion(q);
  const fieldProps = {
    "aria-invalid": error ? (true as const) : undefined,
    "aria-describedby":
      [
        q.description ? `${inputID}-description` : "",
        numeric ? `${inputID}-hint` : "",
        error ? `${inputID}-error` : "",
      ]
        .filter(Boolean)
        .join(" ") || undefined,
  };
  return (
    <div className="request-field">
      <label htmlFor={inputID}>
        {q.title}
        {q.required && <span aria-hidden="true"> *</span>}
      </label>
      {q.description && <p id={`${inputID}-description`}>{q.description}</p>}
      {numeric && (
        <p className="request-field-hint" id={`${inputID}-hint`}>
          {[
            q.kind === "integer" ? "整数" : "数字",
            q.minimum != null ? `最小 ${q.minimum}` : "",
            q.maximum != null ? `最大 ${q.maximum}` : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        </p>
      )}
      {q.allowText && q.kind !== "multi" && q.options.length > 0 && (
        <div className="answer-options">
          {q.options.map((o) => (
            <button
              key={o.value}
              type="button"
              disabled={disabled}
              aria-pressed={value === o.value}
              onClick={() => onChange(o.value)}
            >
              <strong>{o.label}</strong>
              {o.description && <small>{o.description}</small>}
            </button>
          ))}
        </div>
      )}
      {q.kind === "multi" ? (
        <fieldset id={inputID} disabled={disabled} {...fieldProps}>
          <legend className="sr-only">{q.title}</legend>
          {q.options.map((o) => (
            <label key={o.value}>
              <input
                type="checkbox"
                checked={Array.isArray(value) && value.includes(o.value)}
                onChange={(e) => {
                  const selected = Array.isArray(value) ? value : [];
                  onChange(
                    e.target.checked
                      ? [...selected, o.value]
                      : selected.filter((v) => v !== o.value),
                  );
                }}
              />
              {o.label}
            </label>
          ))}
        </fieldset>
      ) : q.kind === "select" || q.kind === "boolean" ? (
        <select
          {...fieldProps}
          id={inputID}
          disabled={disabled}
          required={q.required}
          value={value === undefined ? "" : String(value)}
          onChange={(e) =>
            onChange(
              e.target.value === ""
                ? undefined
                : q.kind === "boolean"
                  ? e.target.value === "true"
                  : e.target.value,
            )
          }
        >
          <option value="">请选择</option>
          {q.kind === "boolean" ? (
            <>
              <option value="true">是</option>
              <option value="false">否</option>
            </>
          ) : (
            q.options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))
          )}
        </select>
      ) : q.kind === "text" && q.allowText ? (
        <textarea
          {...fieldProps}
          id={inputID}
          autoComplete="off"
          rows={3}
          disabled={disabled}
          required={q.required}
          minLength={q.minLength ?? undefined}
          maxLength={q.maxLength ?? 65536}
          value={value === undefined ? "" : String(value)}
          placeholder={
            q.options.length ? "选择一个选项，或输入自己的回答" : "输入你的回答"
          }
          onChange={(event) => onChange(event.target.value)}
        />
      ) : (
        <input
          {...fieldProps}
          id={inputID}
          autoComplete="off"
          disabled={disabled}
          required={!numeric && q.required}
          aria-required={numeric ? q.required : undefined}
          type={
            q.kind === "secret"
              ? "password"
              : numeric
                ? "text"
                : q.format === "email"
                  ? "email"
                  : q.format === "uri"
                    ? "url"
                    : q.format === "date"
                      ? "date"
                      : "text"
          }
          inputMode={numeric ? "decimal" : undefined}
          minLength={numeric ? undefined : (q.minLength ?? undefined)}
          maxLength={numeric ? undefined : (q.maxLength ?? 65536)}
          value={value === undefined ? "" : String(value)}
          placeholder={
            q.allowText && q.options.length
              ? "选择一个选项，或输入自己的回答"
              : ""
          }
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {error && (
        <p id={`${inputID}-error`} className="request-field-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
