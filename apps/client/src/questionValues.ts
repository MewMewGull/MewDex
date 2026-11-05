import { t } from "./localization.ts";
import type { InputQuestion } from "@mewdex/sdk";

export type QuestionValue = string | number | boolean | string[];

export function isNumericQuestion(question: Pick<InputQuestion, "kind">) {
  return question.kind === "number" || question.kind === "integer";
}

// Numeric fields keep their editing text. Only an accepted form creates the
// typed values sent to the executor; ordinary text is copied without trimming.
export function formQuestionValues(
  questions: readonly InputQuestion[],
  current: Readonly<Record<string, QuestionValue>>,
) {
  const values = { ...current };
  const errors: Record<string, string> = {};
  for (const question of questions) {
    if (!isNumericQuestion(question)) continue;
    const raw = current[question.id];
    if (raw === undefined || (typeof raw === "string" && !raw.trim())) {
      if (question.required) errors[question.id] = t("请填写这个数字。");
      else delete values[question.id];
      continue;
    }
    const value =
      typeof raw === "number"
        ? raw
        : typeof raw === "string" &&
            /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(raw.trim())
          ? Number(raw)
          : NaN;
    if (!Number.isFinite(value)) errors[question.id] = t("请输入有效数字。");
    else if (question.kind === "integer" && !Number.isInteger(value))
      errors[question.id] = t("请输入整数。");
    else if (question.minimum != null && value < question.minimum)
      errors[question.id] = t("不能小于 {0}。", question.minimum);
    else if (question.maximum != null && value > question.maximum)
      errors[question.id] = t("不能大于 {0}。", question.maximum);
    else values[question.id] = value;
  }
  return { values, errors };
}
