import { Monitor, Network } from "lucide-react";
import type { LocalDeviceController } from "./useLocalDevice";

export function LocalSetupPanel({ device }: { device: LocalDeviceController }) {
  const { setup, snapshot, updateSetup, busy } = device;
  const pending = !!snapshot?.setupPending;
  const locked = busy || pending;
  const canSave =
    !busy &&
    (!setup.executor || !!snapshot?.enginePresent) &&
    (!setup.hub ||
      (!!setup.account?.username.trim() &&
        (setup.account?.password.length ?? 0) >= 8));
  return (
    <form
      className="local-setup-form"
      onSubmit={(event) => {
        event.preventDefault();
        const submitter = (event.nativeEvent as SubmitEvent)
          .submitter as HTMLButtonElement | null;
        void device.saveSetup(submitter?.value !== "save");
      }}
    >
      <h3>准备这台电脑</h3>
      <p className="muted">
        启用执行能力后会准备默认工作区，进入工作台即可开始，也可选择自己的项目目录。
      </p>
      {snapshot?.startupPreference !== undefined && (
        <p className="muted">
          安装时的开机自启动选择：
          {snapshot.startupPreference ? "开启，完成配置后生效" : "关闭"}
          。可在“启动设置”中修改。
        </p>
      )}
      {setup.hub && (
        <fieldset disabled={busy}>
          <legend>MewDex 账号</legend>
          <p className="muted">
            为这个中央后端设置登录账号，其他终端可用此账号管理所有执行设备。
          </p>
          <label>
            MewDex 用户名
            <input
              autoComplete="username"
              value={setup.account?.username ?? ""}
              required
              onChange={(event) =>
                updateSetup({
                  account: {
                    username: event.target.value,
                    password: setup.account?.password ?? "",
                  },
                })
              }
            />
          </label>
          <label>
            MewDex 密码
            <input
              type="password"
              autoComplete="new-password"
              minLength={8}
              value={setup.account?.password ?? ""}
              required
              onChange={(event) =>
                updateSetup({
                  account: {
                    username: setup.account?.username ?? "",
                    password: event.target.value,
                  },
                })
              }
            />
          </label>
          <p className="muted">
            这不是 ChatGPT 账号。节点的模型订阅可进入工作台后单独设置。
          </p>
        </fieldset>
      )}
      <fieldset disabled={locked}>
        <label>
          电脑名称（可选）
          <input
            value={setup.name}
            onChange={(event) => updateSetup({ name: event.target.value })}
            placeholder="默认使用这台电脑的名称"
          />
        </label>
        <div className="setup-network-choice" aria-label="设备连接方式">
          <button
            type="button"
            aria-pressed={setup.hub}
            onClick={() => updateSetup({ hub: true })}
          >
            <Monitor size={18} />
            <strong>创建自己的网络</strong>
            <small>从这台电脑开始</small>
          </button>
          <button
            type="button"
            aria-pressed={!setup.hub}
            onClick={() => updateSetup({ hub: false, executor: true })}
          >
            <Network size={18} />
            <strong>加入已有网络</strong>
            <small>让这台电脑执行任务</small>
          </button>
        </div>
        {setup.hub && (
          <label>
            Hub 监听端口
            <input
              type="number"
              min={1}
              max={65535}
              step={1}
              required
              value={setup.listen?.split(":").at(-1) ?? "47830"}
              onChange={(event) =>
                updateSetup({ listen: `127.0.0.1:${event.target.value}` })
              }
            />
            <small>默认 47830；端口被占用时请自行修改。</small>
          </label>
        )}
        {!setup.hub && (
          <label>
            设备网络地址
            <input
              type="url"
              value={setup.hubUrl}
              onChange={(event) => updateSetup({ hubUrl: event.target.value })}
              placeholder="https://mewdex.example.com"
              spellCheck={false}
              autoCapitalize="none"
              required
            />
          </label>
        )}
      </fieldset>
      {!setup.hub && (
        <label>
          本机执行配对码
          <input
            value={setup.invitation ?? ""}
            disabled={busy}
            onChange={(event) =>
              updateSetup({ invitation: event.target.value })
            }
            autoComplete="off"
            placeholder={
              pending ? "已有授权时可留空" : "使用网络管理员提供的执行端配对码"
            }
            required={!pending}
          />
        </label>
      )}
      <details className="setup-advanced">
        <summary>高级设置</summary>
        <fieldset disabled={locked}>
          {setup.hub && (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={setup.gateway}
                  onChange={(event) =>
                    updateSetup({ gateway: event.target.checked })
                  }
                />
                为其他设备转发模型请求
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={setup.executor}
                  onChange={(event) =>
                    updateSetup({ executor: event.target.checked })
                  }
                />
                允许在这台电脑执行任务
              </label>
              <p className="muted">
                手机和远端电脑连接前，需要设置可访问的 HTTPS 地址。
              </p>
            </>
          )}
          {!setup.hub && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={setup.useHubGateway}
                onChange={(event) =>
                  updateSetup({ useHubGateway: event.target.checked })
                }
              />
              通过网络主控转发模型请求
            </label>
          )}
        </fieldset>
      </details>
      {pending && (
        <p className="muted">上次配置已保存，完成网络配对后可继续。</p>
      )}
      {setup.executor && snapshot && !snapshot.enginePresent && (
        <p className="error">安装中缺少执行组件，请补全安装后继续。</p>
      )}
      <div className="local-device-actions">
        <button
          type="submit"
          className="primary"
          value="launch"
          disabled={!canSave}
        >
          {device.phase ||
            (pending
              ? "继续配置并启动"
              : setup.hub
                ? "创建并进入工作台"
                : "保存并启动本机执行")}
        </button>
        <button type="submit" value="save" disabled={!canSave}>
          仅保存配置
        </button>
      </div>
    </form>
  );
}
