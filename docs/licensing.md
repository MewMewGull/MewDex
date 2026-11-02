# 依赖许可核对

核对日期：2026-11-02。项目自有代码已按维护者选择采用 **Apache-2.0**，完整条文在根目录 [LICENSE](../LICENSE)，版权声明见 [NOTICE](../NOTICE)。该选择不改变第三方组件的许可。

## MIT 与 Apache-2.0 是否可用

根据当前锁定版本的包级许可、自有代码与第三方代码的分界，以及引擎通过独立进程通信的集成方式，**MewDex 自有代码可以选择 MIT 或 Apache-2.0**。目前核对的依赖声明没有发现要求将 MewDex 自有代码整体改为 GPL / AGPL 的依赖条件。

这并不表示所有依赖都能改标为 MIT / Apache-2.0：MPL 文件、第三方数据、平台库和开发工具仍保留自己的条款。以下结论支持源码仓库采用 Apache-2.0，不等同于已经完成每个平台安装包的许可交付。

MIT 和 Apache-2.0 都允许商业使用、修改和分发。Apache-2.0 第 3 条还明确授予实施贡献所需的贡献者专利许可，并规定针对项目的专利诉讼可能终止该许可；MIT 没有对应的明确专利条款。依据：[Apache-2.0 原文](https://www.apache.org/licenses/LICENSE-2.0)、[MIT 原文](https://opensource.org/license/mit)。

## 核对范围与证据

[dependency-licenses.json](dependency-licenses.json) 记录精确版本、原始许可表达式、包来源、锁文件校验值，以及 Go 许可文件的 SHA-256。输入文件哈希按 Git 的 LF 文本形式计算，便于在另一台电脑复核。

| 范围 | 数量 | 核对方式与结论 |
| --- | ---: | --- |
| npm 锁文件中的第三方包条目 | 314 | 包含开发依赖及不同平台的可选包；主要为 MIT / ISC / Apache-2.0 / BSD；另有 MPL-2.0 和 CC-BY-4.0 |
| Rust 第三方 crate 精确版本 | 501 | `cargo metadata --locked --all-features`，覆盖客户端及原生凭据验证工具的全部目标；验证工具的依赖版本已包含在这 501 项中 |
| Go 在 `go.mod` 中声明的外部模块 | 16 | 含直接及间接要求；逐项读本机模块缓存中的原始许可，结果为 BSD-3-Clause、MIT、ISC 和 Apache-2.0 |
| 仓库中保留源码的通知插件 | 1 | `tauri-plugin-notification` 2.8.0，Apache-2.0 OR MIT；原许可及补丁记录保留 |
| 独立获取的 Codex 引擎 | 1 | 0.162.1；核对 `engine.lock.json` 固定源码修订的 Apache-2.0 LICENSE 及 NOTICE |

数量表示包条目或版本，不表示它们全部进入某个安装包。Rust 原始表达式中的 `/` 保留上游元数据形式；`AND` 需要同时遵守，`OR` 允许选择其中一项，不能把两者混为一谈。

Go 的范围是 `go.mod` 声明的 16 个模块，不把 `go list -m all` 中额外出现、但未在本项目要求列表中的依赖模块自身测试和开发工具算作应用依赖。Go 标准库、模块内部嵌入的第三方源码和图片声明不由模块根许可一并覆盖；二进制分发仍需按实际编译内容收集这些声明。

`third_party/tauri-plugin-notification/Cargo.lock` 是上游包附带的独立锁文件；应用通过路径补丁使用插件，实际依赖版本由 `apps/client/src-tauri/Cargo.lock` 决定，不使用上游独立锁文件替代应用的解析结果。

## 需要保留的第三方条件

### MPL-2.0

- npm 的 `lightningcss` 1.32.0 及 11 个平台包共 12 项，当前均属于开发依赖。
- Rust 有 `cssparser` 0.37.0、`cssparser-macros` 0.7.1、`dtoa-short` 0.3.5、`option-ext` 0.2.0、`selectors` 0.38.0。

MPL 的要求针对被覆盖的文件，可以与采用其他许可的独立文件组合。如果分发的二进制实际含这些组件，应告知接收者在哪里取得对应版本的 MPL 源码；修改过的 MPL 文件也须按 MPL 提供。仅在本机构建时使用、没有分发该工具或其代码，不会因此要求将生成的 MewDex 自有代码改为 MPL。依据：[Mozilla MPL 2.0 FAQ，Q8 / Q11 / Q16](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)、[MPL 2.0 第 3 条](https://www.mozilla.org/en-US/MPL/2.0/)。

### 数据与其他宽松许可

`caniuse-lite` 1.0.30001819 为 CC-BY-4.0，当前属于开发依赖。若分发其数据，应保留署名、许可链接及变更说明。依据：[CC-BY-4.0](https://creativecommons.org/licenses/by/4.0/)。

Rust 闭包还包括 Unicode-3.0、Zlib，以及 Apache-2.0 WITH LLVM-exception。`unicode-ident` 的表达式包含 `AND Unicode-3.0`，不能只保留 MIT / Apache 文本；`r-efi` 的表达式提供 MIT / Apache-2.0 / LGPL 的选择，选用宽松选项不要求改用 LGPL。

MIT / BSD / ISC / Apache 等组件仍需按其条款保留版权、许可和适用 NOTICE。根目录 LICENSE 不替代依赖原文。

### 仓库内通知插件

已与本地缓存的 crates.io 原始包核对，归档 SHA-256 为 `5bc6c194a95a535abdfbe3bc1af9a43935b8b9b29c216b8e71af8b59132703cf`，与补丁记录一致。

原来的 Apache-2.0 / MIT 文本、版权及 SPDX 头保留；被修改的上游文件增加 MewDex 修改标记。差异范围和来源见 [MEWDEX-PATCHES.md](../third_party/tauri-plugin-notification/MEWDEX-PATCHES.md)。新增的 MewDex 测试文件按自身 SPDX 声明采用 Apache-2.0。

### Codex 引擎

`engine.lock.json` 固定 0.162.1 及源码修订 `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`。MewDex 通过 stdio 与独立可执行文件通信；引擎程序和资源不进入本 Git 仓库。

已核对该修订的 [LICENSE](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/LICENSE) 和 [NOTICE](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/NOTICE)。`scripts/prepare-desktop-binaries.mjs` 已保留 npm 包中的顶层 LICENSE / NOTICE，并将固定修订的条文保存到 `engine/licenses/codex-LICENSE` 与 `engine/licenses/codex-NOTICE`。引擎 NOTICE 包含其 Ratatui 衍生代码的署名。

本次没有逐文件核对引擎二进制内部全部依赖或伴随资源。其 Apache-2.0 主许可不能覆盖这些独立组件；发布完整引擎包时需要保留相应第三方声明。代码开源许可也不代替模型服务的账号、服务或商标使用条款。

### 原生平台与开发工具

Linux Tauri 使用 GTK / WebKitGTK 等系统库。Rust 绑定的 MIT 许可不等于底层 C 库的许可，例如 WebKitGTK 的官方文档注明 BSD / LGPL-2.1。使用系统动态库与将库装进 AppImage 等发布物的义务不同；若随包分发，要核对实际库版本、源码提供方式、许可及替换 / 重新链接条件。依据：[Tauri 平台依赖](https://v2.tauri.app/start/prerequisites/)、[WebKitGTK 官方许可说明](https://www.webkitgtk.org/reference/webkit2gtk/stable/index.html)。

`tools/mobile-deps.lock.json` 中的 libimobiledevice 1.4.0 是开发工具；上游 README 和所查的库及工具源码声明 LGPL-2.1-or-later。它不在 MewDex 的 Go / Rust / npm 应用依赖中，不能因为开发时调用它就认定整个 MewDex 必须改用 GPL / LGPL；若以后将这些工具随产品提供，需要另外满足其条款。依据：[该版本 README 的许可章节](https://github.com/libimobiledevice/libimobiledevice/blob/1.4.0/README.md#license)、[工具源码的许可头](https://github.com/libimobiledevice/libimobiledevice/blob/1.4.0/tools/ideviceinfo.c)、[COPYING.LESSER](https://github.com/libimobiledevice/libimobiledevice/blob/1.4.0/COPYING.LESSER)。

Android SDK / NDK、Apple SDK、WebView2、Go / Rust / Node 工具链及移动端准备脚本中的其他工具各有自己的许可。开发机上的 `.local/toolchains` 不进入源码仓库；本次没有将这些 SDK 或工具作为 MewDex 自有代码重新授权。

## 后续更新与二进制分发

依赖变化后，在项目根目录运行（Python 3.11+、Go、Cargo）：

```sh
python scripts/audit-dependency-licenses.py --review-date YYYY-MM-DD
```

脚本只读取包元数据，Cargo 可能下载源码，不构建或启动应用。不读取设备或模型凭据。它会使用项目内现有 Rust 工具链，或 PATH 中的 Cargo；缺失许可声明或出现未经复核的 Go 模块版本时会停止。更新生成的清单后仍需判断新条款是否适用。

正式分发应按目标平台及真实包内容收集第三方版权、许可全文、适用 NOTICE 和需要提供的源码，并把本项目的 LICENSE / NOTICE 一起交付。当前的许可清单是包级调查记录，尚未替代这一步；本次没有制作、发布或确认任何平台的最终安装包。
