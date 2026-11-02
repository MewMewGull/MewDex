#![cfg_attr(windows, windows_subsystem = "windows")]

fn main() {
    #[cfg(all(windows, debug_assertions))]
    if let Err(error) = mewdex_client_lib::notification_risk::run() {
        eprintln!("{error}");
        std::process::exit(1);
    }
    #[cfg(not(all(windows, debug_assertions)))]
    std::process::exit(2);
}
