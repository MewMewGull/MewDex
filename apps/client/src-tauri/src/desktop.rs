//! Native input conveniences for opening received files and editor selection.
use serde_json::{json, Value};
use std::{
    path::PathBuf,
    process::{Command, Stdio},
};
use tauri_plugin_opener::OpenerExt;

pub fn capabilities(_app: &tauri::AppHandle) -> Value {
    json!({"localService": false, "helperPresent": false, "enginePresent": false})
}

pub(crate) fn open_os_path(
    app: &tauri::AppHandle,
    path: &std::path::Path,
    action: &str,
) -> Result<(), String> {
    let path_text = path.to_string_lossy().into_owned();
    match action {
        "default" => app
            .opener()
            .open_path(path_text, None::<&str>)
            .map_err(|_| "无法使用默认应用打开；请右键选择打开方式".to_string()),
        "reveal" => app
            .opener()
            .reveal_item_in_dir(path)
            .map_err(|_| "无法在文件浏览器中显示".to_string()),
        "open-with" => open_with(path),
        id if id.starts_with("editor:") => {
            let editor = installed_editors()
                .into_iter()
                .find(|editor| format!("editor:{}", editor.id) == id)
                .ok_or("此编辑器当前没有安装")?;
            let mut command = Command::new(editor.path);
            command
                .arg(path)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null());
            #[cfg(target_os = "windows")]
            {
                use std::os::windows::process::CommandExt;
                command.creation_flags(0x08000000);
            }
            command
                .spawn()
                .map(|_| ())
                .map_err(|_| "编辑器未能启动".to_string())
        }
        _ => Err("不支持此文件打开方式".into()),
    }
}

#[cfg(target_os = "windows")]
fn open_with(path: &std::path::Path) -> Result<(), String> {
    use std::os::windows::ffi::OsStrExt;
    use windows::{
        core::PCWSTR,
        Win32::{
            System::Com::{CoInitializeEx, CoUninitialize, COINIT_APARTMENTTHREADED},
            UI::Shell::{SHOpenWithDialog, OAIF_ALLOW_REGISTRATION, OAIF_EXEC, OPENASINFO},
        },
    };
    let filename: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    unsafe {
        let initialized = CoInitializeEx(None, COINIT_APARTMENTTHREADED).is_ok();
        let result = SHOpenWithDialog(
            None,
            &OPENASINFO {
                pcszFile: PCWSTR(filename.as_ptr()),
                pcszClass: PCWSTR::null(),
                oaifInFlags: OAIF_ALLOW_REGISTRATION | OAIF_EXEC,
            },
        );
        if initialized {
            CoUninitialize();
        }
        // Closing the application picker is a normal cancellation.
        match result {
            Ok(()) => Ok(()),
            Err(error) if error.code().0 as u32 == 0x800704c7 => Ok(()),
            Err(_) => Err("系统打开方式窗口不可用".into()),
        }
    }
}
#[cfg(not(target_os = "windows"))]
fn open_with(_path: &std::path::Path) -> Result<(), String> {
    Err("此系统暂未提供应用选择器".into())
}

#[derive(serde::Serialize)]
pub struct InstalledEditor {
    id: String,
    name: String,
    #[serde(skip_serializing)]
    path: PathBuf,
}

fn installed_editors() -> Vec<InstalledEditor> {
    #[cfg(target_os = "windows")]
    {
        use winreg::{
            enums::{HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE},
            RegKey,
        };
        let choices = [
            (
                "code",
                "Visual Studio Code",
                "Code.exe",
                "Microsoft VS Code",
            ),
            ("cursor", "Cursor", "Cursor.exe", "cursor"),
            ("windsurf", "Windsurf", "Windsurf.exe", "Windsurf"),
            ("notepadpp", "Notepad++", "notepad++.exe", "Notepad++"),
            (
                "idea",
                "IntelliJ IDEA",
                "idea64.exe",
                "JetBrains\\IntelliJ IDEA\\bin",
            ),
        ];
        let mut editors = Vec::new();
        for (id, name, executable, directory) in choices {
            let mut candidates = Vec::new();
            for hive in [HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE] {
                if let Ok(key) = RegKey::predef(hive).open_subkey(format!(
                    "Software\\Microsoft\\Windows\\CurrentVersion\\App Paths\\{executable}"
                )) {
                    if let Ok(value) = key.get_value::<String, _>("") {
                        candidates.push(PathBuf::from(value.trim_matches('"')));
                    }
                }
            }
            if let Some(local) = std::env::var_os("LOCALAPPDATA") {
                candidates.push(
                    PathBuf::from(local)
                        .join("Programs")
                        .join(directory)
                        .join(executable),
                );
            }
            for variable in ["ProgramFiles", "ProgramFiles(x86)"] {
                if let Some(base) = std::env::var_os(variable) {
                    candidates.push(PathBuf::from(base).join(directory).join(executable));
                }
            }
            if let Some(path) = candidates.into_iter().find(|path| path.is_file()) {
                editors.push(InstalledEditor {
                    id: id.into(),
                    name: name.into(),
                    path,
                });
            }
        }
        editors
    }
    #[cfg(not(target_os = "windows"))]
    {
        Vec::new()
    }
}
#[tauri::command]
pub fn project_openers() -> Vec<InstalledEditor> {
    installed_editors()
}
