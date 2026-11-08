use tauri::Emitter;
#[cfg(desktop)]
use tauri::Manager;

mod credential_store;
mod app_updates;
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
mod input_channels;
#[cfg(desktop)]
mod desktop_notifications;
#[cfg(desktop)]
mod desktop_window;
#[cfg(any(target_os = "windows", target_os = "macos"))]
mod desktop_window_tray;
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
    value["desktopPreferences"] = serde_json::json!(cfg!(any(target_os = "windows", target_os = "macos")));
    value["attachmentSave"] = serde_json::json!(cfg!(desktop));
    value["projectDirectory"] = serde_json::json!(false);
    value["messagingBridge"] = serde_json::json!(cfg!(desktop));
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
    #[cfg(any(target_os = "windows", target_os = "macos"))]
    let builder = builder.plugin(tauri_plugin_single_instance::init(|app, args, _cwd| {
        #[cfg(target_os = "windows")]
        notification_windows::activate(app, args);
        #[cfg(not(target_os = "windows"))]
        let _ = args;
        desktop_window_tray::show(app);
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
        .manage(app_updates::Updater::default())
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
                app_updates::app_update_info,
                app_updates::app_update_check,
                app_updates::app_update_download,
                app_updates::app_update_cancel,
                app_updates::app_update_install,
                input_channels::channel_connection,
                desktop::project_openers,
                desktop_notifications::desktop_notification_permission,
                desktop_notifications::desktop_notification_enable,
                desktop_notifications::desktop_notification_send,
                desktop_window::desktop_preferences,
                desktop_window::desktop_preferences_save,
                desktop_window::desktop_window_minimize,
                desktop_window::desktop_language,
                attachment_files::save_attachment_file,
                attachment_files::open_received_file
            ]);
    #[cfg(mobile)]
    let builder = builder.invoke_handler(tauri::generate_handler![
        vault::vault_load,
        vault::vault_save,
        vault::vault_clear,
        notification_actions::notification_take_action,
        input_capabilities,
        app_updates::app_update_info,
        app_updates::app_update_check
    ]);
    builder
        .setup(|app| {
            #[cfg(desktop)]
            app.manage(input_channels::Channels::default());
            #[cfg(any(target_os = "windows", target_os = "macos"))]
            desktop_window_tray::install(app)?;
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
            #[cfg(any(target_os = "windows", target_os = "macos"))]
            desktop_window_tray::window_event(window, event);
            if matches!(event, tauri::WindowEvent::Focused(true)) {
                let _ = window.emit("mewdex://resume", ());
            }
        })
        .build(tauri::generate_context!())
        .expect("MewDex client runtime failed")
        .run(|app, event| {
            #[cfg(desktop)]
            if matches!(event, tauri::RunEvent::Exit) {
                app.state::<input_channels::Channels>().close();
            }
            if matches!(event, tauri::RunEvent::Resumed) {
                let _ = app.emit("mewdex://resume", ());
            }
        });
}
mod vault;
