// Copyright 2019-2023 Tauri Programme within The Commons Conservancy
// SPDX-License-Identifier: Apache-2.0
// SPDX-License-Identifier: MIT
// Modified for MewDex; see MEWDEX-PATCHES.md in the plugin root.

use serde::{Serialize, ser::Serializer};

/// Alias for a [`std::result::Result`] with the error type set to [`Error`].
pub type Result<T> = std::result::Result<T, Error>;

/// Errors returned by the notification plugin.
///
/// The error is serialized to its [`Display`](std::fmt::Display) string when it crosses the IPC boundary.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// An I/O operation failed, e.g. resolving the path of the running executable on Windows.
    #[error(transparent)]
    Io(#[from] std::io::Error),
    /// The desktop notification server rejected the dispatch. The IPC message
    /// excludes notification contents and platform error text.
    #[cfg(desktop)]
    #[error("system notification dispatch was rejected")]
    Dispatch,
    /// The native blocking worker did not complete its operation.
    #[cfg(desktop)]
    #[error("system notification worker did not complete")]
    Worker,
    /// Failed to run a command on the mobile plugin implementation (Kotlin on Android, Swift on iOS).
    ///
    /// Only available on mobile.
    #[cfg(mobile)]
    #[error(transparent)]
    PluginInvoke(#[from] tauri::plugin::mobile::PluginInvokeError),
}

impl Serialize for Error {
    fn serialize<S>(&self, serializer: S) -> std::result::Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_str(self.to_string().as_ref())
    }
}
