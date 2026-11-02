import { CopyButton } from "./CopyButton";

export function PromptText({ text, label }: { text: string; label: string }) {
  return (
    <section className="prompt-text" aria-label={label}>
      <div className="prompt-text-heading">
        <strong>{label}</strong>
        <CopyButton text={text} label={`复制${label}`} />
      </div>
      <pre tabIndex={0}>{text || "（空内容）"}</pre>
    </section>
  );
}
