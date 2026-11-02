import { useEffect, useState } from "react";
import { FolderOpen } from "lucide-react";
import {
  applyLocalInstallation,
  localInstallation,
  localServiceStates,
  prepareLocalInstallation,
  type LocalInstallation,
  type LocalInstallationPreview,
} from "../localDevice";

export function LocalInstallationPanel({
  busy,
  onWork,
  onChanged,
}: {
  busy: boolean;
  onWork: (action: () => Promise<void>) => Promise<void>;
  onChanged: () => void;
}) {
  const [current, setCurrent] = useState<LocalInstallation | null>(null);
  const [preview, setPreview] = useState<LocalInstallationPreview | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    void localInstallation()
      .then((value) => {
        if (active) setCurrent(value);
      })
      .catch((cause: Error) => {
        if (active) setError(cause.message);
      });
    return () => {
      active = false;
    };
  }, []);

  async function prepare(mode: "choose" | "default") {
    await onWork(async () => {
      setError("");
      const value = await prepareLocalInstallation(mode);
      if (value) setPreview(value);
    });
  }

  async function apply() {
    if (!preview) return;
    await onWork(async () => {
      setError("");
      try {
        await applyLocalInstallation(preview.id);
        onChanged();
      } catch (cause) {
        // The pointer may have been saved before the bridge response was lost.
        // Observe its location; choosing it again is not needed in that case.
        const observed = await localInstallation().catch(() => null);
        if (observed?.root === preview.root) onChanged();
        else throw cause;
      }
    });
  }

  const locked = busy || !!current?.developmentOverride;
  return (
    <section
      className="device-data local-installation"
      aria-label="本机配置位置"
    >
      <h3>继续使用已有配置</h3>
      <p className="muted">
        这台电脑已有 MewDex 服务时，选择它的
        device.json，继续管理原来的设备与服务数据。
      </p>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {current && (
        <details className="local-technical">
          <summary>
            当前配置位置{current.isDefault ? " · 默认目录" : ""}
          </summary>
          <code className="local-installation-path">{current.root}</code>
        </details>
      )}
      {current?.developmentOverride && (
        <p className="muted">
          当前开发启动已指定配置位置，正常启动后可在这里切换。
        </p>
      )}
      {preview ? (
        <div className="local-installation-review">
          <span className="eyebrow">
            {preview.isDefault ? "使用默认目录" : "已选择本机设备"}
          </span>
          <h4>{preview.snapshot.name ?? "准备新的本机配置"}</h4>
          <code className="local-installation-path">{preview.root}</code>
          <p>
            {preview.snapshot.configured
              ? (localServiceStates[preview.snapshot.state] ?? preview.snapshot.state)
              : preview.snapshot.setupPending
                ? "此目录有尚未完成的配置。"
                : "此目录尚未配置，可以从这里开始。"}
          </p>
          <p className="muted">
            工作台将记住这个管理位置。原服务继续保持当前状态，配置、账号与历史留在原处。
          </p>
          <div className="local-device-actions">
            <button
              className="primary"
              disabled={locked}
              onClick={() => void apply()}
            >
              使用此配置位置
            </button>
            <button disabled={busy} onClick={() => setPreview(null)}>
              取消
            </button>
          </div>
        </div>
      ) : (
        <div className="local-device-actions">
          <button disabled={locked} onClick={() => void prepare("choose")}>
            <FolderOpen size={15} /> 选择已有配置
          </button>
          {current && !current.isDefault && (
            <button disabled={locked} onClick={() => void prepare("default")}>
              使用默认目录
            </button>
          )}
        </div>
      )}
    </section>
  );
}
