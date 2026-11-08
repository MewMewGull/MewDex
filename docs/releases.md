# 发布与安装包

MewDex 的组件和平台组成由根目录 `mewdex.product.json` 定义。平台原生能力声明和发布校验共用这份清单。

| 系统 | App 组成 | 安装包 |
| --- | --- | --- |
| Windows | Input Terminal、Hub Controller、Message Connector | `.exe` / `.msi` |
| macOS | Input Terminal、Hub Controller、Message Connector | `.dmg` / `.pkg` |
| Linux Desktop | Input Terminal、Hub Controller、Message Connector | `.AppImage` / `.deb` / `.rpm` |
| Android | Input Terminal、Hub Controller | `.apk` |
| iOS / iPadOS | Input Terminal、Hub Controller | 已签名 `.ipa` |

APP、Hub、网页输入端和执行端分别打包。APP 与网页端共用输入界面。桌面 APP 随包提供消息连接器。Hub 携带模型引擎，Windows x64、macOS ARM64 和 Linux x64 执行端携带独立沙箱，其他执行端目标目前携带完整引擎。执行端包包含其独立网页控制面板。

## 准备平台产物

先为本次发布设置版本，再构建全部产物。此命令同步前端工作区、Tauri、Cargo 与锁文件中的应用版本。Release Tag 使用同一版本加上 `v` 前缀，发布准备命令会核对版本。

```sh
npm run version:app -- 0.1.1-preview.20261104
```

Windows 使用 `scripts/build-desktop-development.ps1 -SideBySide` 构建开发版，再执行下列命令生成安装程序。

```powershell
python scripts/export-windows-development.py
node scripts/build-windows-installer.mjs dist/windows/MewDex-Windows-x64
```

Windows APP 使用 `dist/windows/MewDex-Windows-x64`、`MewDex-Windows-x64.zip` 和 `MewDex-Windows-x64.exe`。导出脚本核对新构建的 APP 与消息连接器，并移出旧导出目录中的合并服务资源。

独立服务与网页输入端可一起导出，也可分别导出。

```sh
npm run package:services
npm run package:hub
npm run package:executor
npm run package:web
npm run package:services -- --target=linux:x64
```

支持 Windows、macOS、Linux 的 x64 和 ARM64 服务目标。各包包含 `component.json`、`START-HERE.txt` 和第三方许可。网页包携带 `ui/`，Hub 携带 `engine/`，Windows x64、macOS ARM64 和 Linux x64 执行端携带 `sandbox/`，其他执行端目标目前携带 `engine/`。默认生成对应目录与 ZIP，导出报告位于目标目录的 `component-packages.json`。服务包固定运行角色，执行端通过 `mewdex-executor panel` 配置 Hub WebSocket 连接和本机目录。

独立沙箱的构建使用目标平台的项目内 Rust 工具链。`scripts/prepare-sandbox-resources.py` 按 `tools/sandbox.lock.json` 校验源码归档、源码文件和许可材料，组件打包命令会自动调用它。跨平台打包需要先将目标平台生成的 `sandbox/` 放入对应的 `.state/sandbox-resources/<target>/`，脚本会核对当前源码指纹、目标平台和全部资源哈希。运行时目录已由执行端管理的已有安装可使用设备数据管理中的准备升级与应用升级流程切换运行时，升级计划保留原任务记录和网络身份。引用外部 APP 引擎的旧配置需要先完成运行时归属迁移。

已有 CLI 安装可用新执行端包的 `upgrade` 命令迁移原配置。先确认任务已停止并退出原服务，再依次运行 `prepare`、检查结果和 `apply`。各阶段使用同一个 32 位十六进制升级编号和原设备编号，`status` 可读取同一升级记录。原配置的文件名保持可供原服务入口继续使用。旧版共享 Codex CLI 仅接受已支持的历史版本，数据与网络凭据需要位于该配置所属的安装目录中。

```sh
./mewdex-executor upgrade -config /path/to/install/device.json -phase prepare -id 0123456789abcdef0123456789abcdef -expected-device-id existing-device
./mewdex-executor upgrade -config /path/to/install/device.json -phase status -id 0123456789abcdef0123456789abcdef -expected-device-id existing-device
./mewdex-executor upgrade -config /path/to/install/device.json -phase apply -id 0123456789abcdef0123456789abcdef -expected-device-id existing-device
```

Linux x64 沙箱使用 MUSL 静态目标构建，并携带校验固定的 Bubblewrap。系统仍需允许用户、挂载和 PID 命名空间，以及固定沙箱使用的内核安全接口。Bubblewrap 的许可、固定修订中的源码与构建文件、完整上游归档的来源和哈希随包放在 `sandbox/licenses/bubblewrap/`。

启用 AppArmor 非特权用户命名空间限制的 Linux 主机提供 `sandbox-setup` 准备入口。默认输出只读预览，管理员核对固定辅助程序哈希与完整授权配置后显式应用，再以执行服务账号运行 `doctor -engine` 检查本机沙箱。步骤见 [Linux 沙箱部署](linux-sandbox.md)。

macOS 和 Linux 在对应系统准备依赖和桌面资源后构建。平台配置默认生成 macOS DMG 和 Linux AppImage，可通过 Tauri 的 `--bundles` 选择其他支持的格式。

```sh
node scripts/prepare-desktop-binaries.mjs
npm run tauri --workspace @mewdex/client -- build --ci -- --locked
```

Android 使用 `scripts/mobile-android.ps1` 准备项目并构建 APK。iOS 在 Mac 上使用已配置的 Apple 签名归档并导出 IPA，发布定义需要说明对应的安装分发方式。Debug 构建用于预览版，正式版使用 Release 构建。

## 发布定义

为本次发布保存 JSON 定义。下面是 Windows 开发预览版的示例，安装包路径应指向本次构建的实际文件。

```json
{
  "tag": "v0.1.1-preview.20261104",
  "prerelease": true,
  "summary": "本次版本变更说明。",
  "platforms": ["windows"],
  "artifacts": [
    {
      "os": "windows",
      "arch": "x64",
      "kind": "installer",
      "distribution": "app",
      "configuration": "Debug",
      "signed": false,
      "file": "dist/windows/MewDex-Windows-x64.exe",
      "components": ["input-terminal", "hub-controller", "message-connector"]
    }
  ]
}
```

`distribution` 可为 `app`、`hub`、`web` 或 `executor`，缺省为 `app`。服务与网页包使用 `kind: "portable"`，组件分别为 `["hub"]`、`["input-terminal", "hub-controller", "message-connector"]` 和 `["execute-terminal", "executor-panel"]`。预览版可单独发布这些便携包。发布 APP 时，所声明平台仍需 APP 安装包。正式版覆盖五个系统的 APP 安装包。

移动端的组件清单为 `input-terminal` 和 `hub-controller`。iOS 产物还需 `signed: true` 和 `installation`，后者描述签名所对应的分发及安装方式。架构、构建类型和签名声明必须与实际产物一致。

## 校验与生成说明

```sh
npm run release:prepare -- path/to/release.json --check
npm run release:prepare -- path/to/release.json
```

校验检查平台覆盖、组件组成、安装包扩展名、文件存在与路径范围、构建及签名声明，并计算文件大小和 SHA-256。各平台的安装、运行与签名验证由相应平台的构建和验收完成。

生成文件位于 `dist/releases/<tag>/`。

- `release-notes.md` 说明四个组件、桌面与移动端的差异、本次平台包、安装方式、架构、构建类型、签名声明和校验值。
- `release-manifest.json` 记录本次安装包和附加包的组件及文件信息。
- `SHA256SUMS` 提供发布附件的校验值。

创建 GitHub Release 时采用生成的说明，并上传 manifest 中列出的全部安装包、附加包、`release-manifest.json` 与 `SHA256SUMS`。Tag 与所有平台产物对应同一版本源码。平台构建未完成时，预览版发布定义列出已完成的平台。

## 执行端服务恢复

Windows 执行端启用登录自启动后，同一任务登记还包含每分钟一次的恢复触发。健康服务通过任务的单实例规则和安装租约保持唯一实例。恢复启动继续核对原有的安装所有权、程序和配置绑定，以及执行清理状态。

用户手动停止服务后，自动恢复在当前 Windows 登录会话内暂停。明确启动服务会解除暂停，新的 Windows 登录会话仍按登录自启动选择处理。暂停标记绑定系统启动身份和登录身份，保存在该安装的 `startup.json` 中。系统实际触发时间受任务计划程序和登录状态影响。

## 应用内更新

“设置 → 应用更新”提供“自动检查更新”开关、稳定版和预览版通道、手动检查、发布说明及忽略单个版本。设备配对前也可从连接页进入。自动检查默认开启，可在应用设置中关闭。开启后，启动和恢复使用时每天至多检查一次，失败后至少间隔一小时重试。发现新版时在应用左下角显示“有新版本可下载”，点击“查看并下载”可阅读说明并下载安装。更新通道默认为稳定版，手动检查可随时使用。下载和安装由用户确认。

检测直接读取本仓库的 GitHub Releases，匿名 API 不可用或限流时回退到 Release Atom 订阅。版本按 SemVer 优先级比较。预览通道同时接收稳定版，草稿和旧版本会过滤，构建元数据变化不触发升级。

发布附件必须上传 `release-manifest.json`，应用根据其中的系统、架构、安装包类型、大小和 SHA-256 选择及校验下载。缺少清单或本机安装包的版本提供 Release 页面入口。清单中的 `summary` 用于 Atom 回退时显示更新说明。

- Windows APP 安装包由本仓库的 NSIS 脚本生成，清单标记 `distribution: "app"` 和 `updateMode: "windows-nsis"`。应用下载校验后，点击“退出并安装更新”会等待当前 APP 退出，在原目录安装并重新打开工作台。安装程序单独管理 APP 的登录自启动选择。
- macOS 和 Linux 桌面端支持应用内下载及校验，之后打开 DMG、PKG 或系统安装程序。AppImage 打开下载目录，按平台安装方式替换。
- Android 打开对应架构 APK 的官方 Release 下载地址，由系统安装流程处理。iOS 打开该 Release 中与签名及分发方式对应的安装说明。

已有清单缺少 `updateMode` 时，桌面端下载校验后打开系统安装程序。首次部署带更新功能的版本，使用对应平台的安装包完成安装。
