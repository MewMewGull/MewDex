fn main() {
    // Tauri's Android CLI replaces target RUSTFLAGS. Emit final-link flags here
    // so Android ELF alignment does not depend on the invoking shell.
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("android") {
        println!("cargo:rustc-link-arg=-Wl,-z,max-page-size=16384");
        println!("cargo:rustc-link-arg=-Wl,-z,common-page-size=16384");
    }
    tauri_build::build()
}
