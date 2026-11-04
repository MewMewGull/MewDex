import {
  useEffect,
  useId,
  useRef,
  useState,
  type ChangeEvent,
  type CSSProperties,
} from "react";
import { Check, Download, Monitor, RotateCcw, Upload } from "lucide-react";
import {
  builtInThemes,
  exportThemeFile,
  maxThemeFileBytes,
  normalizeThemeColor,
  resolveThemeColors,
  themeColorRegistry,
  themeFileName,
  type ColorTheme,
  type ThemeColorId,
  type ThemeColors,
} from "../appearance";
import { canSaveAttachment, saveAttachmentFile } from "../attachmentFiles";
import { useAppearance } from "../useAppearance";

function ThemeCard({
  theme,
  overrides,
  selected,
  onSelect,
}: {
  theme: ColorTheme;
  overrides: ThemeColors;
  selected: boolean;
  onSelect: () => void;
}) {
  const colors = resolveThemeColors(theme, overrides);
  const style = {
    "--preview-background": colors["editor.background"],
    "--preview-sidebar": colors["sideBar.background"],
    "--preview-input": colors["chat.inputBackground"],
    "--preview-foreground": colors.foreground,
    "--preview-accent": colors.accent,
  } as CSSProperties;
  return (
    <button
      type="button"
      className="appearance-theme-card"
      aria-pressed={selected}
      onClick={onSelect}
    >
      <span
        className="appearance-theme-thumbnail"
        style={style}
        aria-hidden="true"
      >
        <span className="appearance-theme-sidebar">
          <i />
          <i />
          <i />
        </span>
        <span className="appearance-theme-content">
          <i />
          <i />
          <b />
        </span>
      </span>
      <span className="appearance-theme-name">
        {theme.name}
        {selected && <Check size={15} />}
      </span>
      <small>
        {theme.type === "dark" ? "深色" : "浅色"}
        {Object.keys(overrides).length > 0 ? " · 已自定义" : ""}
      </small>
    </button>
  );
}

function ColorSetting({
  colorId,
  value,
  customized,
  onChange,
  onReset,
}: {
  colorId: ThemeColorId;
  value: string;
  customized: boolean;
  onChange: (color: string) => void;
  onReset: () => void;
}) {
  const id = useId();
  const definition = themeColorRegistry[colorId];
  const [draft, setDraft] = useState(value);
  const [invalid, setInvalid] = useState(false);
  useEffect(() => {
    setDraft(value);
    setInvalid(false);
  }, [value]);
  function commit() {
    const color = normalizeThemeColor(draft.trim());
    setInvalid(!color);
    if (color) {
      setDraft(color);
      onChange(color);
    }
  }
  return (
    <div className="appearance-color-setting">
      <label htmlFor={id}>
        {definition.label}
        <code>{colorId}</code>
      </label>
      <div className="appearance-color-controls">
        <input
          type="color"
          aria-label={`${definition.label}取色`}
          value={value.slice(0, 7)}
          onChange={(event) => onChange(event.target.value)}
        />
        <input
          id={id}
          className="appearance-color-hex"
          aria-label={`${definition.label}颜色值`}
          aria-invalid={invalid}
          aria-describedby={invalid ? `${id}-error` : undefined}
          spellCheck={false}
          maxLength={9}
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            setInvalid(false);
          }}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commit();
            }
          }}
        />
        <button
          type="button"
          className="icon-button"
          disabled={!customized}
          aria-label={`重置${definition.label}`}
          title={`重置${definition.label}`}
          onClick={onReset}
        >
          <RotateCcw size={14} />
        </button>
      </div>
      {invalid && (
        <small
          id={`${id}-error`}
          className="appearance-color-error"
          role="alert"
        >
          请输入 #RGB、#RGBA、#RRGGBB 或 #RRGGBBAA。
        </small>
      )}
    </div>
  );
}

export function AppearanceSettings() {
  const {
    value,
    error,
    activeTheme,
    update,
    setColor,
    resetColors,
    importTheme,
    removeTheme,
  } = useAppearance();
  const themes = [...builtInThemes, ...value.customThemes];
  const overrides = value.colorCustomizations[activeTheme.id] ?? {};
  const colors = resolveThemeColors(activeTheme, overrides);
  const [name, setName] = useState(activeTheme.name);
  const [fileError, setFileError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [canExport, setCanExport] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    setName(activeTheme.name);
  }, [activeTheme.id, activeTheme.name]);
  useEffect(() => {
    let disposed = false;
    void canSaveAttachment()
      .then((supported) => {
        if (!disposed) setCanExport(supported);
      })
      .catch(() => {});
    return () => {
      disposed = true;
    };
  }, []);
  async function readFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setBusy(true);
    setFileError("");
    setNotice("");
    try {
      if (file.size > maxThemeFileBytes)
        throw new Error("主题文件不能超过 256 KiB。");
      const theme = importTheme(await file.text());
      setNotice(`已导入并应用“${theme.name}”。`);
    } catch (cause) {
      setFileError(
        cause instanceof Error ? cause.message : "无法读取主题文件。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function exportCurrent() {
    setBusy(true);
    setFileError("");
    setNotice("");
    try {
      const text = exportThemeFile(name, activeTheme, overrides);
      const result = await saveAttachmentFile(
        themeFileName(name),
        new Blob([text], { type: "application/json;charset=utf-8" }),
      );
      if (result !== "cancelled")
        setNotice(
          result === "saved"
            ? "主题文件已保存，可以分享给其他用户。"
            : "主题文件已开始下载，可以分享给其他用户。",
        );
    } catch (cause) {
      setFileError(
        typeof cause === "string"
          ? cause
          : cause instanceof Error
            ? cause.message
            : "无法导出主题文件。",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="network-section appearance-settings">
      <h3>颜色主题</h3>
      <p className="muted">
        选择主题后即时生效。每个主题的自定义颜色分别保存在这台设备上。
      </p>
      <label className="checkbox appearance-system-toggle">
        <input
          type="checkbox"
          checked={value.theme === "system"}
          onChange={(event) =>
            update({ theme: event.target.checked ? "system" : activeTheme.id })
          }
        />
        <Monitor size={16} /> 跟随系统外观
      </label>
      {value.theme === "system" && (
        <div className="appearance-system-preferences">
          <label>
            系统深色时使用
            <select
              aria-label="系统深色主题"
              value={value.preferredDarkTheme}
              onChange={(event) =>
                update({ preferredDarkTheme: event.target.value })
              }
            >
              {themes
                .filter((theme) => theme.type === "dark")
                .map((theme) => (
                  <option key={theme.id} value={theme.id}>
                    {theme.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            系统浅色时使用
            <select
              aria-label="系统浅色主题"
              value={value.preferredLightTheme}
              onChange={(event) =>
                update({ preferredLightTheme: event.target.value })
              }
            >
              {themes
                .filter((theme) => theme.type === "light")
                .map((theme) => (
                  <option key={theme.id} value={theme.id}>
                    {theme.name}
                  </option>
                ))}
            </select>
          </label>
        </div>
      )}
      <div className="appearance-theme-grid" role="group" aria-label="颜色主题">
        {themes.map((theme) => (
          <ThemeCard
            key={theme.id}
            theme={theme}
            overrides={value.colorCustomizations[theme.id] ?? {}}
            selected={activeTheme.id === theme.id}
            onSelect={() => update({ theme: theme.id })}
          />
        ))}
      </div>
      <div className="appearance-section-heading">
        <div>
          <h3>自定义颜色</h3>
          <p className="muted">
            正在编辑 {activeTheme.name}。支持取色器和带透明度的十六进制颜色。
          </p>
        </div>
        <button
          type="button"
          className="appearance-action"
          disabled={Object.keys(overrides).length === 0}
          onClick={resetColors}
        >
          <RotateCcw size={14} />
          重置颜色
        </button>
      </div>
      {["基础", "输入与对话", "标题栏与终端", "状态"].map((group) => (
        <details
          key={group}
          className="appearance-color-group"
          open={group === "基础" ? true : undefined}
        >
          <summary>{group}</summary>
          <div className="appearance-color-grid">
            {(Object.keys(themeColorRegistry) as ThemeColorId[])
              .filter((id) => themeColorRegistry[id].group === group)
              .map((id) => (
                <ColorSetting
                  key={`${activeTheme.id}-${id}`}
                  colorId={id}
                  value={colors[id]}
                  customized={overrides[id] !== undefined}
                  onChange={(color) => setColor(id, color)}
                  onReset={() => setColor(id)}
                />
              ))}
          </div>
        </details>
      ))}
      <div className="appearance-preview" aria-label="主题实时预览">
        <div className="appearance-preview-sidebar">
          <strong>MewDex</strong>
          <span>我的项目</span>
          <span>当前对话</span>
        </div>
        <div className="appearance-preview-main">
          <strong>让想法开始执行</strong>
          <p>工作区、对话和执行设备保持清晰。</p>
          <code>const workspace = "MewDex";</code>
          <div className="appearance-preview-composer">
            发送一条消息…<span>↑</span>
          </div>
        </div>
      </div>
      <div className="appearance-section-heading">
        <div>
          <h3>分享主题</h3>
          <p className="muted">
            导出当前主题的完整配色。其他用户可导入 .mewdex-theme.json 文件使用。
          </p>
        </div>
      </div>
      <label>
        主题名称
        <input
          aria-label="主题名称"
          value={name}
          maxLength={80}
          onChange={(event) => setName(event.target.value)}
        />
      </label>
      <div className="appearance-file-actions">
        <button
          type="button"
          className="appearance-action"
          disabled={busy || !canExport || !name.trim()}
          onClick={() => void exportCurrent()}
        >
          <Download size={15} />
          导出主题
        </button>
        <button
          type="button"
          className="appearance-action"
          disabled={busy}
          onClick={() => fileInput.current?.click()}
        >
          <Upload size={15} />
          导入主题
        </button>
        <input
          ref={fileInput}
          className="appearance-file-input"
          type="file"
          accept=".json,application/json"
          aria-label="导入主题文件"
          disabled={busy}
          onChange={(event) => void readFile(event)}
        />
        {value.customThemes.some((theme) => theme.id === activeTheme.id) && (
          <button
            type="button"
            className="appearance-remove"
            disabled={busy}
            onClick={() => removeTheme(activeTheme.id)}
          >
            移除当前导入主题
          </button>
        )}
      </div>
      {notice && (
        <p className="appearance-file-notice" role="status">
          {notice}
        </p>
      )}
      {fileError && (
        <p className="error" role="alert">
          {fileError}
        </p>
      )}
      <div className="appearance-section-heading">
        <h3>字体与动效</h3>
      </div>
      <label>
        界面字体
        <select
          aria-label="界面字体"
          value={value.font}
          onChange={(event) =>
            update({ font: event.target.value as typeof value.font })
          }
        >
          <option value="system">系统默认</option>
          <option value="yahei">微软雅黑 / 苹方</option>
          <option value="segoe">Segoe UI</option>
          <option value="mono">等宽字体</option>
        </select>
      </label>
      <label>
        界面字号 <output>{value.fontSize}px</output>
        <input
          type="range"
          aria-label="界面字号"
          min={12}
          max={20}
          value={value.fontSize}
          onChange={(event) => update({ fontSize: Number(event.target.value) })}
        />
      </label>
      <label>
        对话字号 <output>{value.messageSize}px</output>
        <input
          type="range"
          aria-label="对话字号"
          min={13}
          max={24}
          value={value.messageSize}
          onChange={(event) =>
            update({ messageSize: Number(event.target.value) })
          }
        />
      </label>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={value.motion}
          onChange={(event) => update({ motion: event.target.checked })}
        />
        启用界面动效
      </label>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
