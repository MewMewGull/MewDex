# 自托管 HTTPS 入口的回程维护

`cmd/mewdex-backhaul` 是可选的运维组件，用于维护已经由运营者配置好的 SSH 反向转发与 HTTPS 入口。主产品仍采用一个可达的自托管 Hub；这里不提供设备打洞、自动配置远端服务器或多主网络。

组件只维护连接，模型、对话、配对凭据和任务队列继续由原服务管理。手机和网页不提供此本机操作接口。

## 配置与操作

运行目录中保存 `backhaul.json`。字段包括实例 `id`、SSH 程序绝对路径及其 SHA-256、主机、用户、SSH 端口、现有身份文件和 known_hosts 路径、私有远端监听、Windows 本机 Hub 端口及公网 Hub 地址。身份字段只引用文件路径，不能内嵌私钥或密码。

远端监听必须是明确的私有或回环地址；本机目标固定为回环地址。公网要求 HTTPS，HTTP 只用于回环开发环境。使用独立配置禁用交互、自动更新主机密钥、远程命令、额外配置继承、连接复用及代理/X11 转发；既有 SSH 授权条目保持。

```text
mewdex-backhaul enable -root <owned-directory>
mewdex-backhaul start -root <owned-directory>
mewdex-backhaul status -root <owned-directory>
mewdex-backhaul stop -root <owned-directory>
mewdex-backhaul disable -root <owned-directory>
```

`enable` 将组件按内容哈希保存，并登记配置指纹对应的 OS 入口；不立即连接。`start` 通过 OS 管理器启动已登记的入口。`stop` 向当前运行实例发送带身份核对的停止请求；确认停止应读回状态和租约释放。`disable` 移除之后的自动启动登记，保留当前运行组件，随后可显式停止。

直接开发运行使用 `serve -root ...`；OS 入口使用内部 `managed -root ... -owner ...`。安装与数据租约负责单实例互斥，数据、进程资源和本机控制通道复用现有模块。Windows 子进程属于组件自己的 Job；Mac 入口接入已有独立监护。主控和模型进程不属于回程组件的终止范围。

## 状态与恢复

先检查本机 Hub，再启动受管理的 SSH。外部健康失败时保留同一条连接继续检查；SSH 退出后，关闭并等待原子进程范围，再按 1–30 秒退避重连，不重新提交模型输入。SSH 存活检查为每 15 秒一次、连续三次失败后退出。

`status` 分别返回当前工作进程是否可达、最后保存的连接状态、配置是否匹配以及启动登记。只有状态属于当前运行实例、配置指纹匹配且更新时间未超过 30 秒时，才显示 `connected=true`。旧状态不能为新进程或失联进程提供已连接证明。

配置改变后终止本组件连接并保存 `suspended`，需要重新审阅和启用，不自动采用新目标或身份。SSH 程序哈希变化也不会被静默采用。关闭工作台或结束发起配置的对话不会成为常驻组件的生命周期边界。

## 当前功能证据

`.state/evidence/functional-backhaul-supervision-20261029.json` 记录当前开发源码和检查。Windows 已使用独立回环替身完成 OS 登记、连接退出后的重连、单实例拒绝、强退及配置变更；临时登记已经注销。Go 业务检查和 Darwin 编译、静态检查通过。

实际 SSH、生产回程接替和真实登录触发尚未完成。新组件使用现有 SSH 身份的真实检查被自动审批拦截，当前保持在待授权方案阶段；原临时回程继续服务。审阅材料在 `.state/backhaul-supervision-20261029/candidate/`，该配置尚未采用。
