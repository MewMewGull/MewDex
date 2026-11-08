# Linux executor sandbox preparation

The Linux x64 Executor ships the pinned Codex Linux sandbox module and Bubblewrap in `sandbox/`. Run the Executor under the account that owns its configuration and project directories.

Ubuntu hosts can mediate unprivileged user namespaces through AppArmor. A sandbox probe can then fail with `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`. The Executor provides a preparation preview and an explicit administrator apply command for hosts with `kernel.apparmor_restrict_unprivileged_userns=1`.

## Inspect the preparation

Run this command from the extracted Executor package.

```sh
./mewdex-executor sandbox-setup -phase inspect
```

The JSON report includes the source executable digest, helper path, complete AppArmor profile, current file checks and loaded profile state. `-sandbox-binary /path/to/mewdex-sandbox` selects an existing runtime when the service uses a staged installation. The Bubblewrap resource must be beside that native runtime.

This preparation pins Bubblewrap to SHA256 `01fb705f067bd5365b63d8ad2323a61c8d007733ca5e649437e086f3fb9935d8`, as recorded in `sandbox/source.json`. It uses the following destinations.

- `/usr/libexec/mewdex-sandbox/<sha256>/bwrap`
- `/etc/apparmor.d/mewdex-bwrap-<sha256>`

The helper, its directories and the AppArmor profile require root ownership and protected permissions. Parent directories must allow traversal by the Executor account. The profile attaches to the exact helper path and grants `userns` through an application-specific unconfined AppArmor profile. File, network and process restrictions are applied by the pinned native sandbox around each command.

## Apply the reviewed profile

An administrator reviews the emitted profile and digest, then runs the apply phase with explicit confirmation.

```sh
sudo ./mewdex-executor sandbox-setup \
  -phase apply \
  -expected-sha256 01fb705f067bd5365b63d8ad2323a61c8d007733ca5e649437e086f3fb9935d8 \
  -confirm-userns
```

The apply phase requires the host AppArmor parser and validates the profile syntax before loading it. Existing files with different contents or unsafe permissions cause an error. Matching prepared files are reused. Failed loading attempts report the error and attempt to recover files created during that invocation.

The Executor checks the helper digest, root ownership, executable permissions and parent directories before selecting it through the native sandbox process's `PATH`. The pinned Codex launcher discovers that system helper through its existing launcher selection.

## Verify as the Executor account

Run this diagnostic as the actual service account, with that account's existing Executor profile.

```sh
./mewdex-executor doctor -config /path/to/executor/device.json -engine
```

`localSandboxAvailable` reports the real local probe result. `localSandboxVersion` identifies the native runtime. `localSandboxError` includes a preparation hint when the host's AppArmor restriction prevents startup. The diagnostic creates a runtime home below the configured data directory and reports `inferenceRun` as `false`.

An administrator can repeat `sandbox-setup -phase inspect` to confirm `configured`, including kernel profile loading. Reading loaded AppArmor policy can require administrator privileges.

The preparation preview, explicit-confirmation refusals and profile syntax are covered by validation. Unprivileged file access, network isolation and process cleanup require acceptance on a host where an administrator has applied the profile.
