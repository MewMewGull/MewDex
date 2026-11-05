import { t } from "../localization.ts";
import { CopyButton } from "./CopyButton";

export function PromptText({ text, label }: { text: string; label: string }) {
  return (
    <section className="prompt-text" aria-label={label}>
      <div className="prompt-text-heading">
        <strong>{label}</strong>
        <CopyButton text={text} label={t("复制{0}", label)} />
      </div>
      <pre tabIndex={0}>{text || t("（空内容）")}</pre>
    </section>
  );
}
