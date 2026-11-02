//! Saving an attachment is a user-selected file operation. Networking and Hub
//! credentials stay in the input SDK; this bridge receives only file bytes.
use tauri::{
    ipc::{InvokeBody, Request},
    AppHandle,
    Manager,
};
use tauri_plugin_dialog::DialogExt;

#[tauri::command]
pub(crate) async fn save_attachment_file(
    app: AppHandle,
    request: Request<'_>,
) -> Result<bool, String> {
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) if bytes.len() <= 20 * 1024 * 1024 => bytes.clone(),
        _ => return Err("附件内容无效或超过 20 MiB".into()),
    };
    let header = request
        .headers()
        .get("x-mewdex-file")
        .and_then(|value| value.to_str().ok())
        .ok_or("附件文件名缺失")?;
    let name = url::form_urlencoded::parse(header.as_bytes())
        .find(|(key, _)| key == "name")
        .map(|(_, value)| value.into_owned())
        .ok_or("附件文件名缺失")?;
    let name = name.rsplit(['/', '\\']).next().filter(|value| !value.is_empty()).unwrap_or("attachment").to_owned();
    tauri::async_runtime::spawn_blocking(move || {
        let selected = app
            .dialog()
            .file()
            .set_title("保存附件")
            .set_file_name(&name)
            .blocking_save_file();
        let Some(selected) = selected else {
            return Ok(false);
        };
        let path = selected.into_path().map_err(|_| "请选择本机文件位置")?;
        std::fs::write(path, bytes).map_err(|_| "无法保存附件，请检查目标目录和磁盘空间")?;
        Ok(true)
    })
    .await
    .map_err(|_| "附件保存窗口暂时不可用")?
}

#[tauri::command]
pub(crate) async fn open_received_file(app: AppHandle, request: Request<'_>) -> Result<String, String> {
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) if bytes.len() <= 20 * 1024 * 1024 => bytes.clone(),
        _ => return Err("文件内容无效或超过 20 MiB".into()),
    };
    let header = request.headers().get("x-mewdex-file").and_then(|value| value.to_str().ok()).ok_or("文件名缺失")?;
    let fields: std::collections::HashMap<_, _> = url::form_urlencoded::parse(header.as_bytes()).into_owned().collect();
    let name = fields.get("name").ok_or("文件名缺失")?.rsplit(['/', '\\']).next().unwrap_or("file");
    let name: String = name.chars().map(|c| if c.is_control() || "<>:\"/\\|?*".contains(c) {'_'} else {c}).collect();
    if name.trim_matches([' ', '.']).is_empty() { return Err("文件名无效".into()); }
    let action = fields.get("action").cloned().unwrap_or_else(|| "default".into());
    tauri::async_runtime::spawn_blocking(move || {
        let mut random = [0u8; 16];
        getrandom::fill(&mut random).map_err(|_| "无法创建本机文件副本")?;
        let key: String = random.iter().map(|byte| format!("{byte:02x}")).collect();
        let directory = app.path().app_cache_dir().map_err(|_| "无法找到文件缓存目录")?.join("received-files").join(key);
        std::fs::create_dir_all(&directory).map_err(|_| "无法创建文件副本目录")?;
        let path = directory.join(name);
        std::fs::write(&path, bytes).map_err(|_| "无法保存文件副本")?;
        crate::desktop::open_os_path(&app, &path, &action)?;
        Ok(path.to_string_lossy().into_owned())
    }).await.map_err(|_| "文件打开操作没有完成".to_string())?
}
