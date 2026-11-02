import { useAppearance } from "../useAppearance";

export function AppearanceSettings() {
  const { value, error, update } = useAppearance();
  return (
    <section className="network-section appearance-settings">
      <h3>外观</h3>
      <p className="muted">修改即时生效，并保存在这台输入设备上。</p>
      <label>
        主题
        <select
          aria-label="主题"
          value={value.theme}
          onChange={(event) =>
            update({ theme: event.target.value as typeof value.theme })
          }
        >
          <option value="system">跟随系统</option>
          <option value="dark">深色</option>
          <option value="light">浅色</option>
        </select>
      </label>
      <label>
        界面字体
        <select
          aria-label="界面字体"
          value={value.font}
          onChange={(event) => update({ font: event.target.value })}
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
      <label>
        强调色{" "}
        <input
          type="color"
          aria-label="强调色"
          value={value.accent}
          onChange={(event) => update({ accent: event.target.value })}
        />
      </label>
      <div className="appearance-colors">
        {["#9ccba6", "#74a7ed", "#b79be8", "#e2ad7e", "#dc8fa7"].map(
          (accent) => (
            <button
              key={accent}
              aria-label={`选择强调色 ${accent}`}
              aria-pressed={value.accent === accent}
              style={{ background: accent }}
              onClick={() => update({ accent })}
            />
          ),
        )}
      </div>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={value.motion}
          onChange={(event) => update({ motion: event.target.checked })}
        />
        启用界面动效
      </label>
      <div className="appearance-preview">
        <strong>阅读与输入预览</strong>
        <p>让工作区、对话和执行设备保持清晰。</p>
        <code>const workspace = "MewDex";</code>
      </div>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
