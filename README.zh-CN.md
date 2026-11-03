一款基于 Codex 的跨平台 Agent，用手机或聊天软件远程发起、接续电脑上的任务。正在许愿式实现中……

-----

# MewDex

[English](README.md) | **简体中文**

跨平台 AI 工作台。输入端与执行端独立，任意已授权输入端可以向网络内任意已授权执行端发起或继续对话。手机仅作为输入端；电脑可同时提供输入和执行能力；服务器支持无界面执行。

微信、QQ、飞书输入可在 **设置 → 消息渠道** 中配置。账号连接、允许用户和当前支持范围见 [消息渠道接入](docs/messaging.md)。

桌面 APP 与中央后端一同安装。打开 APP 会启动已配置的本机 Hub，并恢复工作台连接，首次使用会进入本机设置。网页版为可选功能，默认关闭。在 **设置 → 连接与服务 → 当前连接** 或 **本机设备 → 概览** 点击“启动网页版”后，可使用 MewDex 账号或登录密钥在浏览器中登录，也可随时点击“停止网页版”。网页与 API 共用 Hub 地址和端口，默认本机地址为 `http://127.0.0.1:47830`，随用户配置的端口变化。启用选择会保存在本机，退出桌面 APP 后，Hub 和已启用的网页版继续在后台运行。

## 获取源码与许可

源码仓库为 [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex)，主分支为 `main`：

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex 自有代码采用 [Apache License 2.0](LICENSE)。第三方代码、依赖和引擎资源保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md) 和 [依赖许可核对](docs/licensing.md)。本地配置、凭据、工具链和构建产物不进入源码仓库。

## 包体里有什么

MewDex 由输入界面、Go 后台服务和 Codex 引擎资源三部分组成。当前提供的是开发测试产物，正式安装器与生产分发尚未完成。

| 组成 | Windows 桌面包中的位置 | 用途 |
| --- | --- | --- |
| 输入界面 | `mewdex-client.exe` | 桌面工作台，查看对话、发送任务、处理审批和管理本机设备 |
| Go 后台服务 | `mewdex-node.exe` | 按配置承担中央后端 Hub、执行后端 Executor，以及可选的网络出口 Gateway |
| Codex 引擎及完整资源 | `engine/` | 在执行设备上运行模型回合、文件操作和终端命令 |

Windows 桌面开发包的主要文件布局如下，其他平台使用相应平台与架构的程序：

```text
MewDex/
  mewdex-client.exe
  mewdex-node.exe
  engine/
    mewdex-engine.json    # 引擎版本与文件校验清单
    bin/
      codex.exe
      ...
    ...                    # 平台伴随程序、沙箱资源及许可证
```

**Hub 和 Executor 共用同一个 Go 后端程序，通过配置选择角色。** 开发命令中的 `mewdex` 与桌面包中的 `mewdex-node` 均来自 `cmd/mewdex`，不是两套后端。Windows、Mac、Linux 及不同 CPU 架构分别构建，部署时需要选择与目标设备匹配的产物。

Codex 引擎是独立程序，MewDex 通过 `codex app-server --listen stdio://` 启动它；CLI 与 App Server 共用这个 `codex` 可执行文件，不需要 Codex 桌面 App。当前固定兼容版本为 **0.162.1**，以 [引擎锁定文件](engine.lock.json) 为准。请保留完整 `engine/` 目录，只复制 `codex.exe` 不足以部署执行端；无需替换用户全局安装的 Codex。

## 如何选择部署方式

| 你的使用方式 | 在哪里部署 | 启用什么角色 | 是否需要 Codex 引擎 |
| --- | --- | --- | --- |
| 只在一台电脑使用 | 本机桌面包 | Hub + Executor | 需要，随桌面资源准备 |
| 手机控制家里或办公室的电脑 | 电脑运行后台，手机安装输入端 | 电脑启用 Hub + Executor，手机配对接入 | 电脑需要，手机不需要 |
| 多台电脑共用一个中央服务 | 中央服务器运行 Go 后台，各执行设备运行 Go 后台及引擎 | 中央启用 Hub；执行设备启用 Executor 并加入中央网络 | 仅执行设备需要 |
| 服务器上执行任务，不开图形界面 | 服务器运行 Go 后台及引擎 | Executor，连接已有 Hub；也可同时启用 Hub | 需要 |

当前采用一个活动 Hub、多输入端、多执行端。上述是角色组合；当前桌面开发包会同时准备 Go 后台和完整引擎资源，尚未提供按 Hub-only、Executor-only 自动裁剪的独立发布包。纯 Hub 服务在运行上不需要图形界面或 Codex 引擎。

### 单机或电脑加手机

1. 解压与电脑系统、架构匹配的完整桌面测试包，启动 `mewdex-client.exe`（Windows）。
2. 在“配置或启动这台电脑”中创建网络，选择本机项目目录，启动后台服务。
3. 在工作台选择本机设备和项目，完成该执行设备的模型账号配置后发送任务。
4. 如需手机接入，先为 Hub 配置手机可达的 HTTPS/WSS 入口，再通过配对加入同一网络。新建 Hub 默认仅本机可达。

### 中央 Hub 加多个执行设备

1. 在中央服务器部署对应平台的 Go 后台，配置 Hub 角色和可达的 HTTPS/WSS 入口，再启动服务。
2. 在每台执行设备上部署 Go 后台及完整 Codex 引擎资源。桌面设备可从“配置或启动这台电脑”加入已有网络，配置执行能力和本机项目目录。
3. 为执行设备配置各自的模型账号与网络出口，启动服务并确认设备在线。
4. 将电脑或手机输入端配对到同一个 Hub，授予所需设备和项目权限，再选择目标执行设备发送任务。

无界面部署使用同一 Go 程序和设备配置文件启动服务。以下假设已经准备好对应角色、配对、项目及引擎配置；桌面包的 Go 程序名为 `mewdex-node`：

```text
mewdex serve -config /path/to/device.json
```

Gateway 是可选的模型网络出口。执行设备可直接出网，也可设置 `engine.useHubGateway: true` 经 Hub 转发；转发不共享模型账号授权。不提供 NAT 穿透服务，远程设备需要能够连接 Hub。

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

桌面工作台的“配置或启动这台电脑”入口可创建网络、加入已有网络、选择 Hub 监听端口和启动服务。已有配对连接失败时也能进入本机管理，不必先删除配对记录。配置保存在应用自己的数据目录，服务后台运行；登录后可在工作区管理中实时添加项目与整理名称分类。修改本机 Hub 监听端口前需要停止服务。新建 Hub 默认仅本机可达，其他设备接入前需要配置 HTTPS 入口。手机与浏览器只显示输入工作台，不提供这些本机服务命令。

“本机设备 → 启动设置”提供独立的开机自启动和公网反向代理开关。Windows 登录后静默启动 MewDex Hub 到托盘；SSH 隧道由 Hub 进程直接维护，保存设置后随 Hub 启动并自动重连。关闭自启动保留当前服务，关闭代理保留参数并断开公网入口。Windows 安装器明确询问自启动选择，首次安装默认关闭，升级保留原选择；Mac/Linux 沿用原平台服务入口。真实重启登录触发尚未执行。详见 [Hub 启动与代理管理](docs/hub-startup-20261102.md)。

`scripts/build-desktop-development.ps1` 在独立输出目录编译一个 Windows Debug 程序，不启动界面或生成安装器。构建状态保存在 `.state/desktop-development-build.json`，中断后先检查状态，避免重复构建。

工作台仍在运行时可用 `scripts/build-desktop-development.ps1 -SideBySide`，将下一批开发程序编译到 `.state/desktop-development-next-target`，保留原运行目录；编译不退出或重开工作台，也不接替后台服务。实际运行路径继续以 `.state/runtime-processes.json` 为准。

本轮 Windows 桌面改造测试包位于 `dist/windows/MewDex-Windows-x64-20261030-033735` 与同名 `.zip`。连续滚动、工作区创建与分类、文件默认打开、输入区控制、账号额度、外观及侧栏拖宽的实现和切换说明见 [Windows 改造](docs/windows-redesign-20261030.md)。项目与输入区的后续修订见 [项目和工具栏](docs/windows-projects-toolbar-20261030.md)。创建本机项目时可自动更新旧后台；当前运行服务未提前切换。

Android 使用 `scripts/mobile-android.ps1` 的 `Check`、`Init`、`BuildArm64` 操作；独立 SDK 由 `scripts/prepare-android-deps.ps1` 准备。最新 ARM64 Debug 测试 APK 位于 `dist/android/MewDex-Android-arm64-20261030-022209.apk`，已通过签名和 16 KiB 对齐检查。在 Android 15 真机上保留数据覆盖安装，确认配对保留、设备切换、系统栏主题和键盘布局；通过公网向 Windows 实际完成两轮模型对话，并确认应用进程重开后的配对、历史恢复与继续对话。通知和附件仍未在该真机上验证。iOS 使用 Mac 上的 `scripts/mobile-ios.sh`，缺少的项目内工具由 `scripts/remote/prepare-ios-deps.sh` 准备；模拟器已实际完成输入配对、向 Windows 发送模型文本、凭据与历史恢复，并检查键盘布局。iOS 真机和生产签名仍待验证。

远端执行器配置 `engine.useHubGateway: true` 后，通过 Hub 的现有 WSS 入口转发模型 TLS，无需单独开放模型代理端口。它使用自己的节点授权；模型子进程只拿到临时回环代理密码。`doctor -gateway` 可做不含模型凭据和推理的官方 TLS 连通探测。

已有 SSH 与 HTTPS 反向代理的自托管部署，可使用独立的 [回程维护组件](docs/backhaul.md) 管理连接重试、状态与本机自动启动。当前常驻组件仍在功能开发，真实 SSH 与生产接替待授权；不提供设备打洞服务。

协议类型由 `go run ./cmd/protocol-gen` 从 Go 类型生成到 TypeScript SDK；独立的桌面本机协议生成到 `apps/client/src/localProtocol.ts`。前端通过 SDK 接入网络，不直接调用 Codex。`npm run typecheck` 检查共享 SDK 与输入端类型；Go 开发检查按修改范围运行 `go test`。

`.local` 保存开发配置、配对凭据和设备状态；`.state` 保存开发二进制、协议快照与风险验证证据，均被 Git 忽略。当前没有最终候选安装包。
