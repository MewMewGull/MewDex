import { useId } from "react";
import { Languages } from "lucide-react";
import { languages, t, type Language } from "../localization";
import { setLanguage, useLanguage } from "../useLanguage";

export function LanguageSettings({ compact = false }: { compact?: boolean }) {
  const id = useId();
  const { language, error } = useLanguage();
  const selector = (
    <select
      id={id}
      value={language}
      aria-label={t("语言")}
      onChange={(event) => setLanguage(event.target.value as Language)}
    >
      {languages.map((item) => (
        <option key={item.id} value={item.id} lang={item.lang}>
          {item.name}
        </option>
      ))}
    </select>
  );
  if (compact)
    return (
      <div className="connect-language">
        <label htmlFor={id}>
          <Languages size={16} /> {t("语言")}
        </label>
        {selector}
        {error && (
          <p className="error" role="alert">
            {t(error)}
          </p>
        )}
      </div>
    );
  return (
    <section className="network-section language-settings">
      <h3>
        <Languages size={16} /> {t("语言")}
      </h3>
      <p className="muted">{t("选择应用程序的显示语言，更改将立即生效。")}</p>
      <label htmlFor={id}>{t("显示语言")}</label>
      {selector}
      {error && (
        <p className="error" role="alert">
          {t(error)}
        </p>
      )}
    </section>
  );
}
