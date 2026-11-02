# Third-party notices

MewDex original code is licensed under Apache-2.0. This does not replace the
licenses of third-party code, packages, data, development tools, or platform
libraries. The dependency inventory and distribution requirements are recorded
in [docs/licensing.md](docs/licensing.md) and
[docs/dependency-licenses.json](docs/dependency-licenses.json).

## Vendored notification plugin

- Component: `tauri-plugin-notification` 2.8.0.
- Copyright 2019-2023 Tauri Programme within The Commons Conservancy.
- Upstream: <https://github.com/tauri-apps/plugins-workspace>.
- Source: [third_party/tauri-plugin-notification](third_party/tauri-plugin-notification).
- Upstream license: **Apache-2.0 OR MIT**. Both original license texts and source
  notices are retained as [LICENSE_APACHE-2.0](third_party/tauri-plugin-notification/LICENSE_APACHE-2.0)
  and [LICENSE_MIT](third_party/tauri-plugin-notification/LICENSE_MIT).
- MewDex modifications are documented in
  [MEWDEX-PATCHES.md](third_party/tauri-plugin-notification/MEWDEX-PATCHES.md).

## Separately obtained Codex engine

Desktop development packages obtain OpenAI Codex separately, at the version and
source revision in [engine.lock.json](engine.lock.json). Its executable and
resources are not committed to this source repository.

The pinned upstream engine uses Apache-2.0. Its upstream NOTICE credits OpenAI,
Florian Dehau, and The Ratatui Developers for the engine and its MIT-licensed
Ratatui-derived code. Retain the upstream NOTICE when redistributing the engine.

The desktop resource preparation script retains package-level LICENSE / NOTICE
files and obtains the pinned upstream LICENSE / NOTICE in
`engine/licenses/codex-LICENSE` and `engine/licenses/codex-NOTICE`. Engine
dependencies and accompanying resources retain their own licenses; the engine's
Apache-2.0 declaration is not a replacement for those notices.

## Dependencies obtained by package managers

Dependencies installed by npm, Cargo, and Go retain their upstream terms. They
are not relicensed by the root LICENSE. In particular, MPL-2.0 components must
retain their license and source availability, and any redistributed CC-BY-4.0
data requires its attribution. Native system libraries and SDKs also have
separate terms. See the licensing report for exact reviewed versions and the
remaining work required for a binary release.
