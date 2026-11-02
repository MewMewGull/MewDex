//! Trusted desktop bridge. Configuration, routing and service lifecycle live in Go.
use std::{io::{Read, Write}, path::PathBuf, process::{Command, Stdio}, thread, time::{Duration, Instant}};
use serde_json::{json, Value};
use tauri::Manager;
use tauri_plugin_dialog::DialogExt;
use tauri_plugin_opener::OpenerExt;

fn helper(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let name = if cfg!(target_os = "windows") { "mewdex-node.exe" } else { "mewdex-node" };
    let exe = std::env::current_exe().map_err(|_| "Cannot find the application directory")?;
    let adjacent = exe.parent().ok_or("Cannot find the application directory")?.join(name);
    if adjacent.is_file() { return Ok(adjacent); }
    let resource = app.path().resource_dir().map_err(|_| "Cannot resolve application resources")?.join(name);
    if resource.is_file() { return Ok(resource); }
    Err("Local device service is missing from this installation".into())
}

fn engine(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let name = if cfg!(target_os = "windows") { "codex.exe" } else { "codex" };
    let path = app.path().resource_dir().map_err(|_| "Cannot resolve application resources")?.join("engine").join("bin").join(name);
    if path.is_file() { Ok(path) } else { Err("Bundled execution engine is missing".into()) }
}

pub fn capabilities(app: &tauri::AppHandle) -> Value {
    json!({"localService": true, "helperPresent": helper(app).is_ok(), "enginePresent": engine(app).is_ok()})
}

fn read_limited(reader: impl Read, limit: usize) -> Result<Vec<u8>, String> {
    let mut output = Vec::new();
    reader.take((limit + 1) as u64).read_to_end(&mut output).map_err(|_| "Local service output could not be read")?;
    if output.len() > limit { return Err("Local service response exceeded its size limit".into()); }
    Ok(output)
}

fn invoke_helper(binary: PathBuf, root: PathBuf, engine: Option<PathBuf>, request: Value) -> Result<Value, String> {
	// The service keeps the installed desktop path when its own executable is
	// staged under the data directory for upgrades and login startup.
	std::fs::create_dir_all(&root).map_err(|_| "Cannot create local device directory")?;
	let launcher = std::env::current_exe().map_err(|_| "Cannot locate the workbench")?;
	let mut nonce = [0u8; 8];
	getrandom::fill(&mut nonce).map_err(|_| "Cannot prepare workbench path")?;
	let temporary = root.join(format!("workbench.{}.tmp", u64::from_le_bytes(nonce)));
	std::fs::write(&temporary, serde_json::to_vec(&json!({"path": launcher})).map_err(|_| "Cannot save workbench path")?).map_err(|_| "Cannot save workbench path")?;
	std::fs::rename(temporary, root.join("workbench.json")).map_err(|_| "Cannot update workbench path")?;
    let body = serde_json::to_vec(&request).map_err(|_| "Invalid local service request")?;
    if body.len() > 64 * 1024 { return Err("Local service request is too large".into()); }
    let mut command = Command::new(binary);
    command.arg("desktop").arg("-root").arg(root);
    if let Some(path) = engine { command.arg("-engine-binary").arg(path); }
    command.stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::piped());
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW: helper is never an interactive console.
    }
    let mut child = command.spawn().map_err(|_| "Local service helper could not be started")?;
    let mut stdin = child.stdin.take().ok_or("Local helper input is unavailable")?;
    let stdout = child.stdout.take().ok_or("Local helper output is unavailable")?;
    let stderr = child.stderr.take().ok_or("Local helper diagnostics are unavailable")?;
    let writer = thread::spawn(move || stdin.write_all(&body));
    let output = thread::spawn(move || read_limited(stdout, 1024 * 1024));
    let errors = thread::spawn(move || read_limited(stderr, 16 * 1024));
    let seconds = if matches!(request.get("operation").and_then(Value::as_str), Some("backup" | "restore.prepare" | "restore.apply" | "upgrade.prepare" | "upgrade.apply")) { 600 } else { 45 };
    let seconds = if request.get("operation").and_then(Value::as_str) == Some("service.update") { 100 } else { seconds };
    let deadline = Instant::now() + Duration::from_secs(seconds);
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(20)),
            _ => {
                let _ = child.kill(); let _ = child.wait();
                let _ = writer.join(); let _ = output.join(); let _ = errors.join();
                return Err("Local operation remains unconfirmed; refresh its status before retrying".into());
            }
        }
    };
    let _ = writer.join(); let _ = errors.join();
    let raw = output.join().map_err(|_| "Local service reader failed")??;
    if !status.success() { return Err("Local service helper rejected the request".into()); }
    let response: Value = serde_json::from_slice(&raw).map_err(|_| "Invalid local service response")?;
    if response.get("version").and_then(Value::as_u64) != Some(1) { return Err("Unsupported local service protocol".into()); }
    if response.get("ok").and_then(Value::as_bool) != Some(true) {
        return Err(response.get("error").and_then(Value::as_str).unwrap_or("Local service operation failed").to_owned());
    }
    response.get("result").cloned().ok_or_else(|| "Local service response has no result".into())
}

#[tauri::command]
pub async fn device_request(app: tauri::AppHandle, request: Value) -> Result<Value, String> {
    let binary = helper(&app)?;
    let root = crate::desktop_installation::root(&app)?;
    let engine = engine(&app).ok();
    tauri::async_runtime::spawn_blocking(move || invoke_helper(binary, root, engine, request)).await.map_err(|_| "Local service worker failed".to_string())?
}

pub(crate) fn installation_snapshot(app: &tauri::AppHandle, root: PathBuf) -> Result<Value, String> {
    let result = invoke_helper(helper(app)?, root, engine(app).ok(), json!({"operation": "status"}))?;
    result.get("snapshot").cloned().ok_or_else(|| "本机设备没有返回配置状态".into())
}

#[tauri::command]
pub async fn open_project_directory(app: tauri::AppHandle, device_id: String, project_id: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let result = invoke_helper(helper(&app)?, crate::desktop_installation::root(&app)?, engine(&app).ok(), json!({"operation": "project.location", "expectedDeviceId": device_id, "projectId": project_id}))?;
        let path = result.get("project").and_then(|project| project.get("path")).and_then(Value::as_str).ok_or("本机没有返回项目目录")?;
        if !PathBuf::from(path).is_dir() { return Err("项目目录已经不可用".into()); }
        app.opener().reveal_item_in_dir(path).map_err(|_| "无法定位项目目录".to_string())
    }).await.map_err(|_| "项目目录操作没有完成".to_string())?
}

#[tauri::command]
pub async fn open_project_file(app: tauri::AppHandle, device_id: String, project_id: String, relative: String, action: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let result = invoke_helper(helper(&app)?, crate::desktop_installation::root(&app)?, engine(&app).ok(), json!({"operation": "project.location", "expectedDeviceId": device_id, "projectId": project_id}))?;
        let root = result.get("project").and_then(|project| project.get("path")).and_then(Value::as_str).ok_or("本机没有返回项目目录")?;
        let root = std::fs::canonicalize(root).map_err(|_| "项目目录已经不可用")?;
        let relative = std::path::Path::new(&relative);
        if relative.is_absolute() || relative.components().any(|part| matches!(part, std::path::Component::ParentDir | std::path::Component::Prefix(_))) { return Err("请选择工作区内的文件".into()); }
        let path = std::fs::canonicalize(root.join(relative)).map_err(|_| "文件已经不可用")?;
        if !path.starts_with(&root) { return Err("文件位置超出了当前工作区".into()); }
        open_os_path(&app, &path, &action)
    }).await.map_err(|_| "文件打开操作没有完成".to_string())?
}

pub(crate) fn open_os_path(app: &tauri::AppHandle, path: &std::path::Path, action: &str) -> Result<(), String> {
    let path_text = path.to_string_lossy().into_owned();
    match action {
        "default" => app.opener().open_path(path_text, None::<&str>).map_err(|_| "无法使用默认应用打开；请右键选择打开方式".to_string()),
        "reveal" => app.opener().reveal_item_in_dir(path).map_err(|_| "无法在文件浏览器中显示".to_string()),
        "open-with" => open_with(path),
        id if id.starts_with("editor:") => {
            let editor = installed_editors().into_iter().find(|editor| format!("editor:{}", editor.id) == id).ok_or("此编辑器当前没有安装")?;
            let mut command = Command::new(editor.path);
            command.arg(path).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
            #[cfg(target_os = "windows")]
            { use std::os::windows::process::CommandExt; command.creation_flags(0x08000000); }
            command.spawn().map(|_| ()).map_err(|_| "编辑器未能启动".to_string())
        }
        _ => Err("不支持此文件打开方式".into())
    }
}

#[cfg(target_os = "windows")]
fn open_with(path: &std::path::Path) -> Result<(), String> {
    use std::os::windows::ffi::OsStrExt;
    use windows::{core::PCWSTR, Win32::{System::Com::{CoInitializeEx, CoUninitialize, COINIT_APARTMENTTHREADED}, UI::Shell::{SHOpenWithDialog, OPENASINFO, OAIF_ALLOW_REGISTRATION, OAIF_EXEC}}};
    let filename: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    unsafe {
        let initialized = CoInitializeEx(None, COINIT_APARTMENTTHREADED).is_ok();
        let result = SHOpenWithDialog(None, &OPENASINFO {pcszFile: PCWSTR(filename.as_ptr()), pcszClass: PCWSTR::null(), oaifInFlags: OAIF_ALLOW_REGISTRATION | OAIF_EXEC});
        if initialized { CoUninitialize(); }
        // Closing the application picker is a normal cancellation.
        match result { Ok(()) => Ok(()), Err(error) if error.code().0 as u32 == 0x800704c7 => Ok(()), Err(_) => Err("系统打开方式窗口不可用".into()) }
    }
}
#[cfg(not(target_os = "windows"))]
fn open_with(_path: &std::path::Path) -> Result<(), String> { Err("此系统暂未提供应用选择器".into()) }

#[derive(serde::Serialize)]
pub struct InstalledEditor { id: String, name: String, #[serde(skip_serializing)] path: PathBuf }

fn installed_editors() -> Vec<InstalledEditor> {
    #[cfg(target_os = "windows")]
    {
        use winreg::{RegKey, enums::{HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE}};
        let choices = [("code", "Visual Studio Code", "Code.exe", "Microsoft VS Code"), ("cursor", "Cursor", "Cursor.exe", "cursor"), ("windsurf", "Windsurf", "Windsurf.exe", "Windsurf"), ("notepadpp", "Notepad++", "notepad++.exe", "Notepad++"), ("idea", "IntelliJ IDEA", "idea64.exe", "JetBrains\\IntelliJ IDEA\\bin")];
        let mut editors = Vec::new();
        for (id, name, executable, directory) in choices {
            let mut candidates = Vec::new();
            for hive in [HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE] {
                if let Ok(key) = RegKey::predef(hive).open_subkey(format!("Software\\Microsoft\\Windows\\CurrentVersion\\App Paths\\{executable}")) {
                    if let Ok(value) = key.get_value::<String, _>("") { candidates.push(PathBuf::from(value.trim_matches('"'))); }
                }
            }
            if let Some(local) = std::env::var_os("LOCALAPPDATA") { candidates.push(PathBuf::from(local).join("Programs").join(directory).join(executable)); }
            for variable in ["ProgramFiles", "ProgramFiles(x86)"] {
                if let Some(base) = std::env::var_os(variable) { candidates.push(PathBuf::from(base).join(directory).join(executable)); }
            }
            if let Some(path) = candidates.into_iter().find(|path| path.is_file()) {editors.push(InstalledEditor{id:id.into(), name:name.into(), path});}
        }
        editors
    }
    #[cfg(not(target_os = "windows"))]
    { Vec::new() }
}
#[tauri::command]
pub fn project_openers() -> Vec<InstalledEditor> { installed_editors() }

#[tauri::command]
pub async fn choose_project(app: tauri::AppHandle) -> Result<Option<String>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        app.dialog().file().set_title("选择允许执行任务的项目目录").blocking_pick_folder()
            .map(|path| path.into_path().map(|path| path.to_string_lossy().into_owned()).map_err(|_| "Selected project is not a local directory".to_string())).transpose()
    }).await.map_err(|_| "Project picker failed".to_string())?
}

#[tauri::command]
pub async fn choose_backup(app: tauri::AppHandle, mode: String) -> Result<Option<String>, String> {
    if mode != "open" && mode != "save" { return Err("Invalid backup picker mode".into()); }
    tauri::async_runtime::spawn_blocking(move || {
        let picker = app.dialog().file().add_filter("MewDex 加密备份", &["mewdex-backup", "age"]);
        let selected = if mode == "open" {
            picker.set_title("选择 MewDex 备份").blocking_pick_file()
        } else {
            picker.set_title("保存 MewDex 加密备份").set_file_name("mewdex-device.mewdex-backup").blocking_save_file()
        };
        selected.map(|path| path.into_path().map(|path| path.to_string_lossy().into_owned())
            .map_err(|_| "Selected backup must be a local file".to_string())).transpose()
    }).await.map_err(|_| "Backup picker failed".to_string())?
}
