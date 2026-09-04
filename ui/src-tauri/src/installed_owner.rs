use crate::supervisor_process::{
    configure_supervisor_process, resolve_supervisor_executable, SupervisorBootstrapState,
    SupervisorBootstrapStatus, SupervisorChild,
};
use serde::Deserialize;
use std::env;
use std::path::Path;
use std::process::{Command, Stdio};
use tauri::AppHandle;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct InstalledOwnerWire {
    mode: String,
    task_registered: bool,
    task_enabled: bool,
    supervisor_running: bool,
    #[serde(default, rename = "runtimeRunning")]
    _runtime_running: bool,
    #[serde(default, rename = "taskName")]
    _task_name: Option<String>,
    #[serde(default, rename = "actionPath")]
    _action_path: Option<String>,
}

pub fn ensure_installed_owner(
    app_handle: &AppHandle,
    db_path: &Path,
) -> Result<Option<(SupervisorBootstrapStatus, Option<SupervisorChild>)>, String> {
    let status_args = vec![
        "install".to_string(),
        "status".to_string(),
        "--db".to_string(),
        path_arg(db_path),
    ];
    let status = match lifecycle_command(app_handle, &status_args) {
        Ok(status) => status,
        Err(_error) if direct_fallback_is_allowed() => return Ok(None),
        Err(error) => return Err(error),
    };
    if status.mode != "installed-task" || !status.task_registered || !status.task_enabled {
        return Ok(None);
    }

    let ensure_args = vec![
        "install".to_string(),
        "ensure-owner".to_string(),
        "--db".to_string(),
        path_arg(db_path),
    ];
    let ensured = lifecycle_command(app_handle, &ensure_args)?;
    if ensured.mode != "installed-task" || !ensured.task_registered || !ensured.task_enabled {
        return Err("Installed Supervisor owner did not remain registered and enabled".to_string());
    }
    if !ensured.supervisor_running {
        return Err(
            "Installed Supervisor owner did not acquire the authority DB ownership".to_string(),
        );
    }
    Ok(Some((
        SupervisorBootstrapStatus {
            state: SupervisorBootstrapState::Installed,
            pid: None,
            error: None,
            runtime_state: Some("installed-task".to_string()),
            runtime_pid: None,
        },
        None,
    )))
}

fn direct_fallback_is_allowed() -> bool {
    if !cfg!(windows) {
        return true;
    }
    // Existing developer/E2E smoke tests intentionally exercise the direct
    // Supervisor path without an installed-task identity. Never use this
    // fallback for an installed E2E owner, where a lifecycle failure must
    // preserve the read-only Query fallback instead of bypassing ownership.
    env::var("PROXYLENS_E2E_MODE").unwrap_or_default().trim() == "1"
        && env::var("PROXYLENS_E2E_TASK_NAME")
            .unwrap_or_default()
            .trim()
            .is_empty()
}

fn path_arg(path: &Path) -> String {
    path.to_string_lossy().into_owned()
}

fn lifecycle_command(
    app_handle: &AppHandle,
    args: &[String],
) -> Result<InstalledOwnerWire, String> {
    let executable = resolve_supervisor_executable(app_handle)?;
    let mut command = Command::new(executable);
    command
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    configure_supervisor_process(&mut command);
    let output = command
        .output()
        .map_err(|_| "Failed to invoke the installed Supervisor lifecycle command".to_string())?;
    if !output.status.success() {
        return Err("Installed Supervisor lifecycle command failed".to_string());
    }
    serde_json::from_slice::<InstalledOwnerWire>(&output.stdout)
        .map_err(|_| "Installed Supervisor lifecycle command returned invalid status".to_string())
}

#[cfg(test)]
mod tests {
    use super::InstalledOwnerWire;

    #[test]
    fn parses_only_safe_installed_owner_status() {
        let status: InstalledOwnerWire = serde_json::from_str(
            r#"{"mode":"installed-task","taskName":"\\ProxyLens-Test\\00000000-0000-4000-8000-000000000000","taskRegistered":true,"taskEnabled":true,"supervisorRunning":true}"#,
        )
        .expect("status should parse");
        assert!(status.task_registered && status.task_enabled && status.supervisor_running);
        assert_eq!(status.mode, "installed-task");
        assert!(status._action_path.is_none());
    }
}
