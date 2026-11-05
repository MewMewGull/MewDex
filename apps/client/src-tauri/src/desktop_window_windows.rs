//! Window behavior belongs to the Windows input. Closing the workbench never
//! stops an execution service; the tray's explicit quit ends only this input.
use crate::desktop_window::Preferences;
use std::{
    fs, io,
    sync::{
        atomic::{AtomicBool, AtomicU8, Ordering},
        Mutex,
    },
};
use tauri::{
    menu::MenuBuilder,
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager,
};

struct WindowPreferences {
    close_to_tray: AtomicBool,
    minimize_to_tray: AtomicBool,
    language: AtomicU8,
    save: Mutex<()>,
}

fn labels(language: u8) -> [&'static str; 4] {
    match language {
        1 => ["開啟 MewDex", "結束工作台", "MewDex · 工作台", "開啟對話"],
        2 => [
            "Open MewDex",
            "Quit workbench",
            "MewDex · Workbench",
            "Open conversation",
        ],
        3 => [
            "MewDex を開く",
            "ワークベンチを終了",
            "MewDex · ワークベンチ",
            "会話を開く",
        ],
        4 => [
            "MewDex 열기",
            "워크벤치 종료",
            "MewDex · 워크벤치",
            "대화 열기",
        ],
        _ => ["打开 MewDex", "退出工作台", "MewDex · 工作台", "打开对话"],
    }
}

pub(crate) fn notification_open_label(app: &tauri::AppHandle) -> &'static str {
    labels(
        app.state::<WindowPreferences>()
            .language
            .load(Ordering::Relaxed),
    )[3]
}

pub(crate) fn set_language(app: &tauri::AppHandle, language: &str) -> Result<(), String> {
    let language = match language {
        "zh-CN" => 0,
        "zh-TW" => 1,
        "en" => 2,
        "ja" => 3,
        "ko" => 4,
        _ => return Err("Unsupported display language".into()),
    };
    let [open, quit, tooltip, _] = labels(language);
    let menu = MenuBuilder::new(app)
        .text("workbench.open", open)
        .separator()
        .text("workbench.quit", quit)
        .build()
        .map_err(|error| error.to_string())?;
    let tray = app
        .tray_by_id("mewdex-workbench")
        .ok_or("System tray unavailable")?;
    tray.set_menu(Some(menu))
        .map_err(|error| error.to_string())?;
    tray.set_tooltip(Some(tooltip))
        .map_err(|error| error.to_string())?;
    app.state::<WindowPreferences>()
        .language
        .store(language, Ordering::Relaxed);
    Ok(())
}

pub(crate) fn show(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
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
            minimize_to_tray: false,
        },
        Err(error) => return Err(error.into()),
    };
    app.manage(WindowPreferences {
        close_to_tray: AtomicBool::new(preferences.close_to_tray),
        minimize_to_tray: AtomicBool::new(preferences.minimize_to_tray),
        language: AtomicU8::new(0),
        save: Mutex::new(()),
    });
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
        close_to_tray: app
            .state::<WindowPreferences>()
            .close_to_tray
            .load(Ordering::Relaxed),
        minimize_to_tray: app
            .state::<WindowPreferences>()
            .minimize_to_tray
            .load(Ordering::Relaxed),
    }
}

pub(crate) fn save(
    app: &tauri::AppHandle,
    preferences: Preferences,
) -> Result<Preferences, String> {
    let state = app.state::<WindowPreferences>();
    let _guard = state.save.lock().map_err(|_| "无法保存桌面设置")?;
    let folder = app
        .path()
        .app_config_dir()
        .map_err(|_| "无法定位桌面设置目录")?;
    fs::create_dir_all(&folder).map_err(|_| "无法创建桌面设置目录")?;
    let body = serde_json::to_vec_pretty(&preferences).map_err(|_| "无法编码桌面设置")?;
    let path = folder.join("desktop-preferences.json");
    let temporary = folder.join("desktop-preferences.json.tmp");
    fs::write(&temporary, body).map_err(|_| "无法保存桌面设置")?;
    fs::rename(&temporary, &path).map_err(|_| "无法保存桌面设置")?;
    state
        .close_to_tray
        .store(preferences.close_to_tray, Ordering::Relaxed);
    state
        .minimize_to_tray
        .store(preferences.minimize_to_tray, Ordering::Relaxed);
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
    if matches!(event, tauri::WindowEvent::Resized(_))
        && preferences(window.app_handle()).minimize_to_tray
        && window.is_minimized().unwrap_or(false)
    {
        let _ = window.hide();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn older_preferences_keep_existing_close_behavior() {
        let preferences: Preferences = serde_json::from_str(r#"{"closeToTray":true}"#).unwrap();
        assert!(preferences.close_to_tray);
        assert!(!preferences.minimize_to_tray);
        let saved = serde_json::to_value(preferences).unwrap();
        assert_eq!(saved["minimizeToTray"], false);
    }
}
