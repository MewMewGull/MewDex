//! Platform credential operations, independent of Tauri and the input UI.
//! Only this app's connection entry and explicit temporary probes are accessed.
use keyring_core::{Entry, Error};
use serde_json::Value;
use std::sync::{Mutex, OnceLock};

const SERVICE: &str = "app.mewdex.client";
const USER: &str = "network-connection";
static INITIALIZED: OnceLock<Result<(), String>> = OnceLock::new();
static SERIAL: Mutex<()> = Mutex::new(());

fn initialize() -> Result<(), String> {
    INITIALIZED
        .get_or_init(|| {
            #[cfg(target_os = "windows")]
            let store = windows_native_keyring_store::Store::new().map_err(|e| e.to_string())?;
            #[cfg(target_os = "macos")]
            let store =
                apple_native_keyring_store::keychain::Store::new().map_err(|e| e.to_string())?;
            #[cfg(target_os = "ios")]
            let store =
                apple_native_keyring_store::protected::Store::new().map_err(|e| e.to_string())?;
            #[cfg(target_os = "android")]
            let store = android_native_keyring_store::Store::new_with_configuration(
                &std::collections::HashMap::from([("name", "mewdex-connection")]),
            )
            .map_err(|e| e.to_string())?;
            #[cfg(target_os = "linux")]
            let store = zbus_secret_service_keyring_store::Store::new()
                .map_err(|e| format!("Linux 系统凭据服务不可用：{e}"))?;
            #[cfg(any(
                target_os = "windows",
                target_os = "macos",
                target_os = "ios",
                target_os = "android",
                target_os = "linux"
            ))]
            {
                keyring_core::set_default_store(store);
                Ok(())
            }
            #[cfg(not(any(
                target_os = "windows",
                target_os = "macos",
                target_os = "ios",
                target_os = "android",
                target_os = "linux"
            )))]
            Err("Native credential storage is not configured for this platform".into())
        })
        .clone()
}

fn named_entry(service: &str, user: &str) -> Result<Entry, String> {
    initialize()?;
    #[cfg(target_os = "ios")]
    return Entry::new_with_modifiers(
        service,
        user,
        &std::collections::HashMap::from([("access-policy", "WhenUnlockedThisDeviceOnly")]),
    )
    .map_err(|e| e.to_string());
    #[cfg(not(target_os = "ios"))]
    Entry::new(service, user).map_err(|e| e.to_string())
}

fn entry() -> Result<Entry, String> {
    named_entry(SERVICE, USER)
}

#[cfg(any(test, debug_assertions))]
pub(crate) fn probe_storage() -> Result<(), String> {
    let _guard = SERIAL.lock().map_err(|_| "Credential store is busy")?;
    let user = format!(
        "probe-{}-{:?}",
        std::process::id(),
        std::time::SystemTime::now()
    );
    let credential = named_entry("app.mewdex.client.development-probe", &user)?;
    struct Cleanup(Entry);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = self.0.delete_credential();
        }
    }
    let cleanup = Cleanup(credential);
    cleanup
        .0
        .set_password("mewdex-native-storage-probe")
        .map_err(|e| e.to_string())?;
    if cleanup.0.get_password().map_err(|e| e.to_string())? != "mewdex-native-storage-probe" {
        return Err("Native probe entry did not retain its value".into());
    }
    cleanup.0.delete_credential().map_err(|e| e.to_string())?;
    if !matches!(cleanup.0.get_password(), Err(Error::NoEntry)) {
        return Err("Native probe entry deletion could not be confirmed".into());
    }
    Ok(())
}

pub(crate) fn load() -> Result<Option<Value>, String> {
    let _guard = SERIAL.lock().map_err(|_| "Credential store is busy")?;
    match entry()?.get_password() {
        Ok(raw) => serde_json::from_str(&raw)
            .map(Some)
            .map_err(|_| "Stored MewDex connection is invalid".into()),
        Err(Error::NoEntry) => Ok(None),
        Err(error) => Err(error.to_string()),
    }
}

pub(crate) fn save(connection: Value) -> Result<(), String> {
    if !connection.get("baseURL").is_some_and(Value::is_string)
        || !connection
            .get("credentials")
            .and_then(|v| v.get("token"))
            .is_some_and(Value::is_string)
    {
        return Err("Invalid MewDex connection".into());
    }
    let _guard = SERIAL.lock().map_err(|_| "Credential store is busy")?;
    entry()?
        .set_password(&connection.to_string())
        .map_err(|e| e.to_string())
}

pub(crate) fn clear() -> Result<(), String> {
    let _guard = SERIAL.lock().map_err(|_| "Credential store is busy")?;
    match entry()?.delete_credential() {
        Ok(()) | Err(Error::NoEntry) => Ok(()),
        Err(error) => Err(error.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[ignore = "Explicit native delivery-risk probe; creates and removes only its own temporary entry"]
    fn native_round_trip() {
        probe_storage().expect("Native credential storage round trip failed");
    }
}
