import { t } from "../localization.ts";
import { LoaderCircle } from "lucide-react";

export function AssistantActivity({ sending }: { sending: boolean }) {
  return (
    <div className="assistant-activity" role="status" aria-live="polite">
      <LoaderCircle size={17} className="spinning" aria-hidden="true" />
      <span>
        {sending ? t("正在提交你的消息…") : t("正在思考与处理你的要求…")}
      </span>
      <span className="thinking-dots" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
    </div>
  );
}
