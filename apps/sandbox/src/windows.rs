use std::error::Error;
use std::process::{Command, Stdio};

use codex_mxc_sandbox::CreateMxcCommandArgsParams;

pub fn run() -> Result<i32, Box<dyn Error>> {
    if !super::available() {
        return Err("native Windows process isolation is unavailable".into());
    }
    let request = super::request::read()?;
    let permissions = super::request::profile(request.permissions)?;
    let mut environment = super::request::environment()?;
    let arguments = codex_mxc_sandbox::create_command_args(CreateMxcCommandArgsParams {
        command: request.command,
        permission_profile: &permissions,
        sandbox_policy_cwd: &request.workspace,
        managed_network: None,
        env: &mut environment,
    })?;
    let status = Command::new(std::env::current_exe()?)
        .args(arguments)
        .current_dir(request.cwd)
        .env_clear()
        .envs(environment)
        .stdin(Stdio::null())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .status()?;
    Ok(status.code().unwrap_or(1))
}
