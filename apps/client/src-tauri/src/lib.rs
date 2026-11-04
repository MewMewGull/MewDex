use tauri::Emitter;
#[cfg(desktop)]
use tauri::Manager;

mod credential_store;
mod notification_actions;
#[cfg(target_os = "windows")]
mod notification_registration_windows;
#[cfg(all(windows, debug_assertions, feature = "notification-risk"))]
pub mod notification_risk;
mod notification_routes;
#[cfg(target_os = "windows")]
mod notification_windows;

#[cfg(desktop)]
mod attachment_files;
#[cfg(desktop)]
mod desktop;
#[cfg(desktop)]
mod desktop_installation;
#[cfg(desktop)]
mod desktop_notifications;
#[cfg(desktop)]
mod desktop_window;
#[cfg(target_os = "windows")]
mod desktop_window_windows;
#[cfg(debug_assertions)]
mod diagnostics;

#[tauri::command]
fn input_capabilities(app: tauri::AppHandle) -> serde_json::Value {
    #[cfg(desktop)]
    let mut value = desktop::capabilities(&app);
    #[cfg(mobile)]
    let mut value = {
        let _ = app;
        serde_json::json!({"localService": false, "helperPresent": false, "enginePresent": false})
    };
    value["notifications"] = serde_json::json!(true);
    value["notificationActions"] = serde_json::json!(true);
    value["desktopNotifications"] = serde_json::json!(cfg!(target_os = "windows"));
    value["desktopPreferences"] = serde_json::json!(cfg!(target_os = "windows"));
    value["attachmentSave"] = serde_json::json!(cfg!(desktop));
    value["projectDirectory"] = serde_json::json!(cfg!(desktop));
    let product: serde_json::Value = serde_json::from_str(include_str!("../../../../mewdex.product.json"))
        .expect("valid bundled MewDex component manifest");
    value["components"] = product["platforms"][std::env::consts::OS]["components"].clone();
    #[cfg(debug_assertions)]
    diagnostics::emit("input-capabilities", value.clone());
    value
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();
    #[cfg(target_os = "windows")]
    let builder = builder.plugin(tauri_plugin_single_instance::init(|app, args, _cwd| {
        notification_windows::activate(app, args);
        desktop_window_windows::show(app);
    }));
    #[cfg(target_os = "windows")]
    let builder = builder.plugin(
        tauri_plugin_window_state::Builder::default()
            .with_state_flags(
                tauri_plugin_window_state::StateFlags::SIZE
                    | tauri_plugin_window_state::StateFlags::POSITION
                    | tauri_plugin_window_state::StateFlags::MAXIMIZED,
            )
            .build(),
    );
    let builder = builder
        .manage(notification_actions::Inbox::default())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init());
    #[cfg(desktop)]
    let builder =
        builder
            .plugin(tauri_plugin_dialog::init())
            .invoke_handler(tauri::generate_handler![
                vault::vault_load,
                vault::vault_save,
                vault::vault_clear,
                notification_actions::notification_take_action,
                input_capabilities,
                desktop::device_request,
                desktop::choose_project,
                desktop::open_project_directory,
                desktop::open_project_file,
                desktop::project_openers,
                desktop::choose_backup,
                desktop_installation::local_installation_status,
                desktop_installation::local_installation_prepare,
                desktop_installation::local_installation_apply,
                desktop_notifications::desktop_notification_permission,
                desktop_notifications::desktop_notification_enable,
                desktop_notifications::desktop_notification_send,
                desktop_window::desktop_preferences,
                desktop_window::desktop_preferences_save,
                attachment_files::save_attachment_file,
                attachment_files::open_received_file
            ]);
    #[cfg(mobile)]
    let builder = builder.invoke_handler(tauri::generate_handler![
        vault::vault_load,
        vault::vault_save,
        vault::vault_clear,
        notification_actions::notification_take_action,
        input_capabilities
    ]);
    builder
        .setup(|app| {
            #[cfg(desktop)]
            app.manage(desktop_installation::PendingSelection::default());
            #[cfg(target_os = "windows")]
            desktop_window_windows::install(app)?;
            notification_actions::install(app.handle());
            #[cfg(target_os = "windows")]
            notification_windows::activate(app.handle(), std::env::args());
            #[cfg(debug_assertions)]
            diagnostics::startup(app.handle());
            #[cfg(not(debug_assertions))]
            let _ = app;
            Ok(())
        })
        .on_window_event(|window, event| {
            #[cfg(target_os = "windows")]
            desktop_window_windows::window_event(window, event);
            if matches!(event, tauri::WindowEvent::Focused(true)) {
                let _ = window.emit("mewdex://resume", ());
            }
        })
        .build(tauri::generate_context!())
        .expect("MewDex client runtime failed")
        .run(|app, event| {
            if matches!(event, tauri::RunEvent::Resumed) {
                let _ = app.emit("mewdex://resume", ());
            }
        });
}
mod vault;
