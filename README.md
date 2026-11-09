A Codex-based, cross-platform agent for starting and resuming tasks on your computer remotely from a phone or chat app. Still being built, one wish at a time…

-----

# MewDex

**English** | [简体中文](README.zh-CN.md)

MewDex ships independent **APP**, **Web Input**, **Hub**, and **Executor** packages. The APP and browser share the input workbench, conversations, projects, and network management. The desktop APP or standalone Web Input service owns QQ, Weixin, and Feishu connections and submits processed messages to Hub. Hub owns models, sessions, and coordination. Executors connect to Hub over WebSocket.

Configure platform credentials and allowed users under **Settings → Message channels**. Channels use the persistent Hub connector by default, with a separately deployable connector also available. Messages continue after the APP or browser closes. See the [messaging and migration guide](docs/messaging.md).

**Settings → Hub and execution terminals** includes Skills, Memory, Hooks and Plugins. Manage project Skills and memory, native Hub Skills, automatic memory settings, Hook configuration and trust, and plugin sources and installation. Update the Hub to use these controls. See the [extension guide](docs/extensions.md).

## Source and license

The source repository is [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex), with `main` as its default branch:

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex's original code is licensed under the [Apache License 2.0](LICENSE). Third-party code, dependencies, and engine resources retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md) and the [dependency license review](docs/licensing.md). Local configuration, credentials, toolchains, and build artifacts are excluded from the source repository.

## Independent packages and deployment

| Package | Contents | Windows x64 name |
| --- | --- | --- |
| APP | Input terminal, network management, desktop message connector | `MewDex-Windows-x64` |
| Web Input | Shared input UI, standalone web server and message connector | `MewDex-Web-Windows-x64` |
| Hub | Central API, model runtime, sessions and project coordination | `MewDex-Hub-Windows-x64` |
| Executor | WebSocket node, local sandbox and its own web control panel | `MewDex-Executor-Windows-x64` |

The Tauri APP shares React code and the SDK with Web Input. Its desktop payload contains `mewdex-client` and `mewdex-input-bridge`. Hub ships the pinned model runtime in `engine/`. Windows x64, macOS ARM64 and Linux x64 Executors ship an independent `sandbox/` runtime. Windows uses pinned Codex sandbox modules, macOS uses the pinned MXC SDK with Seatbelt, and Linux uses pinned Codex Linux sandbox modules with a verified bundled Bubblewrap executable. Other executor targets currently use the complete `engine/` runtime. See `mewdex.product.json` for component and distribution boundaries.

```powershell
npm run build:app:windows
npm run package:app:windows
npm run package:services
```

Windows packages are exported to `dist/windows`. Use `package:hub`, `package:web`, or `package:executor` to build a single distribution. Service packages accept targets such as `--target=linux:x64`. See the [release guide](docs/releases.md).

On Windows and macOS, double-click a service executable or its `.app` launcher to open its panel. Tray icons distinguish input terminals (blue speech bubble), Hub (green connected nodes), and executors (orange terminal). The tray provides status, startup settings and an exit action.

### Hub

```powershell
.\mewdex-hub.exe init -config .local/hub/device.json
.\mewdex-hub.exe account -config .local/hub/device.json
.\mewdex-hub.exe serve -config .local/hub/device.json
```

The account command reads username and password JSON from standard input. Hub defaults to `127.0.0.1:47830`. Configure its listener, browser `allowedOrigins`, and model egress in `device.json`, then publish an HTTPS/WSS endpoint.

For public access, configure dedicated reverse-proxy or mesh-networking software yourself and connect MewDex to the resulting Hub address.

### Executor control panel

```powershell
.\mewdex-executor.exe panel -root .local/executor
```

Open `http://127.0.0.1:47832` and sign in with `.local/executor/panel-token`. Enter the Hub URL and an executor invitation generated in the input terminal, configure local directories, and start the executor. It maintains its own profile and database and connects to Hub over WebSocket. The panel listens on loopback. SSH forwarding can expose a remote device's local panel.

Linux hosts with AppArmor user-namespace restrictions need administrator preparation for ordinary service accounts. The Executor provides a read-only preview and a local sandbox diagnostic. See the [Linux sandbox guide](docs/linux-sandbox.md).

### Web Input

```powershell
.\mewdex-web.exe -hub-url https://your-hub.example.com -listen 127.0.0.1:47831
```

Deploy this server together with `ui/`. It forwards `/health` and `/v1/` to the configured Hub, including WebSocket upgrades, and serves the input UI independently. Add the browser's public origin to Hub `allowedOrigins`.

Choose either the desktop APP or Web Input for the same input features. Channels use the persistent Hub connector by default. The Web Input service retains its original connector for existing channel configurations.

Hub and Executor can share a computer with separate configuration directories. Existing combined profiles and databases remain in their original locations. Split packages use single-role profiles. The persistent Hub connector uses a separate channel database. Existing channels remain manageable through their legacy input terminals and can be migrated using the messaging guide.

### Hub projects and connections

A project has a name, a category, and a directory group. Each directory identifies an execution device and one of its durable directory bindings. One project can contain multiple directories on the same device and directories on several devices. Project metadata is persisted in the Hub database.

Project settings include instructions, Skills, scripts bound to a specific machine directory, and ordered workflows. The Hub retains configuration history and captures the active version when each turn starts. Tool results show that version. Workflows currently use an Agent-driven execution prototype.

The Computer Use prototype lets a new Hub conversation request control of a selected executor's window. The local executor panel selects a window and grants temporary access. Input terminals show status and a stop action. The native driver currently targets 64-bit Windows interactive desktops. Real window input and physical Escape acceptance remain pending, along with macOS and Linux drivers.

The Hub package includes a resumable task evaluator. It captures the model runtime, project configuration and tool traces, checks explicit task contracts, and exports a review report for comparing changes. See [task evaluations](docs/evaluations.md).

| Component | Responsibility |
| --- | --- |
| Hub | Project catalogue, conversations, ChatGPT sign-in, model inference, task orchestration and result history |
| Input Terminal | Desktop, mobile, web, QQ, WeChat or Feishu conversation entry |
| Execute Terminal | Hub connection, local directory bindings, sandboxed commands and file snapshots |

Only the Hub machine needs OpenAI connectivity. Each execution node needs a connection to the Hub. Those connections form the logical MewDex network. The Hub can use its own network or an HTTP proxy available to that machine, configured through `engine.proxyUrl` in the Hub profile.

### Where work runs

```text
Desktop / mobile / web / QQ / WeChat / Feishu
                     |
                     v
         Hub projects, conversations and model engine
                     |
            paired control connections
              /      |       \
         Windows    macOS    Linux execution nodes
              project directories and local commands
```

One conversation can dispatch commands to several directories in its project group. The Hub resolves each tool call to a paired node. The node uses the pinned local sandbox runtime to execute the command and returns its durable job state and output. Model login is managed in the Hub workspace settings.

On each node, a command can access the project group's bound directories on that machine, subject to the selected execution policy. Running or unresolved jobs reserve overlapping directory roots. Reconnection queries the original command and job IDs. The project file panel selects a group directory and returns file snapshots through the Hub.

Existing device directories are imported into separate Hub projects during the first migration. Owners can combine their directory references in project settings and select a Hub project again in existing message-channel settings. Existing device conversations remain available in the history section. Hub and execution nodes need the updated `hub-execution` protocol capability for new cross-machine work.

## Technology and boundaries

- Go: execution-node services, Hub coordination, and the Codex stdio adapter.
- React / TypeScript: shared input UI for desktop, mobile, and web clients, plus a separate communication SDK.
- Tauri 2: platform shell; network routing and task execution belong to the backend.
- SQLite: persistent state other than configuration; attachments are stored separately.
- Official Codex App Server: reused engine with a pinned compatibility baseline; the user's global Codex configuration and credentials are left unchanged.

The Hub and execution nodes keep managing started tasks after the UI closes. Different input clients can continue the same Hub conversation.

See the [implementation record](docs/implementation.md) for development status, recorded validation, and remaining delivery risks; the [protocol](docs/protocol.md) for input-client integration; and the [architecture](docs/architecture.md) for module boundaries. Development artifacts are not final release candidates or evidence of completed acceptance testing.

The scope and evidence for encrypted device export, two-phase restore, and current installation upgrades are documented in [device data lifecycle](docs/data-lifecycle.md). The management layer supports preparation, review-result queries, configuration switching, and pairing again. The desktop UI supports background jobs, progress, and cancellation; the complete native UI flow still requires hands-on validation.

## Development entry points

`npm run build:ui` builds the shared input UI independently. Hub builds use only Go sources. Desktop resource preparation builds the APP message connector. Use `npm run dev` for frontend hot reload.

```powershell
go run ./cmd/mewdex init
go run ./cmd/mewdex doctor -engine
go run ./cmd/mewdex serve
```

In another terminal, run `npm install` and `npm run dev`, then open the shared input UI at `http://127.0.0.1:1420`. The first `init` writes a single-use administrator pairing code to `.local/pairing-code`, valid for ten minutes. Generate another with `go run ./cmd/mewdex invite -role admin`. Keep pairing codes and the `.local` directory out of version control.

For Windows native development, use `scripts/dev-native.ps1`. It prepares the APP message connector; `-Check` checks native compilation without opening a window or starting a service. The project's local Rust toolchain is in `.local/toolchains`, without changing the global PATH. Native mobile dependencies and production distribution remain in progress.

Desktop, mobile, and browser inputs sign in to a deployed Hub to manage conversations, projects, and device authorization. The executor's own web panel manages its local profile, pairing, service lifecycle, and directory bindings.

The Windows APP installer manages the APP's startup-at-login preference. Hub and Executor use their independent deployment and service configuration.

`scripts/build-desktop-development.ps1` compiles a Windows Debug application in a separate output directory, without launching the UI or generating an installer. Build state is saved in `.state/desktop-development-build.json`; inspect it after an interruption to avoid starting a duplicate build.

If the workbench is already running, use `scripts/build-desktop-development.ps1 -SideBySide` to compile the next development build into `.state/desktop-development-next-target`, keeping the running build's directory intact. Compilation does not close or reopen the workbench or replace the background service. Check `.state/runtime-processes.json` for the actual runtime paths.

Windows development builds can be exported as portable test packages with `scripts/export-windows-development.py` or packaged as installers with `scripts/build-windows-installer.mjs`. The [release guide](docs/releases.md) covers platform artifacts and release validation. Earlier UI implementation and validation are recorded in the [Windows redesign notes](docs/windows-redesign-20261030.md) and [projects and toolbar](docs/windows-projects-toolbar-20261030.md).

For Android, use the `Check`, `Init`, and `BuildArm64` actions in `scripts/mobile-android.ps1`. `scripts/prepare-android-deps.ps1` prepares the separate SDK. For iOS, run `scripts/mobile-ios.sh` on a Mac, with project-local dependencies prepared by `scripts/remote/prepare-ios-deps.sh`. Recorded Android device and iOS simulator checks are documented in the [implementation record](docs/implementation.md).

`go run ./cmd/protocol-gen` generates TypeScript SDK protocol types from Go types and the desktop-local protocol in `apps/client/src/hub-controller/localProtocol.ts`. The frontend connects to the network through the SDK. Run the following checks before submitting changes.

```sh
npm run typecheck
npm run test:sdk
npm run test:client
npm run test:release
npm run build:ui
go test ./...
```

`.local` stores development configuration, pairing credentials, and device state. `.state` stores development binaries, protocol snapshots, and risk-validation evidence. Both are ignored by Git. No final release-candidate installer is currently available.
