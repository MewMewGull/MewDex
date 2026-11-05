import { t } from "../localization.ts";

export function WebWorkbenchButton({ baseURL }: { baseURL: string }) {
  return (
    <div className="web-workbench">
      <p className="muted">
        {t("网页输入端独立部署，与 APP 连接同一个 Hub，并共用对话、项目和设置功能。")}
      </p>
      <code>{baseURL}</code>
    </div>
  );
}
