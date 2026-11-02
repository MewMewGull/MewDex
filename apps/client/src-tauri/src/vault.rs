//! Native commands keep keychain I/O away from the app's UI thread.
use serde_json::Value;

#[cfg(any(test, debug_assertions))]
pub(crate) use crate::credential_store::probe_storage;

async fn blocking<T: Send + 'static>(
    operation: impl FnOnce() -> Result<T, String> + Send + 'static,
) -> Result<T, String> {
    tauri::async_runtime::spawn_blocking(operation)
        .await
        .map_err(|_| "系统凭据库请求未完成，请重试".to_string())?
}

#[tauri::command]
pub async fn vault_load() -> Result<Option<Value>, String> {
    blocking(crate::credential_store::load).await
}

#[tauri::command]
pub async fn vault_save(connection: Value) -> Result<(), String> {
    blocking(move || crate::credential_store::save(connection)).await
}

#[tauri::command]
pub async fn vault_clear() -> Result<(), String> {
    blocking(crate::credential_store::clear).await
}
