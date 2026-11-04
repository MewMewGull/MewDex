import { useEffect, useId, useRef, useState } from "react";
import { Download, FolderOpen, RefreshCw, Upload } from "lucide-react";
import { chooseBackup, deviceRequest } from "../localDevice";
import { operationActive, useLocalOperation } from "./useLocalOperation";
import type { RestoreInfo, UpgradeInfo, Snapshot } from "./localProtocol";
import { formatBytes as bytes } from "../formatBytes";

type ReviewPlan =
  | { kind: "restore"; info: RestoreInfo }
  | { kind: "upgrade"; info: UpgradeInfo };

const phases: Record<string, string> = {
  starting: "正在启动后台任务",
  inventory: "正在读取并校验文件",
  copying: "正在准备独立的数据副本",
  encrypting: "正在准备加密",
  writing: "正在写入加密备份",
  unlocking: "正在解密备份",
  extracting: "正在解包并校验文件",
  verifying: "正在完成完整性校验",
  "checking-current": "正在核对当前配置与数据",
  validating: "正在检查备份内容",
  "preparing-engine": "正在准备执行引擎",
  "preparing-state": "正在迁移并冻结任务状态",
  "checking-prepared": "正在核对恢复副本",
  committing: "正在切换本机配置",
  complete: "处理完成",
};
const operations: Record<string, string> = {
  backup: "加密备份",
  "restore.prepare": "准备恢复",
  "restore.apply": "切换恢复配置",
  "upgrade.prepare": "准备本机升级",
  "upgrade.apply": "采用本机升级",
};
const states: Record<string, string> = {
  starting: "正在开始",
  running: "处理中",
  succeeded: "已完成",
  failed: "未完成",
  cancelled: "已取消",
  unknown: "结果待核实",
};

export function DeviceDataPanel({
  snapshot,
  busy,
  onSnapshot,
  onActivity,
  onRestored,
  onUpgraded,
  onOpenService,
}: {
  snapshot: Snapshot | null;
  busy: boolean;
  onSnapshot: (value: Snapshot) => void;
  onActivity: (active: boolean) => void;
  onRestored: () => void;
  onUpgraded: () => void;
  onOpenService: () => void;
}) {
  const heading = useId();
  const task = useLocalOperation(onSnapshot);
  const [mode, setMode] = useState<"backup" | "restore" | null>(null);
  const [path, setPath] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  const [reviewPlan, setReviewPlan] = useState<ReviewPlan | null>(null);
  const [reviewed, setReviewed] = useState(false);
  const [upgradeID, setUpgradeID] = useState("");
  const [upgradeReviewed, setUpgradeReviewed] = useState(false);
  const [restoreID, setRestoreID] = useState("");
  const action = useRef(false);
  const applied = useRef("");
  const plan = reviewPlan?.kind === "restore" ? reviewPlan.info : null;
  const upgradePlan = reviewPlan?.kind === "upgrade" ? reviewPlan.info : null;

  const active = task.active || working;
  const idle =
    !snapshot ||
    ["stopped", "unconfigured", "setup-pending", "unreadable"].includes(
      snapshot.state,
    );
  const disabled = busy || active;

  useEffect(() => {
    onActivity(active);
    return () => onActivity(false);
  }, [active, onActivity]);

  useEffect(() => {
    const value = task.job?.restore;
    if (!value) return;
    setReviewPlan({ kind: "restore", info: value });
    setRestoreID(value.id);
    setReviewed(false);
    if (value.applied && applied.current !== value.id) {
      applied.current = value.id;
      onRestored();
    }
  }, [task.job?.restore, onRestored]);

  useEffect(() => {
    const value = task.job?.upgrade;
    if (!value) return;
    setReviewPlan({ kind: "upgrade", info: value });
    setUpgradeID(value.id);
    setUpgradeReviewed(false);
    if (value.applied && applied.current !== value.id) {
      applied.current = value.id;
      onUpgraded();
    }
  }, [task.job?.upgrade, onUpgraded]);

  async function work(callback: () => Promise<void>) {
    if (action.current || disabled) return;
    action.current = true;
    setWorking(true);
    setError("");
    try {
      await callback();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      action.current = false;
      setWorking(false);
    }
  }

  async function select(kind: "backup" | "restore") {
    await work(async () => {
      const selected = await chooseBackup(kind === "backup" ? "save" : "open");
      if (!selected) return;
      setPath(selected);
      setMode(kind);
      setPassphrase("");
      setConfirmation("");
      setReviewed(false);
    });
  }

  return (
    <section className="device-data" aria-labelledby={heading}>
      <h3 id={heading}>备份与恢复</h3>
      <p className="muted">
        备份设备配置、对话与附件。项目文件另行备份；模型账号需要重新登录。
      </p>
      {!idle && <p className="muted">停止本机服务后可备份或恢复。</p>}
      <div className="local-device-actions">
        <button
          disabled={disabled || !idle || !snapshot?.configured}
          onClick={() => void select("backup")}
        >
          <Download size={15} /> 导出加密备份
        </button>
        <button
          disabled={disabled || !idle}
          onClick={() => void select("restore")}
        >
          <Upload size={15} /> 从备份恢复
        </button>
      </div>
      {snapshot?.configured &&
        snapshot.operations?.includes("upgrade.prepare") && (
          <div className="device-upgrade" aria-label="本机组件升级">
            <h4>更新本机组件</h4>
            <p className="muted">
              使用当前应用自带的组件准备迁移方案。保留设备编号、网络授权和模型账号目录；任务队列在升级后保持冻结。
            </p>
            <button
              disabled={disabled || !idle}
              onClick={() =>
                void work(async () => {
                  setUpgradeReviewed(false);
                  await task.start({
                    operation: "upgrade.prepare",
                    expectedDeviceId: snapshot.deviceId,
                  });
                })
              }
            >
              准备升级方案
            </button>
          </div>
        )}
      {mode && (
        <form
          className="device-data-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (action.current || disabled || !idle) return;
            if (
              mode === "backup" &&
              (Array.from(passphrase).length < 12 ||
                confirmation !== passphrase)
            ) {
              setError("请设置至少 12 个字符的口令，并输入相同的确认口令");
              return;
            }
            const secret = passphrase;
            void work(async () => {
              const sent = await task.start({
                operation: mode === "backup" ? "backup" : "restore.prepare",
                expectedDeviceId: snapshot?.deviceId,
                ...(mode === "backup"
                  ? { backup: { path, passphrase: secret } }
                  : { restore: { path, passphrase: secret } }),
              });
              if (sent) {
                setPassphrase("");
                setConfirmation("");
                setMode(null);
              }
            });
          }}
        >
          <label>
            {mode === "backup" ? "保存到" : "备份文件"}
            <code>{path}</code>
          </label>
          <button
            type="button"
            disabled={disabled}
            onClick={() => void select(mode)}
          >
            <FolderOpen size={14} /> 重新选择文件
          </button>
          <label>
            {mode === "backup" ? "备份口令（至少 12 个字符）" : "备份口令"}
            <input
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={passphrase}
              disabled={disabled}
              onChange={(event) => setPassphrase(event.target.value)}
              required
            />
          </label>
          {mode === "backup" && (
            <>
              <label>
                确认口令
                <input
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  value={confirmation}
                  disabled={disabled}
                  onChange={(event) => setConfirmation(event.target.value)}
                  required
                />
              </label>
              <p className="muted">
                口令不会保存，遗失后无法解密备份。已有文件不会覆盖。
              </p>
            </>
          )}
          {mode === "restore" && (
            <p className="muted">
              先准备并显示恢复方案，核对后再切换当前配置。
            </p>
          )}
          <div className="local-device-actions">
            <button
              className="primary"
              disabled={
                disabled ||
                !idle ||
                !passphrase ||
                (mode === "backup" &&
                  (Array.from(passphrase).length < 12 ||
                    confirmation !== passphrase))
              }
            >
              {mode === "backup" ? "开始加密备份" : "解密并准备恢复"}
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                setMode(null);
                setPassphrase("");
                setConfirmation("");
              }}
            >
              取消
            </button>
          </div>
        </form>
      )}
      {(error || task.error) && (
        <p className="error" role="alert">
          {error || task.error}
        </p>
      )}
      {task.checking && (
        <p className="muted" role="status">
          正在读取本机操作记录…
        </p>
      )}
      {task.uncertain && (
        <p className="muted" role="status">
          正在核实原操作的结果，不会重复发起。
        </p>
      )}
      {task.job && (
        <article className="device-operation">
          <div className="device-operation-title">
            <strong>{operations[task.job.operation] ?? "本机数据操作"}</strong>
            <span>{states[task.job.state] ?? task.job.state}</span>
          </div>
          {operationActive(task.job) && (
            <>
              <p role="status">
                {phases[task.job.progress.phase] ?? "正在处理"}
                {task.job.cancelRequested ? " · 正在等待取消" : ""}
              </p>
              {task.job.progress.totalBytes > 0 && (
                <progress
                  aria-label="当前阶段进度"
                  value={task.job.progress.bytes}
                  max={task.job.progress.totalBytes}
                />
              )}
              <p className="muted">
                {task.job.progress.files}
                {task.job.progress.totalFiles
                  ? ` / ${task.job.progress.totalFiles}`
                  : ""}{" "}
                个文件 · {bytes(task.job.progress.bytes)}
                {task.job.progress.totalBytes
                  ? ` / ${bytes(task.job.progress.totalBytes)}`
                  : ""}
              </p>
              <p className="muted">
                可以关闭窗口。重新打开本机设备设置后可继续查看。
              </p>
              <button
                disabled={
                  task.job.cancelRequested ||
                  task.job.progress.phase === "committing"
                }
                onClick={() => void task.cancel()}
              >
                取消当前操作
              </button>
            </>
          )}
          {task.job.error && <p className="error">{task.job.error}</p>}
          {task.job.state === "unknown" && (
            <p className="muted">
              没有重复执行。请核对原文件或恢复方案后再发起新的操作。
            </p>
          )}
          {task.job.state === "succeeded" && task.job.backup && (
            <>
              <code>{task.job.backup.path}</code>
              <p className="muted">
                {task.job.backup.files} 个文件 · {bytes(task.job.backup.bytes)}
                {task.job.backup.engineHistoryIncluded
                  ? " · 包含引擎对话历史"
                  : " · 外部引擎历史未包含"}
              </p>
            </>
          )}
          <details>
            <summary>操作编号</summary>
            <code>{task.job.id}</code>
          </details>
        </article>
      )}
      {(task.job || task.error || task.uncertain) && (
        <button
          className="device-operation-refresh"
          disabled={working}
          onClick={() => void task.refresh()}
        >
          <RefreshCw size={14} /> 刷新操作结果
        </button>
      )}
      {upgradePlan && (
        <article className="restore-review" aria-label="本机升级方案">
          <h4>
            {upgradePlan.applied ? "本机升级配置已采用" : "核对本机升级方案"}
          </h4>
          <p>
            <strong>{upgradePlan.name}</strong>
            <br />
            <code>{upgradePlan.deviceId}</code>
          </p>
          {upgradePlan.executor && (
            <p>
              执行组件 {upgradePlan.sourceEngineVersion} →{" "}
              {upgradePlan.targetEngineVersion}
            </p>
          )}
          {upgradePlan.projects.map((project) => (
            <p key={project.id}>
              <strong>{project.name}</strong>
              <code>{project.path}</code>
            </p>
          ))}
          <p className="muted">
            原配置和数据保留。网络授权继续有效；已有任务不会重发。升级后请核对自动启动设置，再启动服务。
          </p>
          {!upgradePlan.applied && (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={upgradeReviewed}
                  disabled={disabled}
                  onChange={(event) => setUpgradeReviewed(event.target.checked)}
                />
                我已核对设备、项目和任务冻结要求
              </label>
              <button
                className="primary"
                disabled={disabled || !idle || !upgradeReviewed}
                onClick={() =>
                  void work(async () => {
                    setUpgradeReviewed(false);
                    await task.start({
                      operation: "upgrade.apply",
                      expectedDeviceId: snapshot?.deviceId,
                      upgrade: { id: upgradePlan.id },
                    });
                  })
                }
              >
                采用此升级方案
              </button>
            </>
          )}
          {upgradePlan.applied && (
            <button className="settings-link" onClick={onOpenService}>
              查看本机服务
            </button>
          )}
          <details>
            <summary>升级方案编号</summary>
            <code>{upgradePlan.id}</code>
          </details>
        </article>
      )}
      {plan && (
        <article className="restore-review">
          <h4>{plan.applied ? "恢复配置已切换" : "核对恢复方案"}</h4>
          <p>
            <strong>{plan.name}</strong>
            <br />
            <code>{plan.deviceId}</code>
          </p>
          <p>
            {[
              plan.hub && "网络主控",
              plan.executor && "任务执行",
              plan.gateway && "模型请求出口",
            ]
              .filter(Boolean)
              .join(" · ")}
          </p>
          {plan.projects.length > 0 && (
            <div className="restore-projects">
              {plan.projects.map((project) => (
                <p key={project.id}>
                  <strong>{project.name}</strong>
                  <code>{project.path}</code>
                </p>
              ))}
            </div>
          )}
          <p className="muted">
            原配置和数据目录保留。旧网络授权失效，需要重新配对；任务队列保持冻结。
          </p>
          {plan.requiresModelLogin && (
            <p className="muted">模型账号需在恢复后的执行端重新登录。</p>
          )}
          {plan.applied ? (
            <section className="restore-next-steps" aria-label="恢复后的下一步">
              <h4>接下来</h4>
              <ol>
                {plan.hub ? (
                  <li>启动恢复后的设备网络，并连接本机工作台。</li>
                ) : (
                  <li>
                    向网络管理员取得新的执行端配对码，重新连接后再启动本机服务。
                  </li>
                )}
                {plan.executor && (
                  <li>核对原项目目录；旧任务与队列继续保持冻结。</li>
                )}
                {plan.requiresModelLogin && (
                  <li>连接恢复后的模型账号，再新建对话继续。</li>
                )}
                <li>核对这台电脑的自动启动设置。</li>
              </ol>
              <button className="settings-link" onClick={onOpenService}>
                {snapshot?.pairingRequired
                  ? "完成执行端配对"
                  : plan.hub
                    ? "查看本机网络与启动"
                    : "查看连接与启动"}
              </button>
            </section>
          ) : (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={reviewed}
                  disabled={disabled}
                  onChange={(event) => setReviewed(event.target.checked)}
                />
                我已核对设备、项目和重新配对要求
              </label>
              <button
                className="primary"
                disabled={disabled || !idle || !reviewed}
                onClick={() => {
                  setReviewed(false);
                  void task.start({
                    operation: "restore.apply",
                    expectedDeviceId: snapshot?.deviceId,
                    restore: { id: plan.id },
                  });
                }}
              >
                确认切换到这份备份
              </button>
            </>
          )}
          <details>
            <summary>恢复方案编号</summary>
            <code>{plan.id}</code>
          </details>
        </article>
      )}
      <details className="restore-lookup">
        <summary>找回已有恢复方案</summary>
        <label>
          方案编号
          <input
            value={restoreID}
            autoComplete="off"
            disabled={disabled}
            onChange={(event) => setRestoreID(event.target.value.trim())}
            placeholder="32 位方案编号"
          />
        </label>
        <button
          disabled={disabled || !/^[0-9a-f]{32}$/.test(restoreID)}
          onClick={() =>
            void work(async () => {
              const result = await deviceRequest({
                operation: "restore.status",
                restore: { id: restoreID },
              });
              onSnapshot(result.snapshot);
              if (!result.restore) throw new Error("本机没有返回恢复方案");
              setReviewPlan({ kind: "restore", info: result.restore });
              setReviewed(false);
              if (result.restore.applied) onRestored();
            })
          }
        >
          读取恢复方案
        </button>
      </details>
      <details className="restore-lookup">
        <summary>找回已准备的升级方案</summary>
        <label>
          升级方案编号
          <input
            value={upgradeID}
            disabled={disabled}
            onChange={(event) => setUpgradeID(event.target.value.trim())}
            placeholder="32 位方案编号"
          />
        </label>
        <button
          disabled={disabled || !/^[0-9a-f]{32}$/.test(upgradeID)}
          onClick={() =>
            void work(async () => {
              const result = await deviceRequest({
                operation: "upgrade.status",
                upgrade: { id: upgradeID },
              });
              onSnapshot(result.snapshot);
              if (!result.upgrade) throw new Error("本机没有返回升级方案");
              setReviewPlan({ kind: "upgrade", info: result.upgrade });
              setUpgradeReviewed(false);
              if (result.upgrade.applied) onUpgraded();
            })
          }
        >
          读取升级方案
        </button>
      </details>
    </section>
  );
}
