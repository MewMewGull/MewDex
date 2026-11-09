# Harness 交互参考

## 研究方式

用户指出具体问题时，先确认同类 Harness 是否已有对应功能或插件，阅读官方文档和相关实现，再选取适合 MewDex 的交互。记录参考来源、解决的问题、采纳的行为以及本项目的限制。优先解决当前问题，避免把一次局部优化扩大为产品重构。

## 电脑操作：2026-11-09

用户难以理解项目执行端卡片上的“窗口控制”。原界面在所有执行端重复展示入口，实际窗口选择与授权又在目标电脑的本机面板完成，缺少任务入口、下一步提示和常驻请求状态。

参考来源：

- [Codex / ChatGPT 官方 Computer Use 说明](https://learn.chatgpt.com/docs/computer-use)：在任务中说明目标应用与操作，需要时按应用授权；电脑操作权限与文件、命令权限分别管理；Windows 操作需要可见的前台桌面。优先使用专用集成来处理结构化操作。
- [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)：将能力组织为可组合插件。能力是否存在需要按实际提供者确认，不能由协调端的能力推断所有设备均支持。
- [dsh-plugin-lcu 文档与实现](https://github.com/ckanner/dsh-plugin-lcu)：按实际应用或站点提出请求，展示用户可理解的授权选项，随任务展示和清理状态。检查了 `src/approval.ts`、`src/index.ts` 的请求识别、应用能力检查和授权桥接。
- [computer-user](https://github.com/jing-hy/computer-user)、[dsh-computer-use-native](https://github.com/dao-awa/dsh-computer-use-native)、[dsh-computer-use](https://github.com/988hj7tczd-oss/dsh-computer-use)：用于比较桌面观察、键鼠操作和观察—操作—核对的完整任务路径；不同驱动的系统支持和前后台行为需要分别核实。

MewDex 采纳：

1. 项目执行端区域只有一个“电脑操作”入口，可在新建对话前查看说明、选择电脑并填入任务提示。填入提示保留已有草稿，不自动发送、不创建授权。
2. 由执行端实际公布的能力判断支持情况。Mac、Linux 等未提供驱动的节点仍可查看原因，不能触发无效操作。一个设备绑定多个目录时保留实际路由索引，只展示一次设备。
3. 当前对话出现请求时，输入区展示目标电脑、等待授权状态、具体授权步骤和取消入口。授权有效时展示窗口与停止入口；“已授权”与“正在发送输入”分别显示。
4. 授权依然由目标电脑本机完成。输入端明确提示在哪台电脑的托盘打开执行端面板，不能把输入设备的面板误当作远程授权入口。
5. 状态读取和停止结果在入口与对话框之间共享。离线、读取失败或未知结果不显示为已确认控制；到期授权不继续显示有效；停止失败保留核对和再次停止入口。
6. 本机面板收到请求后自动列出窗口。请求身份或修订变化时清空旧窗口选择及确认勾选；刷新列表也清除原选择，授权始终对应当前请求。

本次复用 MewDex 自有 Computer Use 协议和原生驱动，没有安装或复用其他产品的私有运行时。当前原生实现仍仅覆盖有交互桌面的 Windows 64 位执行端；交互检查使用隔离模拟请求，不代表真实桌面输入和物理 Escape 已验收。

## 公网接入的职责划分（2026-11-10）

用户决定将公网反向代理交由专用软件配置。MewDex 移除 APP 设置、托盘开关、SSH 参数读取与连接测试、Hub 内置反向隧道和旧回程组件迁移入口。

- Codex 官方远程连接文档建议跨网访问时使用 VPN 或组网工具。参考其将跨网连通性交由外部网络工具的做法。[Remote connections](https://learn.chatgpt.com/docs/remote-connections)
- DeepSeek Harness 默认以本机地址启动 Web UI，SSH 启动时由 SSH 客户端或编辑器持有本地转发地址。参考其将 Harness 服务与转发工具分工的做法。[官方 README](https://github.com/deepseek-ai/deepseek-harness)

MewDex 使用独立 Hub 和多执行端，输入端与执行端连接用户提供的可达 Hub 地址。浏览器来源由 Hub 的 `allowedOrigins` 显式配置，公网 HTTPS/WSS 入口及其生命周期由用户选择的专用软件管理。上述文档核对的是职责分工，具体连接协议仍采用 MewDex 自有协议。

## 侧栏分区与输入命令（2026-11-10）

用户要求补齐分区移除、拖拽排序，以及输入 `/` 时的命令与 Skill 选择。

- DeepSeek Harness 的工作区浏览器把菜单操作绑定到具体工作区，分别处理工作区排序、会话排序和浏览器保存的展开状态。此次核对了浏览器入口及工作区交互说明。MewDex 采用分区与项目共用右键和菜单按钮、拖拽插入提示及键盘上下移动的做法。[浏览器源码](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-workspace/src/client/index.ts) [工作区说明](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-workspace/README.md)
- DeepSeek Harness 的 Skill 输入源从对应会话读取目录，缓存列表并在本地过滤，选中后把调用文本写回草稿。其主机负责解释调用文本。MewDex 采用按设备、项目及会话隔离目录缓存，并提供手动刷新和失败提示。[Skill 输入源](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-skill/src/client/index.ts)
- Codex 的官方说明支持输入 `/` 打开筛选列表，已启用的 Skill 会出现在同一列表，也支持 `$` 显式调用 Skill。MewDex 的内置命令直接操作现有界面，Hub 原生 Skill 选择后填入 `$name`，由现有 Codex 运行时解析。项目配置内的 Skill 使用名称与 ID 指令，随原有项目 Harness 快照进入回合。[Slash commands](https://learn.chatgpt.com/docs/reference/slash-commands)

MewDex 的项目归属通过现有 Hub 修订校验更新。批量重命名或移除分区遇到失败时保留分区和错误，成功的逐项更新可以在重试时继续。移除分区会把项目归入默认分区。排序、空分区和折叠状态存入当前输入端已有的账号级工作台设置。原生 Skill 目录通过新增的只读 `skill.list` 指令查询 Hub 的 `skills/list`，仅展示对应项目的已启用条目。旧 Hub 可继续使用现有项目配置 Skill，并显示原生目录的升级提示。

## 窗口返回与状态缓存（2026-11-10）

用户要求切出再切回窗口时保持状态卡片稳定，查询完成后更新，前台按固定间隔查询。

- DeepSeek Harness 的 `ConnectionController` 独立持有连接代际，连接丢失、网络可用性变化和显式重连分别驱动恢复，并去重相同状态。MewDex 借鉴长连接生命周期独立于普通状态查询的划分，返回窗口时复用已有事件订阅和投影缓存。[连接源码](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/connection/src/client/connection.ts)
- TanStack Query 区分首次加载与后台刷新，已有数据可在刷新时继续显示，也支持自定义焦点判定及关闭焦点触发的自动刷新。MewDex 复用现有 Query 缓存，把界面状态轮询与窗口焦点、可见性关联。[后台刷新](https://tanstack.com/query/latest/docs/framework/react/guides/background-fetching-indicators) [焦点刷新](https://tanstack.com/query/latest/docs/framework/react/guides/window-focus-refetching)

MewDex 的返回窗口通知先静默核对当前身份和访问范围，再合并已进行中的状态读取。常规查询期间保留连接、执行端、账号和额度的上次结果。身份失效或访问范围变化仍清理对应缓存并重新同步。实际连接丢失会更新为断开，重试尝试使用独立恢复标记，建立连接后更新为已连接。前台节点和对话状态每 10 秒查询，模型账号及打开的额度每 30 秒查询，待完成的账号登录沿用 3 秒查询。轮询依据可见性与窗口焦点暂停，实时事件仍按现有同步协议接收。缓存复用限于当前输入会话，具体数据权限和执行仍由 Hub 及执行端负责。

## 消息渠道与输入端独立运行（2026-11-10）

用户要求依据渠道与输入端职责澄清，允许消息平台通过独立连接器接入 Hub，并在关闭 APP 后继续运行。

- DeepSeek 社区飞书插件 `dsh-lark` 的日常运行说明提供后台服务、状态、日志和启停入口。核对 `src/provision.ts` 的独立 profile、launchd / systemd 用户服务、服务配置及凭据处理。MewDex 借鉴连接服务拥有自己的生命周期与持久状态。[日常运行](https://github.com/omdsh-dev/dsh-lark#日常运行) [部署源码](https://github.com/omdsh-dev/dsh-lark/blob/main/src/provision.ts)
- 该插件直接驱动本机 Harness。MewDex 复用现有 Hub 与执行端架构，消息连接器只承担平台适配与输入，项目、会话、模型和指令调度继续由 Hub 负责。默认连接器随 Hub 服务常驻，也提供独立 `MewDex-Channel` 包，通过认证管理 WebSocket 和普通输入 API 连接远端 Hub。

设置默认选择 Hub 常驻连接器，显示实际连接服务与生命周期。Hub 连接器使用独立渠道数据库，旧 APP / Web 连接器继续可选并保留原数据。迁移时在原服务关闭渠道，再在目标服务配置凭据与允许列表，微信重新扫码。独立服务通过标准输入接受首次登录信息，保存连接凭据与固定 Hub 标识，配置文件模式独立于 APP 管道及标准输入关闭。两种部署复用平台适配、原回执恢复和普通 `client` 项目授权。

## Skills、Memory、Hooks 与 Plugins 设置（2026-11-10）

用户要求核对四类扩展能力，并补齐设置选项卡。原实现已有项目 Skills、脚本、工作流和输入菜单，原生技能仅提供已启用目录读取。

- DeepSeek 的插件管理将目录、安装来源和启停放在独立页面，配置表单使用修订校验。MewDex 采用独立设置入口、实际来源展示和保存冲突校验。[插件市场](https://deepseek-harness.github.io/deepseek-harness/en/guide/plugin-market) [配置表单](https://deepseek-harness.github.io/deepseek-harness/en/reference/subsystems/settings)
- DeepSeek 的 Codex Hooks 适配器在会话、输入、工具与停止事件运行命令，使用会话工作目录。MewDex 通过固定版 Codex 原生接口读取和配置 Hooks，命令运行在 Hub，保留内容哈希信任与管理策略。[适配器说明](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/experimental/hooks-codex/README.md)
- 社区自动记忆插件把长期上下文与设置结合。MewDex 提供可编辑的项目记忆并复用项目版本快照，同时暴露 Hub 运行时的自动记忆生成与使用开关。[自动记忆插件](https://github.com/Aik358/dsh-auto-memory)

核对固定运行时 0.162.1、修订 `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d` 的协议、特性默认值与原生配置写入。Skills、Hooks、插件目录和安装操作使用对应原生接口，项目记忆使用 MewDex 的版本化项目配置。原生 Hooks 与 Plugins 默认启用，自动记忆默认关闭，实际已有配置继续按运行时读取。独立项目记忆为空时省略字段，使旧内容哈希与历史恢复兼容。[原生协议](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/app-server-protocol/src/protocol/common.rs) [特性默认值](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/features/src/lib.rs)
