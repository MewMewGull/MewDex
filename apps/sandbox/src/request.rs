use std::collections::HashMap;
use std::error::Error;
use std::io::{self, Read};
#[cfg(any(windows, target_os = "linux"))]
use std::path::Path;

#[cfg(any(windows, target_os = "linux"))]
use codex_protocol::models::{ManagedFileSystemPermissions, PermissionProfile};
#[cfg(any(windows, target_os = "linux"))]
use codex_protocol::permissions::{
    FileSystemAccessMode, FileSystemPath, FileSystemSandboxEntry, FileSystemSpecialPath,
    NetworkSandboxPolicy,
};
#[cfg(any(windows, target_os = "linux"))]
use codex_utils_absolute_path::AbsolutePathBuf;
use serde::Deserialize;

const MAX_REQUEST_BYTES: u64 = 1 << 20;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub(super) struct Request {
    pub protocol_version: u32,
    pub workspace: std::path::PathBuf,
    pub cwd: std::path::PathBuf,
    pub command: Vec<String>,
    pub permissions: Permissions,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct Permissions {
    pub filesystem: HashMap<String, Access>,
    pub network: Network,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct Network {
    pub enabled: bool,
}

#[derive(Clone, Copy, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub(super) enum Access {
    Read,
    Write,
    #[serde(alias = "none")]
    Deny,
}

pub(super) fn environment() -> Result<HashMap<String, String>, Box<dyn Error>> {
    const ALLOWED: &[&str] = &[
        "PATH",
        "HOME",
        "USER",
        "LOGNAME",
        "SHELL",
        "TMP",
        "TEMP",
        "TMPDIR",
        "SYSTEMROOT",
        "SYSTEMDRIVE",
        "COMSPEC",
        "PATHEXT",
        "WINDIR",
        "PROGRAMFILES",
        "PROGRAMFILES(X86)",
        "PROGRAMW6432",
        "LOCALAPPDATA",
        "APPDATA",
        "USERPROFILE",
        "HOMEDRIVE",
        "HOMEPATH",
        "LANG",
        "TZ",
        "TERM",
    ];
    let mut environment = HashMap::new();
    for (key, value) in std::env::vars_os() {
        let key = key.into_string().map_err(|_| "invalid environment name")?;
        let upper = key.to_uppercase();
        if ALLOWED.contains(&upper.as_str()) || upper.starts_with("LC_") {
            let value = value
                .into_string()
                .map_err(|_| "invalid environment value")?;
            environment.insert(key, value);
        }
    }
    Ok(environment)
}

#[cfg(any(windows, target_os = "linux"))]
pub(super) fn profile(permissions: Permissions) -> Result<PermissionProfile, Box<dyn Error>> {
    let mut entries = Vec::new();
    for (path, access) in permissions.filesystem {
        let access = match access {
            Access::Read => FileSystemAccessMode::Read,
            Access::Write => FileSystemAccessMode::Write,
            Access::Deny => FileSystemAccessMode::Deny,
        };
        let path = if path == ":minimal" && access == FileSystemAccessMode::Read {
            FileSystemPath::Special {
                value: FileSystemSpecialPath::Minimal,
            }
        } else {
            if !Path::new(&path).is_absolute()
                || path.contains(['*', '?', '[', ']', '\0', '\r', '\n'])
            {
                return Err("execution permissions require absolute literal paths".into());
            }
            FileSystemPath::from(AbsolutePathBuf::from_absolute_path(path)?)
        };
        entries.push(FileSystemSandboxEntry::new(path, access));
    }
    Ok(PermissionProfile::Managed {
        file_system: ManagedFileSystemPermissions::Restricted {
            entries,
            glob_scan_max_depth: None,
        },
        network: if permissions.network.enabled {
            NetworkSandboxPolicy::Enabled
        } else {
            NetworkSandboxPolicy::Restricted
        },
    })
}

pub(super) fn read() -> Result<Request, Box<dyn Error>> {
    let mut input = Vec::new();
    io::stdin()
        .take(MAX_REQUEST_BYTES + 1)
        .read_to_end(&mut input)?;
    if input.len() as u64 > MAX_REQUEST_BYTES {
        return Err("sandbox request is too large".into());
    }
    let mut request: Request =
        serde_json::from_slice(&input).map_err(|_| "invalid sandbox request")?;
    if request.protocol_version != super::PROTOCOL_VERSION
        || request.command.is_empty()
        || request.command.len() > 256
        || request.command[0].is_empty()
        || request
            .command
            .iter()
            .any(|argument| argument.contains('\0'))
    {
        return Err("invalid sandbox protocol or command".into());
    }
    request.workspace = std::fs::canonicalize(request.workspace)?;
    request.cwd = std::fs::canonicalize(request.cwd)?;
    if !request.cwd.starts_with(&request.workspace) {
        return Err("command directory must remain in the execution project".into());
    }
    for (path, access) in &request.permissions.filesystem {
        if path == ":minimal" && *access == Access::Read {
            continue;
        }
        if !std::path::Path::new(path).is_absolute()
            || path.contains(['*', '?', '[', ']', '\0', '\r', '\n'])
        {
            return Err("execution permissions require absolute literal paths".into());
        }
    }
    Ok(request)
}
