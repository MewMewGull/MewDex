#[cfg(target_os = "linux")]
mod linux;
#[cfg(target_os = "macos")]
mod macos;
#[cfg(any(windows, target_os = "macos", target_os = "linux"))]
mod request;
#[cfg(windows)]
mod windows;

const PROTOCOL_VERSION: u32 = 1;

fn main() {
    let arguments: Vec<String> = std::env::args().skip(1).collect();
    #[cfg(target_os = "linux")]
    {
        codex_process_hardening::pre_main_hardening();
        // Codex's inner stage selects its dispatcher with argv[0]. Older
        // system Bubblewrap versions retain this executable's absolute name.
        let inner = std::env::args_os().next().is_some_and(|name| {
            std::path::Path::new(&name).file_name()
                == Some(std::ffi::OsStr::new("codex-linux-sandbox"))
        }) || arguments
            .iter()
            .take_while(|argument| argument.as_str() != "--")
            .any(|argument| argument == "--sandbox-policy-cwd");
        if inner {
            codex_linux_sandbox::run_main();
        }
    }
    if arguments.as_slice() == ["--version"] {
        println!("mewdex-sandbox {}", env!("CARGO_PKG_VERSION"));
        return;
    }
    #[cfg(windows)]
    if arguments.as_slice() == [codex_mxc_sandbox::CODEX_WINDOWS_MXC_ARG1] {
        codex_mxc_sandbox::run_main();
    }
    if arguments.as_slice() == ["--probe"] {
        println!(
            "{}",
            serde_json::json!({
                "protocolVersion": PROTOCOL_VERSION,
                "backend": if cfg!(windows) { "windows-mxc" } else if cfg!(target_os = "linux") { "linux-codex-bwrap" } else { "macos-seatbelt-mxc" },
                "available": available(),
            })
        );
        return;
    }
    if !arguments.is_empty() {
        eprintln!("unsupported sandbox operation");
        std::process::exit(1);
    }
    #[cfg(windows)]
    match windows::run() {
        Ok(code) => std::process::exit(code),
        Err(error) => {
            eprintln!("sandbox launch failed: {error}");
            std::process::exit(1);
        }
    }
    #[cfg(target_os = "macos")]
    match std::panic::catch_unwind(macos::run) {
        Ok(Ok(code)) => std::process::exit(code),
        Ok(Err(error)) => {
            eprintln!("sandbox launch failed: {error}");
            if error.downcast_ref::<macos::UnconfirmedCleanup>().is_some() {
                macos::terminate_unconfirmed();
            }
            std::process::exit(1);
        }
        Err(_) => {
            eprintln!("sandbox host panicked before process cleanup was confirmed");
            macos::terminate_unconfirmed();
        }
    }
    #[cfg(target_os = "linux")]
    match std::panic::catch_unwind(linux::run) {
        Ok(Ok(code)) => std::process::exit(code),
        Ok(Err(error)) => {
            eprintln!("sandbox launch failed: {error}");
            if error.downcast_ref::<linux::UnconfirmedCleanup>().is_some() {
                linux::terminate_unconfirmed();
            }
            std::process::exit(1);
        }
        Err(_) => {
            eprintln!("sandbox host panicked before process cleanup was confirmed");
            linux::terminate_unconfirmed();
        }
    }
    #[cfg(not(any(windows, target_os = "macos", target_os = "linux")))]
    {
        eprintln!("this platform has no native sandbox backend");
        std::process::exit(1);
    }
}

fn available() -> bool {
    #[cfg(windows)]
    {
        codex_mxc_sandbox::is_available()
    }
    #[cfg(target_os = "macos")]
    {
        mxc_sdk::v1::available_backends()
            .iter()
            .any(|backend| backend.backend == "seatbelt")
    }
    #[cfg(target_os = "linux")]
    {
        linux::available()
    }
    #[cfg(not(any(windows, target_os = "macos", target_os = "linux")))]
    {
        false
    }
}
