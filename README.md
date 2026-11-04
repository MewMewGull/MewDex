A Codex-based, cross-platform agent for starting and resuming tasks on your computer remotely from a phone or chat app. Still being built, one wish at a time…

-----

# MewDex

**English** | [简体中文](README.zh-CN.md)

MewDex has four composable components. **Input Terminal** handles user input and results. **Execute Terminal** runs tasks on its device. **Hub** coordinates devices and routes requests. **Hub Controller** manages local terminals and authorized remote terminals.

Windows, macOS, and desktop Linux apps include all four components. Android and iOS / iPadOS apps include Input Terminal and Hub Controller. Users configure local execution, Hub hosting, and the Hub to connect to. Mobile apps provide input and remote management through an existing Hub.

WeChat, QQ, and Feishu inputs are configured in **Settings → Message channels**.
See the [messaging setup guide](docs/messaging.md) for credentials, access lists,
and supported message types.

The desktop app installs all four components. Opening it starts the configured local Hub or Execute Terminal and restores the workbench connection. The Input Terminal can connect to a local or remote Hub, and the Hub Controller exposes the corresponding management controls. First use supports configuring local terminals or connecting to an existing Hub. The web workbench is optional and disabled by default. Choose **Start web workbench** under **Settings → Connections and services → Current connection** or the local device overview, then sign in with a MewDex account or login key. The same controls can stop it. Browsers and the API share the Hub address and port, which defaults to `http://127.0.0.1:47830` and follows the configured port. This preference is saved locally. The Hub and an enabled web workbench continue running after the desktop app exits.

## Source and license

The source repository is [MewMewGull/MewDex](https://github.com/MewMewGull/MewDex), with `main` as its default branch:

```sh
git clone https://github.com/MewMewGull/MewDex.git
cd MewDex
```

MewDex's original code is licensed under the [Apache License 2.0](LICENSE). Third-party code, dependencies, and engine resources retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md) and the [dependency license review](docs/licensing.md). Local configuration, credentials, toolchains, and build artifacts are excluded from the source repository.

## Components and platform packages

| Component | Responsibility | Source |
| --- | --- | --- |
| Input Terminal | User input, conversation continuation, results, and interactions | `apps/client/src/input-terminal/` |
| Execute Terminal | Model turns, files, and commands in local projects | `internal/executor/`, `internal/engine/` |
| Hub | Network identities, devices, routing, and event synchronization | `internal/hub/`, `internal/transport/` |
| Hub Controller | Local service management and authorized remote terminal management | `apps/client/src/hub-controller/`, `internal/localnode/` |

| Platform | App components | Release installer |
| --- | --- | --- |
| Windows | All four | `.exe` / `.msi` |
| macOS | All four | `.dmg` / `.pkg` |
| Linux Desktop | All four | `.AppImage` / `.deb` / `.rpm` |
| Android | Input Terminal, Hub Controller | `.apk` |
| iOS / iPadOS | Input Terminal, Hub Controller | Signed `.ipa` with distribution instructions |

[`mewdex.product.json`](mewdex.product.json) defines component ownership and platform composition. Native app capability declarations and release validation use this manifest. Each release describes components, operating systems, architectures, build configuration, signing, and installation, and includes the matching installers. See [release packaging](docs/releases.md).

The desktop `mewdex-client` provides Input Terminal and Hub Controller. The shared `mewdex-node` backend runs Hub and Execute Terminal according to configuration and is built from `cmd/mewdex`. The Hub can forward model traffic through a Gateway on itself or another paired device.

A Windows package contains the following files. Other desktop platforms use their matching applications and service binaries.

```text
MewDex/
  mewdex-client.exe      # Input Terminal + Hub Controller
  mewdex-node.exe        # Hub + Execute Terminal, enabled by configuration
  mewdex.product.json    # Components and platform composition
  engine/               # Complete Execute Terminal engine resources
    mewdex-engine.json
    bin/codex.exe
```

The Codex engine is a separate program launched through `codex app-server --listen stdio://`. The CLI and App Server share the same `codex` executable; the Codex desktop app is not required. The pinned compatible version is **0.162.1**, defined in [engine.lock.json](engine.lock.json). Keep the complete `engine/` directory: copying only `codex.exe` is insufficient to deploy an execution device. There is no need to replace the user's global Codex installation.

## Choosing a deployment

| Use case | Where to deploy | Roles to enable | Codex engine required? |
| --- | --- | --- | --- |
| Use a single computer | Local desktop package | Hub + Execute Terminal | Yes, prepared with the desktop resources |
| Control a home or office computer from a phone | Background service on the computer; Input Terminal and Hub Controller app on the phone | Hub + Execute Terminal on the computer; paired input client on the phone | On the computer only |
| Share a central service across several computers | Go backend on a central server; Go backend and engine on each execution device | Hub on the central server; Executor on execution devices joined to the same network | On execution devices only |
| Run tasks on a server without a graphical interface | Go backend and engine on the server | Executor connected to an existing Hub; optionally Hub as well | Yes |

One active Hub coordinates multiple Input Terminals, Execute Terminals, and Hub Controllers. Desktop packages include all four components, with local Hub hosting and execution enabled by configuration. Headless servers can run the same Go backend as a Hub, Execute Terminal, or Gateway. A Hub-only deployment requires the Go service binary and its device configuration.

### Single computer, with an optional phone

1. Extract the complete desktop test package matching the computer's operating system and architecture, then launch `mewdex-client.exe` on Windows.
2. Use the desktop setup entry to create a network, choose a local project directory, and start the background service.
3. Select the local device and project in the workbench, configure that execution device's model account, and send a task.
4. To connect a phone, first configure an HTTPS/WSS endpoint that the phone can reach, then pair it with the same network. A newly created Hub is reachable only from the local machine by default.

### Public access through SSH

For a Hub hosted on a personal computer or private network, a recommended setup uses an SSH reverse tunnel to a reachable server and an HTTPS reverse proxy on that server.

```text
Input Terminal / Hub Controller
  -> Server HTTPS reverse proxy -> Server listener -> SSH reverse tunnel -> Hub
```

1. Enable SSH reverse forwarding on the server and prepare a private key and trusted `known_hosts` entry on the Hub device.
2. Open **Local Device → Startup Settings**. The page automatically reads `.ssh/config`. Select a host or enter SSH settings manually, then use **Test connection** to check connectivity, the host key, and private-key authentication.
3. Choose the server listener's IP and port and enable the tunnel. The Hub maintains the listener and reconnects automatically.
4. Configure the domain, certificate, and Caddy/Nginx upstream on the server. The upstream must reach the selected server listener. A reverse proxy running in Docker needs a container-reachable private host address.
5. Connect other apps to the server's public HTTPS address. The tunnel panel reports SSH authentication and listener status. Verify the public HTTPS endpoint separately after configuring the server.

The SSH connection test reports elapsed time or a failure reason. Additional setup and migration details are in [Hub startup and SSH proxy management](docs/hub-startup-20261102.md).

### Central Hub with multiple execution devices

1. Deploy the Go backend for the central server's platform, configure the Hub role and a reachable HTTPS/WSS endpoint, and start the service.
2. Deploy the Go backend and complete Codex engine resources on each execution device. Desktop devices can use the setup entry to join an existing network, enable execution, and configure local project directories.
3. Configure each execution device's own model account and network egress, start its service, and confirm that it is online.
4. Pair desktop or mobile input clients with the same Hub, grant access to the required devices and projects, and select a target execution device before sending tasks.

Headless deployments start the same Go program using a device configuration file. The following assumes that roles, pairing, projects, and engine configuration are already prepared. In desktop packages, the Go binary is named `mewdex-node`:

```text
mewdex serve -config /path/to/device.json
```

The central server can host the Hub while every Execute Terminal retains its own projects, model account, and task execution. Configure the server's HTTPS reverse proxy to forward to the Hub listener, then connect desktop and mobile apps to that public address.

### Model egress through another device

The Hub can route model API traffic to a paired Gateway device with suitable network access. Configure these fields in the corresponding device profiles.

| Device | Configuration |
| --- | --- |
| Hub | Set `gateway.forwardDeviceId` to the egress device's ID and `gateway.allowedHosts` to the permitted model hostnames |
| Egress device | Connect to the Hub using `hubUrl` and the paired `nodeTokenFile`, configure `gateway.allowedHosts`, and optionally set `gateway.upstreamProxyUrl` to an existing proxy |
| Execute Terminal | Enable `engine.useHubGateway: true` and grant its paired identity Gateway access |

The egress device can run a dedicated Gateway with `hub: false` and `executor: false`, or combine Gateway with local task execution. Its device ID must match the Hub's `gateway.forwardDeviceId`. The Hub and egress device each enforce their allowed-host list. `gateway.forwardDeviceId` and `gateway.upstreamProxyUrl` select alternative routes on a given Gateway.

```text
Model API traffic
  Execute Terminal -> Hub WSS -> Paired Gateway -> Optional local proxy -> Model API
```

Control and one-use data connections share the Hub's HTTPS/WSS endpoint. Model TLS remains end-to-end, and each execution device uses its own model credentials. The selected egress device must be online for new model connections to succeed. Run `mewdex doctor -config /path/to/device.json -gateway` on an execution device to probe model TLS connectivity. Configuration and authorization details are in the [architecture guide](docs/architecture.md).

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
- React / TypeScript: shared input UI for desktop, mobile, and web clients, plus a separate communication SDK.
- Tauri 2: platform shell; network routing and task execution belong to the backend.
- SQLite: persistent state other than configuration; attachments are stored separately.
- Official Codex App Server: reused engine with a pinned compatibility baseline; the user's global Codex configuration and credentials are left unchanged.

Closing the UI does not stop tasks. Switching input clients does not move execution. Forwarding model traffic does not share model account authorization.

See the [implementation record](docs/implementation.md) for development status, recorded validation, and remaining delivery risks; the [protocol](docs/protocol.md) for input-client integration; and the [architecture](docs/architecture.md) for module boundaries. Development artifacts are not final release candidates or evidence of completed acceptance testing.

The scope and evidence for encrypted device export, two-phase restore, and current installation upgrades are documented in [device data lifecycle](docs/data-lifecycle.md). The management layer supports preparation, review-result queries, configuration switching, and pairing again. The desktop UI supports background jobs, progress, and cancellation; the complete native UI flow still requires hands-on validation.

## Development entry points

Run `npm install` and `npm run build:ui` before compiling the Go Hub to embed the shared web UI. Desktop build scripts perform this step automatically. Use `npm run dev` for frontend hot reload.

```powershell
go run ./cmd/mewdex init
go run ./cmd/mewdex doctor -engine
go run ./cmd/mewdex serve
```

In another terminal, run `npm install` and `npm run dev`, then open the shared input UI at `http://127.0.0.1:1420`. The first `init` writes a single-use administrator pairing code to `.local/pairing-code`, valid for ten minutes. Generate another with `go run ./cmd/mewdex invite -role admin`. Keep pairing codes and the `.local` directory out of version control.

For Windows native development, use `scripts/dev-native.ps1`. It prepares the Go service and pinned official engine resources; `-Check` checks native compilation without opening a window or starting a service. The project's local Rust toolchain is in `.local/toolchains`, without changing the global PATH. Native mobile dependencies and production distribution remain in progress.

The desktop setup entry can create a network, join an existing network, choose the Hub's listening port, and start the local service. It remains accessible when an existing paired connection fails, without first deleting the pairing record. Configuration is stored in the app's own data directory, and the service runs in the background. After signing in, workspace management supports adding projects and organizing their names and categories while the service is running. Changing the local Hub's listening port requires stopping the service. Newly created Hubs are local-only by default, so configure an HTTPS endpoint before connecting other devices. Mobile and browser apps expose Input Terminal and remote Hub Controller capabilities. Desktop Hub Controller also provides local configuration, service lifecycle, startup, and data maintenance through the native management interface.

**Local Device → Startup Settings** provides separate switches for startup at login and SSH reverse forwarding. On Windows, MewDex Hub starts quietly in the system tray after login. The installer asks whether to enable startup at login, with fresh installations defaulting to off and upgrades preserving the previous choice. macOS and Linux use their platform service entry points.

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
