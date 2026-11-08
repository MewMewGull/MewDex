use std::collections::HashMap;
use std::error::Error;
use std::fmt;
use std::io;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::{CommandExt, ExitStatusExt};
use std::path::PathBuf;
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use super::request::{Access, Network, Permissions, Request};

#[derive(Debug)]
pub(super) struct UnconfirmedCleanup(String);

impl fmt::Display for UnconfirmedCleanup {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "Linux sandbox cleanup is unconfirmed: {}",
            self.0
        )
    }
}

impl Error for UnconfirmedCleanup {}

pub(super) fn terminate_unconfirmed() -> ! {
    // A kernel signal exit is distinct from every ordinary payload exit code.
    // The Go executor keeps this task unknown until ownership is reconciled.
    unsafe { libc::kill(libc::getpid(), libc::SIGKILL) };
    std::process::abort();
}

pub(super) fn available() -> bool {
    let directory = std::path::PathBuf::from("/");
    let request = Request {
        protocol_version: super::PROTOCOL_VERSION,
        workspace: directory.clone(),
        cwd: directory,
        command: vec!["/bin/true".into()],
        permissions: Permissions {
            filesystem: HashMap::from([(":minimal".into(), Access::Read)]),
            network: Network { enabled: false },
        },
    };
    execute(request).is_ok_and(|code| code == 0)
}

pub(super) fn run() -> Result<i32, Box<dyn Error>> {
    execute(super::request::read()?)
}

fn execute(mut request: Request) -> Result<i32, Box<dyn Error>> {
    let launcher = prepare_launcher(&mut request.permissions)?;
    let permissions = super::request::profile(request.permissions)?;
    let environment = super::request::environment()?;
    // Every descendant stays owned even after double-fork or setsid. Only this
    // dedicated command host becomes a subreaper, never the long-lived worker.
    if unsafe { libc::prctl(libc::PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) } != 0 {
        return Err(io::Error::last_os_error().into());
    }
    let interrupted = Arc::new(AtomicBool::new(false));
    signal_hook::flag::register(libc::SIGTERM, interrupted.clone())?;
    signal_hook::flag::register(libc::SIGINT, interrupted.clone())?;
    let parent = unsafe { libc::getppid() };
    let mut command = Command::new(launcher.path().join("mewdex-sandbox"));
    command
        .arg("--sandbox-policy-cwd")
        .arg(request.workspace)
        .arg("--command-cwd")
        .arg(request.cwd.clone())
        .arg("--permission-profile")
        .arg(serde_json::to_string(&permissions)?)
        .arg("--")
        .args(request.command)
        .current_dir(request.cwd)
        .env_clear()
        .envs(environment)
        .stdin(Stdio::null())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit());
    let host = unsafe { libc::getpid() };
    unsafe {
        command.pre_exec(move || {
            // Cover an abrupt host death as well as cooperative cancellation.
            // Check again after prctl to close the parent-death setup race.
            if libc::prctl(libc::PR_SET_PDEATHSIG, libc::SIGKILL, 0, 0, 0) != 0 {
                return Err(io::Error::last_os_error());
            }
            if libc::getppid() != host {
                return Err(io::Error::from_raw_os_error(libc::EINTR));
            }
            Ok(())
        });
    }
    let mut child = command.spawn()?;
    let result = wait_for_payload(&mut child, &interrupted, parent);
    if result.is_err() {
        let _ = child.kill();
    }
    cleanup_descendants()?;
    let status = result.map_err(|error| UnconfirmedCleanup(error.to_string()))?;
    Ok(status
        .code()
        .unwrap_or_else(|| 128 + status.signal().unwrap_or(1)))
}

fn prepare_launcher(permissions: &mut Permissions) -> Result<tempfile::TempDir, Box<dyn Error>> {
    // Codex reexecutes its inner stage inside the filesystem namespace. A
    // private per-command copy lets the original installation remain denied.
    let candidates = [
        std::env::temp_dir(),
        PathBuf::from("/tmp"),
        PathBuf::from("/var/tmp"),
    ];
    let mut directory = None;
    for candidate in candidates {
        let Ok(parent) = std::fs::canonicalize(candidate) else {
            continue;
        };
        if permissions
            .filesystem
            .iter()
            .any(|(path, access)| *access == Access::Deny && parent.starts_with(path))
        {
            continue;
        }
        if let Ok(created) = tempfile::Builder::new()
            .prefix("mewdex-sandbox-")
            .tempdir_in(parent)
        {
            directory = Some(created);
            break;
        }
    }
    let directory =
        directory.ok_or("no public temporary location is available for the sandbox inner stage")?;
    let executable = std::env::current_exe()?;
    let binary = directory.path().join("mewdex-sandbox");
    std::fs::copy(&executable, &binary)?;
    std::fs::set_permissions(&binary, std::fs::Permissions::from_mode(0o555))?;
    let bubblewrap = executable
        .parent()
        .ok_or("sandbox executable has no parent")?
        .join("bwrap");
    if bubblewrap.exists() {
        let target = directory.path().join("bwrap");
        std::fs::copy(bubblewrap, &target)?;
        std::fs::set_permissions(target, std::fs::Permissions::from_mode(0o555))?;
    }
    permissions
        .filesystem
        .insert(binary.to_string_lossy().into_owned(), Access::Read);
    Ok(directory)
}

fn wait_for_payload(
    child: &mut Child,
    interrupted: &AtomicBool,
    parent: libc::pid_t,
) -> io::Result<ExitStatus> {
    let mut stopping = None;
    loop {
        if let Some(status) = child.try_wait()? {
            return Ok(status);
        }
        if interrupted.load(Ordering::Relaxed) || unsafe { libc::getppid() } != parent {
            if stopping.is_none() {
                // This PID cannot be reused while its unreaped Child is held.
                unsafe { libc::kill(child.id() as libc::pid_t, libc::SIGTERM) };
                stopping = Some(Instant::now());
            }
            if stopping.is_some_and(|started| started.elapsed() >= Duration::from_secs(2)) {
                child.kill()?;
            }
        }
        thread::sleep(Duration::from_millis(20));
    }
}

fn cleanup_descendants() -> Result<(), UnconfirmedCleanup> {
    let deadline = Instant::now() + Duration::from_secs(3);
    loop {
        loop {
            let mut status = 0;
            let reaped = unsafe { libc::waitpid(-1, &mut status, libc::WNOHANG) };
            if reaped > 0 {
                continue;
            }
            if reaped == -1 {
                let error = io::Error::last_os_error();
                if error.raw_os_error() == Some(libc::ECHILD) {
                    return Ok(());
                }
                if error.raw_os_error() == Some(libc::EINTR) {
                    continue;
                }
                return Err(UnconfirmedCleanup(error.to_string()));
            }
            break;
        }
        // Unreaped children retain their PIDs. No other thread reaps them, so
        // a PID read from this kernel-owned list cannot become a foreign PID
        // between this read and kill. A later pass reaps newly adopted orphans.
        let path = format!("/proc/self/task/{}/children", unsafe { libc::getpid() });
        let children =
            std::fs::read_to_string(path).map_err(|error| UnconfirmedCleanup(error.to_string()))?;
        for child in children.split_whitespace() {
            let pid = child
                .parse::<libc::pid_t>()
                .map_err(|error| UnconfirmedCleanup(error.to_string()))?;
            if pid <= 0 {
                return Err(UnconfirmedCleanup("invalid kernel child identity".into()));
            }
            unsafe { libc::kill(pid, libc::SIGKILL) };
        }
        if Instant::now() >= deadline {
            return Err(UnconfirmedCleanup(
                "owned descendants did not exit within the cleanup deadline".into(),
            ));
        }
        thread::sleep(Duration::from_millis(10));
    }
}
