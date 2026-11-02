//! The desktop remembers which local installation it manages. Existing profiles,
//! accounts and data stay in place; the Go manager owns their service operations.
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{fs, io, path::PathBuf, sync::Mutex};
use tauri::Manager;
use tauri_plugin_dialog::DialogExt;

#[derive(Deserialize, Serialize)]
struct Selection {
    root: PathBuf,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Installation {
    root: PathBuf,
    is_default: bool,
    development_override: bool,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Preview {
    id: String,
    root: PathBuf,
    is_default: bool,
    snapshot: Value,
}

#[derive(Default)]
pub(crate) struct PendingSelection(Mutex<Option<Preview>>);

fn default_root(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_local_data_dir()
        .map_err(|_| "无法定位本机设备目录".into())
}

fn settings_file(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    Ok(app
        .path()
        .app_config_dir()
        .map_err(|_| "无法定位桌面设置目录")?
        .join("local-installation.json"))
}

fn development_root() -> Option<PathBuf> {
    #[cfg(debug_assertions)]
    return std::env::var_os("MEWDEX_DEVICE_ROOT").map(PathBuf::from);
    #[cfg(not(debug_assertions))]
    None
}

pub(crate) fn root(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    if let Some(path) = development_root() {
        return Ok(path);
    }
    match fs::read(settings_file(app)?) {
        Ok(raw) => {
            let selected: Selection = serde_json::from_slice(&raw)
                .map_err(|_| "无法读取本机配置位置，请在数据与更新中重新选择")?;
            if !selected.root.is_absolute() {
                return Err("本机配置位置需要是绝对路径".into());
            }
            Ok(selected.root)
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => default_root(app),
        Err(_) => Err("无法读取本机配置位置，请在数据与更新中重新选择".into()),
    }
}

#[tauri::command]
pub(crate) fn local_installation_status(app: tauri::AppHandle) -> Result<Installation, String> {
    let root = root(&app)?;
    Ok(Installation {
        is_default: root == default_root(&app)?,
        root,
        development_override: development_root().is_some(),
    })
}

#[tauri::command]
pub(crate) async fn local_installation_prepare(
    app: tauri::AppHandle,
    mode: String,
) -> Result<Option<Preview>, String> {
    if development_root().is_some() {
        return Err("当前开发启动已指定本机配置位置，请在正常启动后切换".into());
    }
    tauri::async_runtime::spawn_blocking(move || {
        let root = match mode.as_str() {
            "default" => default_root(&app)?,
            "choose" => {
                let Some(selected) = app
                    .dialog()
                    .file()
                    .set_title("选择已有的 MewDex device.json")
                    .add_filter("MewDex 设备配置", &["json"])
                    .blocking_pick_file()
                else {
                    return Ok(None);
                };
                let path = selected.into_path().map_err(|_| "请选择本机设备配置文件")?;
                if !path
                    .file_name()
                    .is_some_and(|name| name.to_string_lossy().eq_ignore_ascii_case("device.json"))
                {
                    return Err("请选择已有设备目录中的 device.json".into());
                }
                path.parent()
                    .ok_or("无法定位设备配置目录")?
                    .to_path_buf()
            }
            _ => return Err("未知的本机配置选择方式".into()),
        };
        let snapshot = crate::desktop::installation_snapshot(&app, root.clone())?;
        if mode == "choose" && snapshot.get("configured").and_then(Value::as_bool) != Some(true) {
            return Err("所选位置没有可读取的 MewDex 设备配置".into());
        }
        let mut random = [0u8; 16];
        getrandom::fill(&mut random).map_err(|_| "无法准备本机配置选择")?;
        let preview = Preview {
            id: random.iter().map(|value| format!("{value:02x}")).collect(),
            is_default: root == default_root(&app)?,
            root,
            snapshot,
        };
        *app.state::<PendingSelection>()
            .0
            .lock()
            .map_err(|_| "本机配置选择暂时不可用")? = Some(preview.clone());
        Ok(Some(preview))
    })
    .await
    .map_err(|_| "无法打开本机配置选择")?
}

#[tauri::command]
pub(crate) async fn local_installation_apply(
    app: tauri::AppHandle,
    id: String,
) -> Result<Installation, String> {
    if development_root().is_some() {
        return Err("当前开发启动已指定本机配置位置".into());
    }
    tauri::async_runtime::spawn_blocking(move || {
        let pending = app.state::<PendingSelection>();
        let mut selected = pending.0.lock().map_err(|_| "本机配置选择暂时不可用")?;
        let preview = selected
            .as_ref()
            .filter(|value| value.id == id)
            .ok_or("请重新选择并查看本机配置")?;
        let snapshot = crate::desktop::installation_snapshot(&app, preview.root.clone())?;
        if snapshot.get("deviceId") != preview.snapshot.get("deviceId")
            || snapshot.get("configured") != preview.snapshot.get("configured")
        {
            return Err("所选设备配置已经变化，请重新选择".into());
        }
        let path = settings_file(&app)?;
        fs::create_dir_all(path.parent().ok_or("无法定位桌面设置目录")?)
            .map_err(|_| "无法创建桌面设置目录")?;
        let bytes = serde_json::to_vec_pretty(&json!({"root": preview.root}))
            .map_err(|_| "无法保存本机配置位置")?;
        let temporary = path.with_extension("json.tmp");
        fs::write(&temporary, bytes).map_err(|_| "无法保存本机配置位置")?;
        fs::rename(&temporary, &path).map_err(|_| "无法切换本机配置位置")?;
        let info = Installation {
            root: preview.root.clone(),
            is_default: preview.is_default,
            development_override: false,
        };
        *selected = None;
        Ok(info)
    })
    .await
    .map_err(|_| "无法切换本机配置位置")?
}
