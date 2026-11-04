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
  "tag": "v0.1.0-preview.20261104",
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
