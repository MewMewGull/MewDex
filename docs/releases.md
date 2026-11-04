# 发布与安装包

MewDex 的组件和平台组成由根目录 `mewdex.product.json` 定义。平台原生能力声明和发布校验共用这份清单。

| 系统 | App 组成 | 安装包 |
| --- | --- | --- |
| Windows | Input Terminal、Execute Terminal、Hub、Hub Controller | `.exe` / `.msi` |
| macOS | Input Terminal、Execute Terminal、Hub、Hub Controller | `.dmg` / `.pkg` |
| Linux Desktop | Input Terminal、Execute Terminal、Hub、Hub Controller | `.AppImage` / `.deb` / `.rpm` |
| Android | Input Terminal、Hub Controller | `.apk` |
| iOS / iPadOS | Input Terminal、Hub Controller | 已签名 `.ipa` |

桌面 App 随包提供本机服务程序和完整执行引擎，Hub 与 Execute Terminal 的启用由设备配置决定。移动端通过 Hub Controller 使用远程 Hub 的管理接口。无界面服务器可单独运行 Hub 或 Execute Terminal 的 Go 服务。

## 准备平台产物

先为本次发布设置版本，再构建全部产物。此命令同步前端工作区、Tauri、Cargo 与锁文件中的应用版本。Release Tag 使用同一版本加上 `v` 前缀，发布准备命令会核对版本。

```sh
npm run version:app -- 0.1.1-preview.20261104
```

Windows 使用 `scripts/build-desktop-development.ps1 -SideBySide` 构建开发版，再执行下列命令生成安装程序。

```powershell
node scripts/build-windows-installer.mjs .state/desktop-development-next-target/debug
```

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
      "configuration": "Debug",
      "signed": false,
      "file": "dist/windows/MewDex-Windows-x64.exe",
      "components": ["input-terminal", "execute-terminal", "hub", "hub-controller"]
    }
  ]
}
```

正式版的 `platforms` 覆盖五个系统，每个系统至少一个对应架构的安装包。预览版可选择本次已提供的平台，生成的说明会同时列出其他平台的待构建状态。ZIP 和 tar.gz 可作为附加便携包，每个已声明的平台仍需安装包。

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

## 应用内更新

“设置 → 应用更新”提供“自动检查更新”开关、稳定版和预览版通道、手动检查、发布说明及忽略单个版本。设备配对前也可从连接页进入。自动检查默认开启，可在应用设置中关闭。开启后，启动和恢复使用时每天至多检查一次，失败后至少间隔一小时重试。发现新版时在应用左下角显示“有新版本可下载”，点击“查看并下载”可阅读说明并下载安装。更新通道默认为稳定版，手动检查可随时使用。下载和安装由用户确认。

检测直接读取本仓库的 GitHub Releases，匿名 API 不可用或限流时回退到 Release Atom 订阅。版本按 SemVer 优先级比较。预览通道同时接收稳定版，草稿和旧版本会过滤，构建元数据变化不触发升级。

发布附件必须上传 `release-manifest.json`，应用根据其中的系统、架构、安装包类型、大小和 SHA-256 选择及校验下载。缺少清单或本机安装包的版本提供 Release 页面入口。清单中的 `summary` 用于 Atom 回退时显示更新说明。

- Windows EXE 安装包由本仓库的 NSIS 脚本生成，清单标记 `updateMode: "windows-nsis"`。应用下载校验后，点击“退出并安装更新”会等待当前进程退出，在原目录运行安装并重新打开工作台。安装前复用本机任务检查，配置位置与启动偏好由现有安装管理器读取。
- macOS 和 Linux 桌面端支持应用内下载及校验，之后打开 DMG、PKG 或系统安装程序。AppImage 打开下载目录，按平台安装方式替换。
- Android 打开对应架构 APK 的官方 Release 下载地址，由系统安装流程处理。iOS 打开该 Release 中与签名及分发方式对应的安装说明。

已有清单缺少 `updateMode` 时，桌面端下载校验后打开系统安装程序。首次部署带更新功能的版本，使用对应平台的安装包完成安装。
