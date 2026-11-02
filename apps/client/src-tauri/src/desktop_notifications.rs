//! Stable input-facing commands; platform code owns registration and delivery.
#[cfg(windows)]
pub(crate) use crate::notification_windows::Notice;
#[cfg(not(windows))]
pub(crate) type Notice = serde_json::Value;

#[tauri::command]
pub(crate) async fn desktop_notification_permission(
    app: tauri::AppHandle,
) -> Result<Option<bool>, String> {
    #[cfg(windows)]
    return crate::notification_windows::desktop_notification_permission(app).await;
    #[cfg(not(windows))]
    {
        let _ = app;
        Err("此平台暂未接入桌面提醒入口".into())
    }
}

#[tauri::command]
pub(crate) async fn desktop_notification_enable(app: tauri::AppHandle) -> Result<String, String> {
    #[cfg(windows)]
    return crate::notification_windows::desktop_notification_enable(app).await;
    #[cfg(not(windows))]
    {
        let _ = app;
        Err("此平台暂未接入桌面提醒入口".into())
    }
}

#[tauri::command]
pub(crate) async fn desktop_notification_send(
    app: tauri::AppHandle,
    options: Notice,
) -> Result<(), String> {
    #[cfg(windows)]
    return crate::notification_windows::desktop_notification_send(app, options).await;
    #[cfg(not(windows))]
    {
        let _ = (app, options);
        Err("此平台暂未接入桌面提醒入口".into())
    }
}
