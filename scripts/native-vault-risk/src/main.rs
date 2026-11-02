//! A synthetic Linux credential probe. Never reads the real connection entry.
#[cfg(all(target_os = "linux", debug_assertions))]
#[allow(dead_code)]
#[path = "../../../apps/client/src-tauri/src/credential_store.rs"]
mod credential_store;

#[cfg(all(target_os = "linux", debug_assertions))]
fn main() -> Result<(), Box<dyn std::error::Error>> {
    use keyring_core::{Entry, Error};
    let arguments: Vec<_> = std::env::args().skip(1).collect();
    if arguments.len() != 3 || arguments[0] != "--isolated-synthetic-entry" {
        return Err(
            "Use --isolated-synthetic-entry create|read|remove <32 lowercase hex ID>".into(),
        );
    }
    let id = &arguments[2];
    if id.len() != 32
        || !id
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
    {
        return Err("Invalid synthetic entry ID".into());
    }
    credential_store::probe_storage().map_err(|e| format!("Production platform adapter: {e}"))?;
    let entry = Entry::new(
        "app.mewdex.client.development-probe",
        &format!("persist-{id}"),
    )?;
    let expected = format!("mewdex-synthetic-{id}");
    match arguments[1].as_str() {
        "create" => {
            if !matches!(entry.get_password(), Err(Error::NoEntry)) {
                return Err("A synthetic entry already exists or could not be observed".into());
            }
            entry.set_password(&expected)?;
        }
        "read" => {
            if entry.get_password()? != expected {
                return Err("The synthetic entry changed across processes".into());
            }
        }
        "remove" => {
            if entry.get_password()? != expected {
                return Err("Refusing to remove an unconfirmed synthetic entry".into());
            }
            entry.delete_credential()?;
            if !matches!(entry.get_password(), Err(Error::NoEntry)) {
                return Err("Synthetic entry removal was not confirmed".into());
            }
        }
        _ => return Err("Unknown probe phase".into()),
    }
    println!(
        "{}",
        serde_json::json!({
            "platform": "linux", "phase": arguments[1], "state": "succeeded",
            "productionAdapter": true, "syntheticEntryOnly": true,
            "realConnectionRead": false, "modelCredentialsRead": false,
            "packagingValidation": false,
        })
    );
    Ok(())
}

#[cfg(not(all(target_os = "linux", debug_assertions)))]
fn main() {
    eprintln!("This synthetic probe requires a Linux debug target.");
    std::process::exit(2);
}
