一款基于 Codex 的跨平台 Agent，用手机或聊天软件远程发起、接续电脑上的任务。正在许愿式实现中……

-----

# MewDex

[English](README.md) | **简体中文**

MewDex 由四个可组合组件组成。Input Terminal（输入终端）负责输入与阅读，Execute Terminal（执行终端）负责本机任务，Hub（管理终端）负责设备网络与路由，Hub Controller（管理终端的管理器）负责本机和远程管理。

Windows、macOS 和桌面 Linux App 包含四个组件。Android、iOS / iPadOS App 包含 Input Terminal 与 Hub Controller。电脑的本机执行、Hub 服务和连接目标由用户配置，移动端通过已有 Hub 输入任务并管理远程终端。

微信、QQ、飞书输入可在 **设置 → 消息渠道** 中配置。账号连接、允许用户和当前支持范围见 [消息渠道接入](docs/messaging.md)。

桌面 App 一同安装四个组件，打开时按配置启动本机 Hub 或 Execute Terminal，并恢复工作台连接。Input Terminal 可以连接本机或远程 Hub，Hub Controller 提供对应的管理入口。首次使用可配置本机终端，也可连接已有 Hub。网页版为可选功能，默认关闭。在 **设置 → 连接与服务 → 当前连接** 或 **本机设备 → 概览** 点击“启动网页版”后，可使用 MewDex 账号或登录密钥在浏览器中登录，也可随时点击“停止网页版”。网页与 API 共用 Hub 地址和端口，默认本机地址为 `http://127.0.0.1:47830`，随用户配置的端口变化。启用选择会保存在本机，退出桌面 APP 后，Hub 和已启用的网页版继续在后台运行。

## 获取源码与许可

源码仓库为 [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex)，主分支为 `main`：

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex 自有代码采用 [Apache License 2.0](LICENSE)。第三方代码、依赖和引擎资源保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md) 和 [依赖许可核对](docs/licensing.md)。本地配置、凭据、工具链和构建产物不进入源码仓库。

## 四个组件与平台组成

| 组件 | 职责 | 主要代码 |
| --- | --- | --- |
| Input Terminal · 输入终端 | 输入任务、继续对话、阅读结果与响应交互 | `apps/client/src/input-terminal/` |
| Execute Terminal · 执行终端 | 在本机项目中运行模型、文件操作与命令 | `internal/executor/`、`internal/engine/` |
| Hub · 管理终端 | 管理网络身份、设备、路由与事件同步 | `internal/hub/`、`internal/transport/` |
| Hub Controller · 管理终端的管理器 | 管理本机服务与已授权的远程终端 | `apps/client/src/hub-controller/`、`internal/localnode/` |

| 平台 | App 内组件 | Release 安装包 |
| --- | --- | --- |
| Windows | 四个组件 | `.exe` / `.msi` |
| macOS | 四个组件 | `.dmg` / `.pkg` |
| Linux Desktop | 四个组件 | `.AppImage` / `.deb` / `.rpm` |
| Android | Input Terminal、Hub Controller | `.apk` |
| iOS / iPadOS | Input Terminal、Hub Controller | 已签名 `.ipa`，附分发安装说明 |

组件与平台清单以 [`mewdex.product.json`](mewdex.product.json) 为准。它同时用于原生 App 能力声明和 Release 校验。发布时说明组件组成、平台、架构、构建类型、签名情况和安装方法，并提供对应安装包。流程见 [发布与安装包](docs/releases.md)。

四个组件按职责分层，桌面 App 通过 `mewdex-client` 提供 Input Terminal 和 Hub Controller，通过 `mewdex-node` 按配置运行 Hub 和 Execute Terminal。Go 服务统一来自 `cmd/mewdex`。Hub 可通过自身或其他已配对设备上的 Gateway 转发模型流量。

Windows 包体布局如下，其他桌面系统使用对应平台和架构的应用与服务程序。

```text
MewDex/
  mewdex-client.exe      # Input Terminal + Hub Controller
  mewdex-node.exe        # Hub + Execute Terminal，按配置启用
  mewdex.product.json    # 组件与平台清单
  engine/               # Execute Terminal 的完整引擎资源
    mewdex-engine.json
    bin/codex.exe
```

Codex 引擎是独立程序，MewDex 通过 `codex app-server --listen stdio://` 启动它；CLI 与 App Server 共用这个 `codex` 可执行文件，不需要 Codex 桌面 App。当前固定兼容版本为 **0.162.1**，以 [引擎锁定文件](engine.lock.json) 为准。请保留完整 `engine/` 目录，只复制 `codex.exe` 不足以部署执行端；无需替换用户全局安装的 Codex。

## 如何选择部署方式

| 你的使用方式 | 在哪里部署 | 启用什么角色 | 是否需要 Codex 引擎 |
| --- | --- | --- | --- |
| 只在一台电脑使用 | 本机桌面 App | Hub + Execute Terminal | 需要，随桌面资源准备 |
| 手机控制家里或办公室的电脑 | 电脑运行 Hub / Execute Terminal，手机安装移动 App | 电脑按配置启用 Hub + Execute Terminal，手机提供输入与管理 | 电脑需要，手机不需要 |
| 多台电脑共用一个中央服务 | 中央服务器运行 Go 后台，各执行设备运行 Go 后台及引擎 | 中央启用 Hub；执行设备启用 Executor 并加入中央网络 | 仅执行设备需要 |
| 服务器上执行任务，不开图形界面 | 服务器运行 Go 后台及引擎 | Executor，连接已有 Hub；也可同时启用 Hub | 需要 |

一个活动 Hub 协调多个 Input Terminal、Execute Terminal 和 Hub Controller。桌面包提供四个组件，由用户配置本机 Hub 和执行能力。无界面服务器也可通过同一个 Go 后台运行 Hub、Execute Terminal 或 Gateway。仅运行 Hub 时，部署 Go 服务程序和对应设备配置即可。

### 单机或电脑加手机

1. 解压与电脑系统、架构匹配的完整桌面测试包，启动 `mewdex-client.exe`（Windows）。
2. 在“配置或启动这台电脑”中创建网络，选择本机项目目录，启动后台服务。
3. 在工作台选择本机设备和项目，完成该执行设备的模型账号配置后发送任务。
4. 如需手机接入，先为 Hub 配置手机可达的 HTTPS/WSS 入口，再通过配对加入同一网络。新建 Hub 默认仅本机可达。

### 通过 SSH 提供公网入口

Hub 部署在个人电脑或私有网络时，推荐通过 SSH 反向隧道连接一台公网可达的服务器，并在服务器上配置 HTTPS 反向代理。

```text
Input Terminal / Hub Controller
  → 服务器 HTTPS 反向代理 → 服务器监听端口 → SSH 反向隧道 → Hub
```

1. 在服务器上允许 SSH 反向转发，在 Hub 设备上准备私钥和受信任的 `known_hosts` 记录。
2. 打开“本机设备 → 启动设置”，页面自动读取 `.ssh/config`。选择主机或手动填写 SSH 参数，再点击“测试连接”，检查连通性、主机密钥及私钥认证。
3. 选择服务器监听 IP 和端口并启用隧道，Hub 会维护监听并自动重连。
4. 在服务器端配置域名、证书及 Caddy/Nginx 上游，上游需能访问所选的服务器监听地址。反向代理运行在 Docker 中时，应使用容器可达的宿主机私网地址。
5. 其他 App 连接服务器的公网 HTTPS 地址。隧道面板显示 SSH 认证和监听状态，完成服务器配置后另行验证公网 HTTPS 入口。

SSH 连接测试显示耗时或失败原因。详细配置与迁移步骤见 [Hub 启动与 SSH 代理管理](docs/hub-startup-20261102.md)。

### 中央 Hub 加多个执行设备

1. 在中央服务器部署对应平台的 Go 后台，配置 Hub 角色和可达的 HTTPS/WSS 入口，再启动服务。
2. 在每台执行设备上部署 Go 后台及完整 Codex 引擎资源。桌面设备可从“配置或启动这台电脑”加入已有网络，配置执行能力和本机项目目录。
3. 为执行设备配置各自的模型账号与网络出口，启动服务并确认设备在线。
4. 将电脑或手机输入端配对到同一个 Hub，授予所需设备和项目权限，再选择目标执行设备发送任务。

无界面部署使用同一 Go 程序和设备配置文件启动服务。以下假设已经准备好对应角色、配对、项目及引擎配置；桌面包的 Go 程序名为 `mewdex-node`：

```text
mewdex serve -config /path/to/device.json
```

中央服务器托管 Hub，各个 Execute Terminal 分别保留自己的项目、模型账号和任务执行。在服务器的 HTTPS 反向代理中将上游指向 Hub 监听地址，电脑和手机 App 连接这个公网入口。

### 经其他设备转发模型请求

Hub 可将模型 API 流量路由到具备所需网络出口的已配对 Gateway 设备。在各自的设备配置中填写以下字段。

| 设备 | 配置 |
| --- | --- |
| Hub | `gateway.forwardDeviceId` 填写出口设备编号，`gateway.allowedHosts` 列出允许访问的模型域名 |
| 出口设备 | 通过 `hubUrl` 和已配对的 `nodeTokenFile` 连接 Hub，配置 `gateway.allowedHosts`，可选用 `gateway.upstreamProxyUrl` 接入已有代理 |
| Execute Terminal | 启用 `engine.useHubGateway: true`，并为其配对身份授予 Gateway 使用权限 |

出口设备可以配置 `hub: false`、`executor: false`，单独运行 Gateway，也可同时提供本机任务执行。它的设备编号须与 Hub 的 `gateway.forwardDeviceId` 一致。Hub 和出口设备各自检查允许的目标域名。对于同一个 Gateway，`gateway.forwardDeviceId` 和 `gateway.upstreamProxyUrl` 二选一。

```text
模型 API 流量
  Execute Terminal → Hub WSS → 已配对的 Gateway → 可选本地代理 → 模型 API
```

控制连接和一次性数据连接共用 Hub 的 HTTPS/WSS 入口。模型 TLS 保持端到端加密，各执行设备使用自己的模型凭据。指定出口设备在线时才能建立新的模型连接。在执行设备运行 `mewdex doctor -config /path/to/device.json -gateway` 可探测模型 TLS 连通性。配置与授权细节见 [架构说明](docs/architecture.md)。

### 任务实际在哪里执行

```text
电脑 / 手机输入端 → Hub → 目标设备的 Executor → 本机 Codex App Server
                                                   ↓
                                            该设备的项目文件与命令
```

工作区由执行设备上配置的项目目录决定，通过 `cwd` 传给引擎，与 `codex.exe` 的存放位置无关。切换输入端继续同一对话，执行位置保持不变；同一引擎可管理多个项目的对话。同一执行器内，运行中或结果未知的会话独占项目目录及其父子目录，不重叠的目录可并行；当前不自动创建 worktree，也没有跨执行器进程的目录锁。

关闭输入窗口后后台任务继续运行。后台服务和引擎资源会按内容哈希保存到运行资源目录，实际进程路径以本机状态为准。配置、配对凭据与历史属于设备数据，不能作为通用程序包复制给其他设备。

## 技术与边界

- Go：设备服务、Hub、Codex stdio 适配与请求出口。
- React / TypeScript：桌面、手机与网页共享的输入端及独立通信 SDK。
- Tauri 2：平台外壳，不承担网络路由或任务执行业务。
- SQLite：配置以外的持久状态；附件独立存储。
- 官方 Codex App Server：复用引擎，固定兼容基线；不更改用户全局 Codex 配置或凭据。

界面关闭不等于停止任务，切换输入端不等于迁移执行端。模型网络转发不等于共享模型账号授权。

开发状态、已验证证据和剩余交付风险见 [实施记录](docs/implementation.md)。输入端接入见 [协议](docs/protocol.md)，模块边界见 [架构](docs/architecture.md)。开发中的产物不能视为最终候选或验收完成。

设备加密导出、两阶段恢复和当前安装升级的范围与证据见 [设备数据生命周期](docs/data-lifecycle.md)。管理层支持准备、审阅结果查询、配置切换及重新配对；桌面界面已接入后台任务、进度与取消，完整原生界面流程仍待实际检查。

## 当前开发入口

运行 `npm install` 和 `npm run build:ui` 后，Go 编译会把同一份网页界面嵌入 Hub。桌面构建脚本自动完成此步骤。`npm run dev` 用于前端热更新开发。

```powershell
go run ./cmd/mewdex init
go run ./cmd/mewdex doctor -engine
go run ./cmd/mewdex serve
```

另一个终端运行 `npm install` 和 `npm run dev`，通过本机 `http://127.0.0.1:1420` 查看共享输入端。首次 `init` 将一次性管理员配对码写入 `.local/pairing-code`，有效期十分钟；重新生成可使用 `go run ./cmd/mewdex invite -role admin`。不要将配对码或 `.local` 目录纳入版本控制。

Windows 原生开发入口为 `scripts/dev-native.ps1`；脚本准备 Go 服务与固定版官方引擎资源，`-Check` 进行原生编译检查，不启动窗口或服务。当前项目自用 Rust 工具链位于 `.local/toolchains`，没有修改全局 PATH。移动端原生依赖和生产分发仍在实施中。

桌面工作台的“配置或启动这台电脑”入口可创建网络、加入已有网络、选择 Hub 监听端口和启动服务。已有配对连接失败时也能进入本机管理，不必先删除配对记录。配置保存在应用自己的数据目录，服务后台运行；登录后可在工作区管理中实时添加项目与整理名称分类。修改本机 Hub 监听端口前需要停止服务。新建 Hub 默认仅本机可达，其他设备接入前需要配置 HTTPS 入口。手机和浏览器提供 Input Terminal 与远程 Hub Controller。桌面 App 的 Hub Controller 还通过原生管理接口提供本机配置、启停、自启动与数据维护。

“本机设备 → 启动设置”提供独立的开机自启动和 SSH 反向转发开关。Windows 登录后静默启动 MewDex Hub 到托盘，安装器询问是否启用登录自启动，首次安装默认关闭，升级保留原选择。macOS 和 Linux 使用各自的平台服务入口。

`scripts/build-desktop-development.ps1` 在独立输出目录编译一个 Windows Debug 程序，不启动界面或生成安装器。构建状态保存在 `.state/desktop-development-build.json`，中断后先检查状态，避免重复构建。

工作台仍在运行时可用 `scripts/build-desktop-development.ps1 -SideBySide`，将下一批开发程序编译到 `.state/desktop-development-next-target`，保留原运行目录；编译不退出或重开工作台，也不接替后台服务。实际运行路径继续以 `.state/runtime-processes.json` 为准。

Windows 开发构建可通过 `scripts/export-windows-development.py` 导出便携测试包，或用 `scripts/build-windows-installer.mjs` 生成安装器。平台产物和发布校验见 [发布与安装包](docs/releases.md)。此前的界面实现与验证记录见 [Windows 改造](docs/windows-redesign-20261030.md) 和 [项目与工具栏](docs/windows-projects-toolbar-20261030.md)。

Android 使用 `scripts/mobile-android.ps1` 的 `Check`、`Init`、`BuildArm64` 操作，独立 SDK 由 `scripts/prepare-android-deps.ps1` 准备。iOS 在 Mac 上使用 `scripts/mobile-ios.sh`，项目内依赖由 `scripts/remote/prepare-ios-deps.sh` 准备。已有的 Android 真机与 iOS 模拟器检查见 [实施记录](docs/implementation.md)。

协议类型由 `go run ./cmd/protocol-gen` 从 Go 类型生成到 TypeScript SDK，桌面本机协议生成到 `apps/client/src/hub-controller/localProtocol.ts`。前端通过 SDK 接入网络。提交改动前运行以下检查。

```sh
npm run typecheck
npm run test:sdk
npm run test:client
npm run test:release
npm run build:ui
go test ./...
```

`.local` 保存开发配置、配对凭据和设备状态；`.state` 保存开发二进制、协议快照与风险验证证据，均被 Git 忽略。当前没有最终候选安装包。
