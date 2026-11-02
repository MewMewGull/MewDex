# MewDex

**English** | [简体中文](README.zh-CN.md)

A cross-platform AI workbench. Input clients and execution devices are independent: any authorized client can start or resume a conversation on any authorized execution device in the same network. Phones act as input clients; computers can provide both input and execution; servers support headless execution.

## Source and license

The source repository is [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex), with `main` as its default branch:

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex's original code is licensed under the [Apache License 2.0](LICENSE). Third-party code, dependencies, and engine resources retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md) and the [dependency license review](docs/licensing.md). Local configuration, credentials, toolchains, and build artifacts are excluded from the source repository.

## What's in a package

MewDex consists of an input UI, a Go background service, and Codex engine resources. Current artifacts are development test builds; production installers and distribution are still in progress.

| Component | Location in a Windows desktop package | Purpose |
| --- | --- | --- |
| Input UI | `mewdex-client.exe` | Desktop workbench for viewing conversations, sending tasks, handling approvals, and managing the local device |
| Go background service | `mewdex-node.exe` | Provides the central Hub, execution backend (Executor), and optional model network egress (Gateway), according to configuration |
| Codex engine and complete resources | `engine/` | Runs model turns, file operations, and terminal commands on execution devices |

The main files in a Windows desktop development package are laid out as follows. Other platforms use binaries built for their operating system and architecture:

```text
MewDex/
  mewdex-client.exe
  mewdex-node.exe
  engine/
    mewdex-engine.json    # Engine version and file checksum manifest
    bin/
      codex.exe
      ...
    ...                    # Platform helpers, sandbox resources, and licenses
```

**Hub and Executor use the same Go backend binary, with roles selected through configuration.** Both the development command `mewdex` and the desktop package's `mewdex-node` are built from `cmd/mewdex`. Builds are specific to Windows, macOS, or Linux and their CPU architectures; deploy the artifacts that match the target device.

The Codex engine is a separate program launched through `codex app-server --listen stdio://`. The CLI and App Server share the same `codex` executable; the Codex desktop app is not required. The pinned compatible version is **0.162.1**, defined in [engine.lock.json](engine.lock.json). Keep the complete `engine/` directory: copying only `codex.exe` is insufficient to deploy an execution device. There is no need to replace the user's global Codex installation.

## Choosing a deployment

| Use case | Where to deploy | Roles to enable | Codex engine required? |
| --- | --- | --- | --- |
| Use a single computer | Local desktop package | Hub + Executor | Yes, prepared with the desktop resources |
| Control a home or office computer from a phone | Background service on the computer; input app on the phone | Hub + Executor on the computer; paired input client on the phone | On the computer only |
| Share a central service across several computers | Go backend on a central server; Go backend and engine on each execution device | Hub on the central server; Executor on execution devices joined to the same network | On execution devices only |
| Run tasks on a server without a graphical interface | Go backend and engine on the server | Executor connected to an existing Hub; optionally Hub as well | Yes |

The current topology is one active Hub with multiple input clients and execution devices. These are role combinations: the desktop development package currently prepares both the Go backend and complete engine resources. Separate releases automatically trimmed for Hub-only or Executor-only deployments are not yet available. A Hub-only service does not need a graphical interface or Codex engine at runtime.

### Single computer, with an optional phone

1. Extract the complete desktop test package matching the computer's operating system and architecture, then launch `mewdex-client.exe` on Windows.
2. Use the desktop setup entry to create a network, choose a local project directory, and start the background service.
3. Select the local device and project in the workbench, configure that execution device's model account, and send a task.
4. To connect a phone, first configure an HTTPS/WSS endpoint that the phone can reach, then pair it with the same network. A newly created Hub is reachable only from the local machine by default.

### Central Hub with multiple execution devices

1. Deploy the Go backend for the central server's platform, configure the Hub role and a reachable HTTPS/WSS endpoint, and start the service.
2. Deploy the Go backend and complete Codex engine resources on each execution device. Desktop devices can use the setup entry to join an existing network, enable execution, and configure local project directories.
3. Configure each execution device's own model account and network egress, start its service, and confirm that it is online.
4. Pair desktop or mobile input clients with the same Hub, grant access to the required devices and projects, and select a target execution device before sending tasks.

Headless deployments start the same Go program using a device configuration file. The following assumes that roles, pairing, projects, and engine configuration are already prepared. In desktop packages, the Go binary is named `mewdex-node`:

```text
mewdex serve -config /path/to/device.json
```

Gateway is an optional network egress for model traffic. Execution devices can connect directly or set `engine.useHubGateway: true` to forward traffic through the Hub. Forwarding does not share model account authorization. MewDex does not provide NAT traversal; remote devices must be able to reach the Hub.

### Where tasks actually run

```text
Desktop / mobile client -> Hub -> Target device's Executor -> Local Codex App Server
                                                                       |
                                                          Project files and commands
                                                           on that execution device
```

The workspace is the project directory configured on the execution device and passed to the engine as `cwd`; it is independent of where `codex.exe` is stored. Resuming a conversation from another input client keeps execution on the same device. One engine can manage conversations across multiple projects. Within one Executor, a running session or a session with an unknown outcome holds exclusive access to its project directory and its parent and child directories; sessions in non-overlapping directories can run concurrently. Automatic worktree creation and directory locking across separate Executor processes are not currently implemented.

Background tasks continue after the input window closes. The backend service and engine resources are stored in runtime resource directories by content hash; local runtime state identifies the actual process paths. Configuration, pairing credentials, and history are device data and must not be copied to other devices as part of a generic application package.

## Technology and boundaries

- Go: device services, Hub, Codex stdio adapter, and request egress.
- React / TypeScript: shared input UI for desktop, mobile, and a future web client, plus a separate communication SDK.
- Tauri 2: platform shell; network routing and task execution belong to the backend.
- SQLite: persistent state other than configuration; attachments are stored separately.
- Official Codex App Server: reused engine with a pinned compatibility baseline; the user's global Codex configuration and credentials are left unchanged.

Closing the UI does not stop tasks. Switching input clients does not move execution. Forwarding model traffic does not share model account authorization.

See the [implementation record](docs/implementation.md) for development status, recorded validation, and remaining delivery risks; the [protocol](docs/protocol.md) for input-client integration; and the [architecture](docs/architecture.md) for module boundaries. Development artifacts are not final release candidates or evidence of completed acceptance testing.

The scope and evidence for encrypted device export, two-phase restore, and current installation upgrades are documented in [device data lifecycle](docs/data-lifecycle.md). The management layer supports preparation, review-result queries, configuration switching, and pairing again. The desktop UI supports background jobs, progress, and cancellation; the complete native UI flow still requires hands-on validation.

## Development entry points

```powershell
go run ./cmd/mewdex init
go run ./cmd/mewdex doctor -engine
go run ./cmd/mewdex serve
```

In another terminal, run `npm install` and `npm run dev`, then open the shared input UI at `http://127.0.0.1:1420`. The first `init` writes a single-use administrator pairing code to `.local/pairing-code`, valid for ten minutes. Generate another with `go run ./cmd/mewdex invite -role admin`. Keep pairing codes and the `.local` directory out of version control.

For Windows native development, use `scripts/dev-native.ps1`. It prepares the Go service and pinned official engine resources; `-Check` checks native compilation without opening a window or starting a service. The project's local Rust toolchain is in `.local/toolchains`, without changing the global PATH. Native mobile dependencies and production distribution remain in progress.

The desktop setup entry can create a network, join an existing network, select execution directories, and start the local service. It remains accessible when an existing paired connection fails, without first deleting the pairing record. Configuration is stored in the app's own data directory, and the service runs in the background. Workspace management supports adding projects and organizing their names and categories while the service is running; editing the existing local device configuration still requires stopping the service. Newly created Hubs are local-only by default, so configure an HTTPS endpoint before connecting other devices. Mobile and browser clients expose the input workbench without local service-management commands.

The local-device settings show and control automatic startup. Windows and macOS start the background service at user login; Linux servers can use a systemd system service. Recheck these settings after restoring data or changing configuration. Disabling automatic startup leaves the currently running service active. Registration and execution have been checked in isolated directories on all three platforms; actual startup after boot, installers, and upgrades remain in progress.

`scripts/build-desktop-development.ps1` compiles a Windows Debug application in a separate output directory, without launching the UI or generating an installer. Build state is saved in `.state/desktop-development-build.json`; inspect it after an interruption to avoid starting a duplicate build.

If the workbench is already running, use `scripts/build-desktop-development.ps1 -SideBySide` to compile the next development build into `.state/desktop-development-next-target`, keeping the running build's directory intact. Compilation does not close or reopen the workbench or replace the background service. Check `.state/runtime-processes.json` for the actual runtime paths.

The Windows desktop redesign test build is recorded at `dist/windows/MewDex-Windows-x64-20261030-033735` and the matching `.zip`. Implementation and switching notes for continuous scrolling, workspace creation and categories, opening files with their default applications, input controls, account quotas, appearance, and sidebar resizing are in the [Windows redesign notes](docs/windows-redesign-20261030.md). Later project and input-toolbar revisions are documented in [projects and toolbar](docs/windows-projects-toolbar-20261030.md). Creating a local project can automatically update an older backend; the running service was not switched in advance.

For Android, use the `Check`, `Init`, and `BuildArm64` actions in `scripts/mobile-android.ps1`; `scripts/prepare-android-deps.ps1` prepares the separate SDK. The recorded ARM64 Debug test APK is `dist/android/MewDex-Android-arm64-20261030-022209.apk`, with signature and 16 KiB alignment checks completed. A physical Android 15 device was updated while retaining app data, and pairing retention, device switching, system-bar themes, and keyboard layout were checked. Two actual model conversations were completed on Windows over the public network, followed by checks of pairing and history recovery and conversation continuation after reopening the app process. Notifications and attachments have not yet been verified on that device. For iOS, run `scripts/mobile-ios.sh` on a Mac; `scripts/remote/prepare-ios-deps.sh` prepares missing project-local tools. The simulator has completed input pairing, model text requests sent to Windows, credential and history recovery, and keyboard-layout checks. Physical iOS devices and production signing remain unverified.

Remote Executors configured with `engine.useHubGateway: true` forward model TLS through the Hub's existing WSS endpoint, without exposing a separate model-proxy port. Each Executor uses its own node authorization; the model subprocess receives only a temporary loopback-proxy password. `doctor -gateway` probes official TLS connectivity without model credentials or inference.

Self-hosted deployments with existing SSH and HTTPS reverse-proxy access can use the separate [backhaul component](docs/backhaul.md) to manage connection retries, status, and local automatic startup. The persistent component is still in development; real SSH operation and production takeover await authorization. It does not provide NAT traversal.

`go run ./cmd/protocol-gen` generates TypeScript SDK protocol types from Go types and generates the separate desktop-local protocol in `apps/client/src/localProtocol.ts`. The frontend connects to the network through the SDK, without calling Codex directly. `npm run typecheck` checks types in the shared SDK and input UI; run `go test` for the affected Go packages.

`.local` stores development configuration, pairing credentials, and device state. `.state` stores development binaries, protocol snapshots, and risk-validation evidence. Both are ignored by Git. No final release-candidate installer is currently available.
