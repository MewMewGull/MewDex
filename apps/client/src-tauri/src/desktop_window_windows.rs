//! Window behavior belongs to the Windows input. Closing the workbench never
//! stops an execution service; the tray's explicit quit ends only this input.
use crate::desktop_window::Preferences;
use std::{
    fs, io,
    sync::atomic::{AtomicBool, Ordering},
};
use tauri::{
    menu::MenuBuilder,
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager,
};

struct WindowPreferences(AtomicBool);

pub(crate) fn show(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

pub(crate) fn install(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let path = app
        .path()
        .app_config_dir()?
        .join("desktop-preferences.json");
    let preferences: Preferences = match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes)?,
        Err(error) if error.kind() == io::ErrorKind::NotFound => Preferences {
            close_to_tray: true,
        },
        Err(error) => return Err(error.into()),
    };
    app.manage(WindowPreferences(AtomicBool::new(
        preferences.close_to_tray,
    )));
    let menu = MenuBuilder::new(app)
        .text("workbench.open", "打开 MewDex")
        .separator()
        .text("workbench.quit", "退出工作台")
        .build()?;
    TrayIconBuilder::with_id("mewdex-workbench")
        .icon(tauri::include_image!("icons/32x32.png"))
        .tooltip("MewDex · 工作台")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "workbench.open" => show(app),
            "workbench.quit" => app.exit(0),
            _ => (),
        })
        .on_tray_icon_event(|tray, event| {
            if matches!(
                event,
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                } | TrayIconEvent::DoubleClick {
                    button: MouseButton::Left,
                    ..
                }
            ) {
                show(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

pub(crate) fn preferences(app: &tauri::AppHandle) -> Preferences {
    Preferences {
        close_to_tray: app.state::<WindowPreferences>().0.load(Ordering::Relaxed),
    }
}

pub(crate) fn save(
    app: &tauri::AppHandle,
    preferences: Preferences,
) -> Result<Preferences, String> {
    let folder = app
        .path()
        .app_config_dir()
        .map_err(|_| "无法定位桌面设置目录")?;
    fs::create_dir_all(&folder).map_err(|_| "无法创建桌面设置目录")?;
    let body = serde_json::to_vec_pretty(&preferences).map_err(|_| "无法编码桌面设置")?;
    fs::write(folder.join("desktop-preferences.json"), body).map_err(|_| "无法保存桌面设置")?;
    app.state::<WindowPreferences>()
        .0
        .store(preferences.close_to_tray, Ordering::Relaxed);
    Ok(preferences)
}

pub(crate) fn window_event(window: &tauri::Window, event: &tauri::WindowEvent) {
    if window.label() != "main" {
        return;
    }
    if let tauri::WindowEvent::CloseRequested { api, .. } = event {
        if preferences(window.app_handle()).close_to_tray && window.hide().is_ok() {
            api.prevent_close();
        }
    }
}
