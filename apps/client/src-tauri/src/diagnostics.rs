//! Opt-in native delivery-risk observations. This module is absent in release.
use serde_json::{json, Value};
use std::{fs::{File, OpenOptions}, io::Write, sync::{Mutex, OnceLock}};
use tauri::Manager;

const PREFIX: &str = "[MewDex development probe] ";
static OUTPUT: OnceLock<Mutex<File>> = OnceLock::new();

fn enabled() -> bool {
    static ENABLED: OnceLock<bool> = OnceLock::new();
    *ENABLED.get_or_init(|| std::env::var("MEWDEX_NATIVE_RISK").as_deref() == Ok("1"))
}

pub fn emit(stage: &str, observation: Value) {
    if enabled() {
        let record = json!({"stage": stage, "observation": observation}).to_string();
        if let Some(output) = OUTPUT.get() {
            if let Ok(mut file) = output.lock() {
                let _ = writeln!(file, "{}", record);
                let _ = file.sync_data();
            }
        }
        println!("{}{}", PREFIX, record);
    }
}

pub fn startup(app: &tauri::AppHandle) {
    if !enabled() { return; }
    // iOS redirects stdout into its platform log. Keep only these explicit,
    // non-secret probe observations in this app's sandbox as well.
    if let Ok(directory) = app.path().app_local_data_dir() {
        if std::fs::create_dir_all(&directory).is_ok() {
            let mut options = OpenOptions::new();
            options.create(true).truncate(true).write(true);
            #[cfg(unix)]
            { use std::os::unix::fs::OpenOptionsExt; options.mode(0o600); }
            if let Ok(file) = options.open(directory.join("mewdex-development-probe.jsonl")) {
                let _ = OUTPUT.set(Mutex::new(file));
            }
        }
    }
    emit("startup", json!({"platform": std::env::consts::OS, "mobile": cfg!(mobile), "pid": std::process::id()}));
    match crate::vault::probe_storage() {
        Ok(()) => emit("storage", json!({"roundTrip": true, "temporaryEntryRemoved": true})),
        Err(error) => emit("storage", json!({"roundTrip": false, "error": error})),
    }
}
