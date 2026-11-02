//! Explicit, bounded Windows integration probe. Only fresh synthetic identities
//! are registered. It sends no toast and reads no input or model credentials.
use crate::{
    notification_registration_windows::Registration,
    notification_routes::{
        tickets::{self, Store},
        Route,
    },
};
use serde_json::{json, Value};
use std::{
    collections::BTreeMap,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
use windows::{
    core::{HSTRING, PCWSTR, PWSTR},
    Win32::{
        Foundation::{CloseHandle, WAIT_OBJECT_0},
        System::Threading::{
            GetExitCodeProcess, QueryFullProcessImageNameW, TerminateProcess, WaitForSingleObject,
            PROCESS_NAME_FORMAT,
        },
        UI::{
            Shell::{
                ShellExecuteExW, SEE_MASK_NOASYNC, SEE_MASK_NOCLOSEPROCESS, SHELLEXECUTEINFOW,
            },
            WindowsAndMessaging::SW_HIDE,
        },
    },
};
use winreg::{enums::HKEY_CURRENT_USER, RegKey};

const SCHEME_PREFIX: &str = "mewdex-notification-check-";
fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
}
fn base() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .join(".state/desktop-notifications-20261029/native-risk")
}
fn hex_valid(value: &str) -> bool {
    value.len() == 32
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn save(path: &Path, value: &Value) -> Result<(), String> {
    let mut file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(path)
        .map_err(|_| "probe record already exists or cannot be saved")?;
    file.write_all(serde_json::to_string_pretty(value).unwrap().as_bytes())
        .and_then(|_| file.sync_all())
        .map_err(|_| "probe record could not be saved".into())
}

fn tree(key: &RegKey, depth: usize) -> Result<Value, String> {
    if depth > 4 {
        return Err("unexpected registration depth".into());
    }
    let values = key
        .enum_values()
        .take(33)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| "cannot inspect synthetic registration")?;
    let children = key
        .enum_keys()
        .take(9)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| "cannot inspect synthetic registration")?;
    if values.len() > 32 || children.len() > 8 {
        return Err("synthetic registration changed outside the probe".into());
    }
    let values: BTreeMap<_, _> = values
        .into_iter()
        .map(|(name, value)| {
            (
                name,
                json!({"type":format!("{:?}",value.vtype),"bytes":value.bytes}),
            )
        })
        .collect();
    let mut descendants = BTreeMap::new();
    for name in children {
        descendants.insert(
            name.clone(),
            tree(
                &key.open_subkey(&name)
                    .map_err(|_| "cannot inspect synthetic registration")?,
                depth + 1,
            )?,
        );
    }
    Ok(json!({"values":values,"children":descendants}))
}

fn snapshot(paths: &[String; 2], shortcut: &Path) -> Result<Value, String> {
    let user = RegKey::predef(HKEY_CURRENT_USER);
    let mut entries = BTreeMap::new();
    for path in paths {
        entries.insert(
            path.clone(),
            tree(
                &user
                    .open_subkey(path)
                    .map_err(|_| "missing synthetic registration")?,
                0,
            )?,
        );
    }
    let info = fs::symlink_metadata(shortcut).map_err(|_| "missing synthetic shortcut")?;
    if !info.file_type().is_file() || info.len() > 1 << 20 {
        return Err("synthetic shortcut changed".into());
    }
    Ok(
        json!({"keys":entries,"shortcut":fs::read(shortcut).map_err(|_| "cannot inspect synthetic shortcut")?}),
    )
}

fn clean(paths: &[String; 2], shortcut: &Path, expected: &Value) -> Result<(), String> {
    if &snapshot(paths, shortcut)? != expected {
        return Err("synthetic registration changed; retained for review".into());
    }
    if !paths[0].starts_with("Software\\Classes\\AppUserModelId\\app.mewdex.notificationcheck.")
        || !paths[1].starts_with(&format!("Software\\Classes\\{SCHEME_PREFIX}"))
    {
        return Err("refusing cleanup outside synthetic registration".into());
    }
    fs::remove_file(shortcut).map_err(|_| "cannot remove the unchanged synthetic shortcut")?;
    let user = RegKey::predef(HKEY_CURRENT_USER);
    for path in paths {
        user.delete_subkey_all(path)
            .map_err(|_| "cannot remove the unchanged synthetic registration")?;
    }
    for path in paths {
        match user.open_subkey(path) {
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
            _ => return Err("synthetic registration cleanup is unconfirmed".into()),
        }
    }
    if shortcut.exists() {
        return Err("synthetic shortcut cleanup is unconfirmed".into());
    }
    Ok(())
}

fn activate(uri: &str) -> Result<(), String> {
    let url = url::Url::parse(uri).map_err(|_| "invalid synthetic link")?;
    let nonce = url
        .scheme()
        .strip_prefix(SCHEME_PREFIX)
        .ok_or("invalid synthetic scheme")?;
    if !hex_valid(nonce) {
        return Err("invalid synthetic identity".into());
    }
    let directory = base().join(nonce);
    let boundary = fs::canonicalize(base()).map_err(|_| "missing probe boundary")?;
    if fs::canonicalize(&directory)
        .map_err(|_| "missing probe directory")?
        .parent()
        != Some(boundary.as_path())
    {
        return Err("probe directory escaped its boundary".into());
    }
    let token = tickets::parse_activation(url.scheme(), uri).ok_or("invalid synthetic route")?;
    let ticket = Store::open(directory.join("routes"))?.resolve(now(), &token)?;
    if ticket.route
        != (Route {
            scope: "synthetic notification scope".into(),
            device_id: "synthetic executor".into(),
            session_id: "synthetic conversation".into(),
        })
    {
        return Err("unexpected synthetic navigation".into());
    }
    save(
        &directory.join("activation.json"),
        &json!({"state":"resolved","newProcess":true,"routeMatched":true,"modelCalls":0,"token":token}),
    )
}

fn launch(uri: &str) -> Result<(), String> {
    let uri = HSTRING::from(uri);
    let mut operation = SHELLEXECUTEINFOW {
        cbSize: std::mem::size_of::<SHELLEXECUTEINFOW>() as u32,
        fMask: SEE_MASK_NOCLOSEPROCESS | SEE_MASK_NOASYNC,
        lpFile: PCWSTR(uri.as_ptr()),
        nShow: SW_HIDE.0,
        ..Default::default()
    };
    unsafe { ShellExecuteExW(&mut operation) }
        .map_err(|_| "Windows did not accept the synthetic protocol activation")?;
    if operation.hProcess.is_invalid() {
        return Err("Windows did not return the synthetic activation process handle".into());
    }
    let mut image = vec![0u16; 32768];
    let mut length = image.len() as u32;
    let identified = unsafe {
        QueryFullProcessImageNameW(
            operation.hProcess,
            PROCESS_NAME_FORMAT(0),
            PWSTR(image.as_mut_ptr()),
            &mut length,
        )
    }
    .ok()
    .and_then(|_| String::from_utf16(&image[..length as usize]).ok())
    .and_then(|path| fs::canonicalize(path).ok());
    let expected = std::env::current_exe()
        .ok()
        .and_then(|path| fs::canonicalize(path).ok());
    if identified.is_none() || identified != expected {
        let _ = unsafe { CloseHandle(operation.hProcess) };
        return Err("the protocol activation process was not the exact owned probe image".into());
    }
    let wait = unsafe { WaitForSingleObject(operation.hProcess, 10000) };
    if wait != WAIT_OBJECT_0 {
        let _ = unsafe { TerminateProcess(operation.hProcess, 91) };
        let _ = unsafe { WaitForSingleObject(operation.hProcess, 5000) };
        let _ = unsafe { CloseHandle(operation.hProcess) };
        return Err("synthetic activation did not complete within its deadline".into());
    }
    let mut exit = 0;
    let result = unsafe { GetExitCodeProcess(operation.hProcess, &mut exit) };
    let _ = unsafe { CloseHandle(operation.hProcess) };
    result.map_err(|_| "synthetic activation outcome is unconfirmed")?;
    if exit != 0 {
        return Err("synthetic activation process reported failure".into());
    }
    Ok(())
}

/// Run a fresh probe or resolve its OS-launched opaque activation link.
pub fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if let [uri] = args.as_slice() {
        return activate(uri);
    }
    if !args.is_empty() {
        return Err("unsupported notification probe arguments".into());
    }
    let mut random = [0u8; 16];
    getrandom::fill(&mut random).map_err(|_| "no random synthetic identity")?;
    let nonce: String = random.iter().map(|v| format!("{v:02x}")).collect();
    fs::create_dir_all(base()).map_err(|_| "cannot create probe evidence boundary")?;
    let directory = base().join(&nonce);
    fs::create_dir(&directory).map_err(|_| "probe directory already exists")?;
    let app_id = format!("app.mewdex.notificationcheck.{nonce}");
    let scheme = format!("{SCHEME_PREFIX}{nonce}");
    let registration = Registration::new(
        app_id.clone(),
        scheme.clone(),
        format!("MewDex notification check {nonce}"),
        std::env::current_exe().map_err(|_| "missing probe program")?,
    )?;
    let (shortcut, paths) = registration.risk_paths();
    save(
        &directory.join("intent.json"),
        &json!({"appId":app_id,"scheme":scheme,"shortcut":shortcut,"keys":paths,"realModel":false,"actualToast":false}),
    )?;
    let user = RegKey::predef(HKEY_CURRENT_USER);
    for path in &paths {
        match user.open_subkey(path) {
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
            _ => return Err("synthetic identity was not freshly absent".into()),
        }
    }
    if fs::symlink_metadata(&shortcut).is_ok() {
        return Err("synthetic shortcut was not freshly absent".into());
    }
    let mut captured = None;
    let result = (|| {
        if registration.ready()? {
            return Err("fresh synthetic identity was already enabled".into());
        }
        registration.install()?;
        captured = Some(snapshot(&paths, &shortcut)?);
        save(
            &directory.join("registered-snapshot.json"),
            captured.as_ref().unwrap(),
        )?;
        let before = captured.as_ref().unwrap().clone();
        registration.install()?;
        captured = Some(snapshot(&paths, &shortcut)?);
        save(
            &directory.join("repeated-snapshot.json"),
            captured.as_ref().unwrap(),
        )?;
        let repeated = before["keys"] == captured.as_ref().unwrap()["keys"];
        if !repeated || !registration.ready()? {
            return Err("repeated registration did not retain its association".into());
        }
        let route = Route {
            scope: "synthetic notification scope".into(),
            device_id: "synthetic executor".into(),
            session_id: "synthetic conversation".into(),
        };
        let (ticket, _) = Store::open(directory.join("routes"))?.create(now(), 7, route)?;
        launch(&tickets::activation_url(&scheme, &ticket.token)?)?;
        let activation: Value = serde_json::from_slice(
            &fs::read(directory.join("activation.json"))
                .map_err(|_| "synthetic activation receipt is missing")?,
        )
        .map_err(|_| "synthetic activation receipt is invalid")?;
        if activation["state"] != "resolved" || activation["token"] != ticket.token {
            return Err("synthetic activation receipt did not match its link".into());
        }
        let (command, _) = user
            .create_subkey(format!("{}\\shell\\open\\command", paths[1]))
            .map_err(|_| "cannot prepare synthetic association conflict")?;
        let original: String = command
            .get_value("")
            .map_err(|_| "cannot read synthetic association")?;
        command
            .set_value("", &"synthetic foreign association - do not replace")
            .map_err(|_| "cannot prepare synthetic association conflict")?;
        if registration.install().is_ok()
            || command.get_value::<String, _>("").ok().as_deref()
                != Some("synthetic foreign association - do not replace")
        {
            return Err("changed association was overwritten".into());
        }
        command
            .set_value("", &original)
            .map_err(|_| "cannot restore the synthetic conflict fixture")?;
        save(
            &directory.join("checks.json"),
            &json!({"registered":true,"readyReadback":true,"repeatedRegistration":true,"crossProcessActivation":true,"changedAssociationPreserved":true,"realModel":false,"actualToast":false}),
        )?;
        Ok::<(), String>(())
    })();
    let cleanup = captured
        .as_ref()
        .map(|expected| clean(&paths, &shortcut, expected));
    let cleaned = matches!(cleanup, Some(Ok(())));
    save(
        &directory.join("report.json"),
        &json!({"state":if result.is_ok()&&cleaned {"passed"} else {"incomplete"},"operation":result.as_ref().err(),"cleanupConfirmed":cleaned,"cleanupError":cleanup.as_ref().and_then(|value|value.as_ref().err()),"evidenceDirectory":directory,"realModel":false,"actualToast":false}),
    )?;
    result?;
    if !cleaned {
        return Err("synthetic registration cleanup remains unconfirmed".into());
    }
    Ok(())
}
