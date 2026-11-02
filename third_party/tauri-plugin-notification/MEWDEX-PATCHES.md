# MewDex notification fixes

Upstream: `tauri-plugin-notification` 2.8.0 from crates.io, archive SHA-256
`5bc6c194a95a535abdfbe3bc1af9a43935b8b9b29c216b8e71af8b59132703cf`.
The upstream Apache-2.0 and MIT licenses remain included.
Modified upstream files carry a MewDex modification notice. The added
`ios/Tests/MewDexNotificationTests.swift` is licensed under Apache-2.0.

Changes cover native action delivery and dispatch results:

- iOS returns `extra` from the notification's stored `userInfo` and accepts
  notifications that are no longer in the current process's in-memory map.
- Android retains the latest action received before its first action listener,
  then consumes that pending action when the listener registers.
- The iOS package includes focused tests for delivered metadata with an empty
  process cache and older notifications without metadata.
- Desktop notification commands await the dispatch worker. Windows returns the
  actual toast dispatch result; Linux returns missing-bus and dispatch errors
  instead of discarding them. macOS still uses the upstream dispatch path.

The application registers an early Rust action listener and keeps a bounded
navigation hint until the input is ready. Opening a conversation still requires
a fresh network authorization check. No model text or credentials enter hints.

The Windows application additionally uses its own WinRT notification adapter
and per-user protocol routing in `apps/client/src-tauri/src/notification_*`.
That adapter supports activation from a stopped application through opaque
local navigation tickets and the single-instance entry point. It is separate
from the vendored plugin; source integration does not prove actual OS delivery.

This is project-local source; registry caches and global toolchains are unchanged.
It lives under `third_party` so Go does not interpret a root `vendor` directory
as its own dependency tree. Relocation does not modify upstream source content.
