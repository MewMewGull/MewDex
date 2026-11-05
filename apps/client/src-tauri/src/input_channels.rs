//! The APP owns platform connections. The helper contains no Hub or model engine.
use serde::Deserialize;
use std::{
    io::Write,
    path::PathBuf,
    process::{Child, ChildStdin, Command, Stdio},
    sync::Mutex,
};
use tauri::Manager;

#[derive(Default)]
pub(crate) struct Channels(Mutex<State>);

impl Channels {
    pub(crate) fn close(&self) {
        if let Ok(mut state) = self.0.lock() {
            state.epoch = u64::MAX;
            state.process = None;
            state.key.clear();
        }
    }
}

#[derive(Default)]
struct State {
    epoch: u64,
    key: Vec<u8>,
    process: Option<Process>,
}

struct Process {
    child: Child,
    input: Option<ChildStdin>,
}

impl Drop for Process {
    fn drop(&mut self) {
        self.input.take();
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        while std::time::Instant::now() < deadline {
            if matches!(self.child.try_wait(), Ok(Some(_))) {
                return;
            }
            std::thread::sleep(std::time::Duration::from_millis(25));
        }
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[derive(Deserialize, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Connection {
    #[serde(rename = "baseURL")]
    base_url: String,
    token: String,
    hub_device_id: String,
    identity_id: String,
}

fn helper(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let name = if cfg!(target_os = "windows") {
        "mewdex-input-bridge.exe"
    } else {
        "mewdex-input-bridge"
    };
    let program = std::env::current_exe().map_err(|_| "Cannot locate the APP")?;
    let adjacent = program
        .parent()
        .ok_or("Cannot locate the APP directory")?
        .join(name);
    if adjacent.is_file() {
        return Ok(adjacent);
    }
    let resource = app
        .path()
        .resource_dir()
        .map_err(|_| "Cannot locate APP resources")?
        .join(name);
    if resource.is_file() {
        return Ok(resource);
    }
    Err("APP channel helper is missing".into())
}

#[tauri::command]
pub(crate) async fn channel_connection(
    app: tauri::AppHandle,
    epoch: u64,
    connection: Option<Connection>,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let channels = app.state::<Channels>();
        let mut state = channels
            .0
            .lock()
            .map_err(|_| "APP channel state unavailable")?;
        if epoch < state.epoch {
            return Ok(());
        }
        state.epoch = epoch;
        let Some(connection) = connection else {
            state.process = None;
            state.key.clear();
            return Ok(());
        };
        let key = serde_json::to_vec(&connection).map_err(|_| "Invalid APP channel connection")?;
        if key == state.key {
            if let Some(process) = &mut state.process {
                if matches!(process.child.try_wait(), Ok(None)) {
                    return Ok(());
                }
            }
        }
        state.process = None;
        state.key.clear();
        let root = app
            .path()
            .app_local_data_dir()
            .map_err(|_| "Cannot locate APP channel data")?
            .join("input-channels");
        let mut command = Command::new(helper(&app)?);
        command
            .arg("-root")
            .arg(root)
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        #[cfg(target_os = "windows")]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command
            .spawn()
            .map_err(|_| "Cannot start APP channel connections")?;
        let input = child.stdin.take();
        let mut process = Process { child, input };
        let input = process
            .input
            .as_mut()
            .ok_or("APP channel input unavailable")?;
        input
            .write_all(&key)
            .and_then(|_| input.write_all(b"\n"))
            .map_err(|_| "Cannot configure APP channel connections")?;
        state.key = key;
        state.process = Some(process);
        Ok(())
    })
    .await
    .map_err(|_| "APP channel worker failed".to_string())?
}
