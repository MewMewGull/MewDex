import { Bell } from "lucide-react";
import type { InputAlerts } from "../useInputAlerts";

export function InputAlertSettings({ alerts }: { alerts: InputAlerts }) {
  return (
    <section className="network-section" aria-label="当前输入端提醒">
      <h3>
        <Bell size={15} />
        工作台提醒
      </h3>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={alerts.enabled}
          disabled={
            alerts.busy ||
            alerts.permission === "loading" ||
            alerts.permission === "unsupported"
          }
          onChange={(event) => void alerts.toggle(event.target.checked)}
        />
        任务完成与待处理请求
      </label>
      <p className="muted">
        仅在这台输入设备上提醒。开启时需要允许系统通知；点击提醒可打开仍有权限访问的对话。
      </p>
      <p className="muted">
        应用在线时接收提醒；后台暂停或关闭期间的结果，在下次打开时同步。
      </p>
      {alerts.permission === "loading" && (
        <p role="status">正在读取提醒权限…</p>
      )}
      {alerts.permission === "unsupported" && (
        <p className="muted">当前输入端版本或浏览器未提供系统提醒。</p>
      )}
      {(alerts.permission === "denied" ||
        (alerts.enabled && alerts.permission === "default")) && (
        <p className="muted">系统通知权限尚未允许。</p>
      )}
      {alerts.error && (
        <p className="error" role="alert">
          {alerts.error}
        </p>
      )}
    </section>
  );
}
