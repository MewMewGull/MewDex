use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Preferences {
    pub close_to_tray: bool,
}

#[tauri::command]
pub(crate) fn desktop_preferences(app: tauri::AppHandle) -> Result<Preferences, String> {
    #[cfg(target_os = "windows")]
    return Ok(crate::desktop_window_windows::preferences(&app));
    #[cfg(not(target_os = "windows"))]
    {
        let _ = app;
        Err("当前平台没有 Windows 托盘偏好设置".into())
    }
}

#[tauri::command]
pub(crate) async fn desktop_preferences_save(
    app: tauri::AppHandle,
    preferences: Preferences,
) -> Result<Preferences, String> {
    #[cfg(target_os = "windows")]
    return tauri::async_runtime::spawn_blocking(move || {
        crate::desktop_window_windows::save(&app, preferences)
    })
    .await
    .map_err(|_| "无法保存桌面偏好设置")?;
    #[cfg(not(target_os = "windows"))]
    {
        let _ = (app, preferences);
        Err("当前平台没有 Windows 托盘偏好设置".into())
    }
}
