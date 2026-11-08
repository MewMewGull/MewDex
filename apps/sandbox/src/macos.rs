use std::error::Error;
use std::io::{self, Read, Write};
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use std::thread;
use std::time::Duration;

use mxc_sdk::v1::policy::{
    FilesystemPolicy, NetworkAction, NetworkEgressPolicy, NetworkIngressPolicy, NetworkPolicy,
    UiPolicy,
};
use mxc_sdk::v1::{self, ContainerRequest, Containment, MxcProcess, WaitResult};

use super::request::Access;

#[derive(Debug)]
pub(super) struct UnconfirmedCleanup(String);

impl std::fmt::Display for UnconfirmedCleanup {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl Error for UnconfirmedCleanup {}

pub(super) fn terminate_unconfirmed() -> ! {
    // A signal exit is distinct from every payload exit code. The executor
    // treats an abnormal host exit as unconfirmed, including a forced stop.
    unsafe { libc::kill(libc::getpid(), libc::SIGKILL) };
    loop {
        thread::park();
    }
}

fn shell_argument(argument: &str) -> String {
    let escaped = argument.replace('\'', "'\\''");
    format!("'{escaped}'")
}

fn forward(
    mut source: Box<dyn Read + Send>,
    mut destination: impl Write,
    failed: Arc<AtomicBool>,
) -> io::Result<()> {
    let result = io::copy(&mut source, &mut destination).map(|_| ());
    if result.is_err() {
        failed.store(true, Ordering::SeqCst);
    }
    result
}

pub(super) fn run() -> Result<i32, Box<dyn Error>> {
    let stopped = Arc::new(AtomicBool::new(false));
    signal_hook::flag::register(signal_hook::consts::SIGTERM, Arc::clone(&stopped))?;
    signal_hook::flag::register(signal_hook::consts::SIGINT, Arc::clone(&stopped))?;
    // A lost executor must also release the sandbox it owned.
    let parent = unsafe { libc::getppid() };
    let request = super::request::read()?;
    let mut filesystem = FilesystemPolicy::default();
    for (path, access) in request.permissions.filesystem {
        if path == ":minimal" {
            continue;
        }
        match access {
            Access::Read => filesystem.readonly_paths.push(path),
            Access::Write => filesystem.readwrite_paths.push(path),
            Access::Deny => filesystem.denied_paths.push(path),
        }
    }
    let network = if request.permissions.network.enabled {
        NetworkAction::Allow
    } else {
        NetworkAction::Deny
    };
    let arguments: Vec<String> = request
        .command
        .iter()
        .map(|value| shell_argument(value))
        .collect();
    let command = "exec ".to_owned() + &arguments.join(" ");
    let invocation = ContainerRequest {
        containment: Containment::Seatbelt(Default::default()),
        working_directory: Some(
            request
                .cwd
                .to_str()
                .ok_or("invalid command directory")?
                .to_owned(),
        ),
        filesystem: Some(filesystem),
        network: Some(NetworkPolicy {
            egress: Some(NetworkEgressPolicy {
                default: Some(network.clone()),
                ..Default::default()
            }),
            ingress: Some(NetworkIngressPolicy {
                default: Some(network.clone()),
                host_loopback: Some(network),
            }),
            ..Default::default()
        }),
        environment: Some(super::request::environment()?.into_iter().collect()),
        inherit_default_environment: Some(false),
        ui: Some(UiPolicy::default()),
        ..ContainerRequest::new(command)
    };
    if stopped.load(Ordering::SeqCst) {
        return Err("sandbox launch canceled".into());
    }
    let process = v1::spawn(invocation, Default::default())
        .map_err(|error| UnconfirmedCleanup(error.to_string()))?;
    supervise(process, stopped, parent)
        .map_err(|error| Box::new(UnconfirmedCleanup(error.to_string())) as Box<dyn Error>)
}

fn supervise(
    mut process: MxcProcess,
    stopped: Arc<AtomicBool>,
    parent: libc::pid_t,
) -> Result<i32, Box<dyn Error>> {
    // Terminal jobs have no interactive input. Close the host's retained writer
    // so tools observe EOF instead of waiting on the request transport.
    drop(process.take_stdin());
    let stdout = process.take_stdout().ok_or("missing command stdout")?;
    let stderr = process.take_stderr().ok_or("missing command stderr")?;
    let failed = Arc::new(AtomicBool::new(false));
    let output_failed = Arc::clone(&failed);
    let stdout_reader = thread::spawn(move || forward(stdout, io::stdout().lock(), output_failed));
    let output_failed = Arc::clone(&failed);
    let stderr_reader = thread::spawn(move || forward(stderr, io::stderr().lock(), output_failed));
    while process.try_wait()?.is_none() {
        if stopped.load(Ordering::SeqCst)
            || failed.load(Ordering::SeqCst)
            || unsafe { libc::getppid() } != parent
        {
            process.kill()?;
            break;
        }
        thread::sleep(Duration::from_millis(10));
    }
    let outcome = process.wait()?;
    stdout_reader
        .join()
        .map_err(|_| "command stdout forwarding panicked")??;
    stderr_reader
        .join()
        .map_err(|_| "command stderr forwarding panicked")??;
    let warnings = process.warnings();
    if !warnings.is_empty() {
        return Err(format!("sandbox cleanup was not confirmed: {}", warnings.join("; ")).into());
    }
    match outcome {
        WaitResult::Exited(code) => Ok(if code >= 0 { code } else { 1 }),
        WaitResult::TimedOut => Err("sandbox command timed out".into()),
    }
}
