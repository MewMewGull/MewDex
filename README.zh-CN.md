一款基于 Codex 的跨平台 Agent，用手机或聊天软件远程发起、接续电脑上的任务。正在许愿式实现中……

-----

# MewDex

[English](README.md) | **简体中文**

MewDex 分别发布 APP、网页输入端、Hub 和执行端。APP 与网页端共用输入工作台、对话、项目和网络管理功能。桌面 APP 连接 QQ、微信和飞书，将收到的消息处理后提交给 Hub。Hub 负责模型、会话和调度，执行端主动通过 WebSocket 连接 Hub。

消息渠道在 **设置 → 消息渠道** 中选择负责连接的在线 APP，并配置平台凭据及允许用户。使用渠道时需保持该 APP 运行。连接方式见 [消息渠道接入](docs/messaging.md)。

## 获取源码与许可

源码仓库为 [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex)，主分支为 `main`：

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex 自有代码采用 [Apache License 2.0](LICENSE)。第三方代码、依赖和引擎资源保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md) 和 [依赖许可核对](docs/licensing.md)。本地配置、凭据、工具链和构建产物不进入源码仓库。

## 独立发布与部署

| 产物 | 内容 | Windows x64 包名 |
| --- | --- | --- |
| APP | 输入终端、网络管理界面、桌面消息连接器 | `MewDex-Windows-x64` |
| 网页输入端 | 与 APP 共用的输入界面、独立网页服务器 | `MewDex-Web-Windows-x64` |
| Hub | 中央 API、模型引擎、会话和项目调度 | `MewDex-Hub-Windows-x64` |
| 执行端 | WebSocket 节点、本地沙箱、独立网页控制面板 | `MewDex-Executor-Windows-x64` |

APP 使用 Tauri 界面，与网页输入端共用 React 和 SDK。桌面 APP 包中包含 `mewdex-client.exe` 和 `mewdex-input-bridge.exe`。Hub 与执行端分别携带完整 `engine/` 目录。模型引擎版本以 [引擎锁定文件](engine.lock.json) 为准，组件清单见 `mewdex.product.json`。

### 构建与导出

```powershell
npm run build:app:windows
npm run package:app:windows
npm run package:services
```

Windows 输出位于 `dist/windows`。APP 使用 `MewDex-Windows-x64` 目录、同名 ZIP 和安装程序。`package:hub`、`package:web`、`package:executor` 可单独生成服务包，支持 `--target=linux:x64` 等目标。完整流程见 [发布与安装包](docs/releases.md)。

### 启动 Hub

```powershell
.\mewdex-hub.exe init -config .local/hub/device.json
.\mewdex-hub.exe account -config .local/hub/device.json
.\mewdex-hub.exe serve -config .local/hub/device.json
```

`account` 从标准输入读取用户名和密码 JSON。默认监听 `127.0.0.1:47830`。在 `device.json` 中配置监听地址、网页公开 Origin 对应的 `allowedOrigins` 和模型出口，再发布 HTTPS/WSS 入口。

### 启动执行端

```powershell
.\mewdex-executor.exe panel -root .local/executor
```

打开 `http://127.0.0.1:47832`，使用 `.local/executor/panel-token` 登录。填写 Hub 地址与从输入端生成的执行端配对码，配置本机目录，启动执行服务。执行端使用自己的配置、身份和数据库，通过 WebSocket 连接 Hub。面板只监听本机地址，可通过 SSH 转发访问远程设备的面板。

### 启动网页输入端

```powershell
.\mewdex-web.exe -hub-url https://your-hub.example.com -listen 127.0.0.1:47831
```

网页服务器与 `ui/` 一同部署。它将 `/v1/` 和 `/health` 转发给指定 Hub，包括 WebSocket 升级，其余请求读取独立网页资源。将网页公开 Origin 加入 Hub 的 `allowedOrigins` 后，从网页服务地址登录。

同一台电脑可分别部署 Hub 与执行端，两者使用独立目录和单角色配置。旧版合并配置与数据库保留在原处。旧 Hub 中的消息渠道需要在选定 APP 上重新配置，渠道凭据和收发状态由该 APP 保存。

### Hub 项目与连接

一个项目包含名称、分类和项目目录组。目录引用由执行设备编号和该设备上的持久目录绑定组成。同一项目可以包含一台机器上的多个目录，也可以包含多台机器上的目录。项目元数据保存在 Hub 数据库。

| 组件 | 职责 |
| --- | --- |
| Hub | 项目目录组、对话、ChatGPT 登录、模型推理、任务编排和结果历史 |
| 输入端 | 桌面、移动端、网页、QQ、微信或飞书对话入口 |
| 执行端 | Hub 连接、本机目录绑定、沙箱命令和文件快照 |

只有 Hub 机器需要访问 OpenAI。各执行端连接 Hub，这些连接构成 MewDex 逻辑网络。Hub 可以使用本机网络或这台机器可访问的 HTTP 代理，通过 Hub 部署配置中的 `engine.proxyUrl` 设置。

### 工作如何执行

```text
桌面 / 手机 / 网页 / QQ / 微信 / 飞书
                    ↓
         Hub 项目、对话和模型引擎
                    ↓
              已配对的控制连接
           ↙        ↓         ↘
        Windows    macOS     Linux 执行端
              项目目录与本机命令
```

同一段对话可以调度项目组中的多个目录。Hub 将模型工具调用解析为目标执行端上的指令，执行端调用固定版本的本地沙箱，回传持久任务状态和输出。模型账号在 Hub 工作台的设置中统一登录。

执行指令按照当前策略访问同一项目在该机器上的目录组。运行中或结果未确认的指令保留重叠目录的占用，重连按原指令和任务编号查询。项目文件面板可以选择目录组中的目录，文件快照通过 Hub 回传。

首次迁移会把已有设备目录分别导入 Hub 项目，之后可在项目设置中组合目录引用，并在已有消息渠道设置中重新选择 Hub 项目。原设备对话可从历史区域查阅。Hub 和执行端需更新到支持 `hub-execution` 能力的版本，进行新的跨机器工作。

## 技术与边界

- Go 负责执行端服务、Hub 调度和 Codex stdio 适配。
- React / TypeScript：桌面、手机与网页共享的输入端及独立通信 SDK。
- Tauri 2：平台外壳，不承担网络路由或任务执行业务。
- SQLite：配置以外的持久状态；附件独立存储。
- 官方 Codex App Server：复用引擎，固定兼容基线；不更改用户全局 Codex 配置或凭据。

界面关闭后，Hub 与执行端继续管理已启动的任务。输入端可从不同入口继续同一 Hub 对话。

开发状态、已验证证据和剩余交付风险见 [实施记录](docs/implementation.md)。输入端接入见 [协议](docs/protocol.md)，模块边界见 [架构](docs/architecture.md)。开发中的产物不能视为最终候选或验收完成。

设备加密导出、两阶段恢复和当前安装升级的范围与证据见 [设备数据生命周期](docs/data-lifecycle.md)。管理层支持准备、审阅结果查询、配置切换及重新配对；桌面界面已接入后台任务、进度与取消，完整原生界面流程仍待实际检查。

## 当前开发入口

`npm run build:ui` 构建独立网页输入界面，APP 界面由 `build:app` 输出到独立目录。Hub 构建只使用 Go 源码。`npm run dev` 用于共享界面的前端热更新开发。

```powershell
go run ./cmd/mewdex init
go run ./cmd/mewdex doctor -engine
go run ./cmd/mewdex serve
```

另一个终端运行 `npm install` 和 `npm run dev`，通过本机 `http://127.0.0.1:1420` 查看共享输入端。首次 `init` 将一次性管理员配对码写入 `.local/pairing-code`，有效期十分钟；重新生成可使用 `go run ./cmd/mewdex invite -role admin`。不要将配对码或 `.local` 目录纳入版本控制。

Windows 原生开发入口为 `scripts/dev-native.ps1`，脚本准备 APP 消息连接器。`-Check` 构建 APP 界面并检查原生编译。项目自用 Rust 工具链位于 `.local/toolchains`。移动端原生依赖和生产分发仍在实施中。

桌面、手机和浏览器通过已有 Hub 登录并管理项目、对话与设备授权。执行端的本机配置、服务启停、目录与重新配对由其独立网页面板处理。

Windows APP 安装器独立保存 APP 登录自启动选择。Hub 与执行端的服务和网络入口由各自部署管理。

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
