use crate::state::AppState;
use crate::supervisor_process::{
    configure_supervisor_process, resolve_supervisor_executable, SupervisorBootstrapState,
    SupervisorBootstrapStatus,
};
use serde::{Deserialize, Serialize};
use std::io::Write;
use std::process::{Command, Stdio};
use tauri::{AppHandle, State};

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeSettingsSnapshot {
    pub schema_version: i32,
    pub persisted_controller_url: String,
    pub effective_controller_url: String,
    pub controller_source: String,
    pub autostart_enabled: bool,
    pub installed_layout: bool,
    pub credential_stored: bool,
    pub effective_secret_present: bool,
    pub secret_source: String,
    pub owner_mode: String,
    pub task_registered: bool,
    pub task_enabled: bool,
    pub supervisor_running: bool,
    pub runtime_running: bool,
    pub bootstrap_state: String,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeSettingsApplyRequest {
    pub controller_url: String,
    pub autostart_enabled: bool,
    pub secret_action: String,
    #[serde(default)]
    pub secret: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeSettingsApplyOutcome {
    pub saved: bool,
    pub activation: String,
    pub restart_required: bool,
    pub message: Option<String>,
    pub settings: RuntimeSettingsSnapshot,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
#[allow(dead_code)]
struct ConfigStatusWire {
    schema_version: i32,
    #[serde(default, rename = "controllerConfigured")]
    _controller_configured: bool,
    #[serde(default, rename = "controllerUrl")]
    _controller_url: Option<String>,
    #[serde(default)]
    persisted_controller_url: String,
    effective_controller_url: String,
    controller_source: String,
    autostart_enabled: bool,
    #[serde(default)]
    credential_stored: bool,
    #[serde(default)]
    effective_secret_present: bool,
    #[serde(default, rename = "secretPresent")]
    _secret_present: bool,
    #[serde(default)]
    secret_source: String,
    #[serde(default)]
    installed_layout: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct InstalledOwnerStatusWire {
    #[serde(default, rename = "mode")]
    _mode: String,
    #[serde(default, rename = "taskName")]
    _task_name: Option<String>,
    #[serde(default, rename = "installedLayout")]
    _installed_layout: bool,
    #[serde(default)]
    task_registered: bool,
    #[serde(default)]
    task_enabled: bool,
    #[serde(default)]
    supervisor_running: bool,
    #[serde(default)]
    runtime_running: bool,
    #[serde(default, rename = "actionPath")]
    _action_path: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct ControlStatusWire {
    #[serde(default)]
    supervisor_running: bool,
    #[serde(default)]
    runtime_running: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
#[allow(dead_code)]
struct ConfigApplyWire {
    saved: bool,
    #[serde(default)]
    connection_changed: bool,
    #[serde(default)]
    controller_changed: bool,
    #[serde(default)]
    secret_changed: bool,
    #[serde(default)]
    autostart_changed: bool,
    restart_required: bool,
    #[serde(default)]
    autostart_enabled: bool,
    #[serde(default)]
    credential_stored: bool,
    #[serde(default)]
    rollback_incomplete: bool,
    #[serde(default)]
    error: Option<String>,
}

#[tauri::command]
pub fn get_runtime_settings(
    app_handle: AppHandle,
    state: State<'_, AppState>,
) -> Result<RuntimeSettingsSnapshot, String> {
    collect_snapshot(&app_handle, &state)
}

#[tauri::command]
pub async fn apply_runtime_settings(
    app_handle: AppHandle,
    state: State<'_, AppState>,
    request: RuntimeSettingsApplyRequest,
) -> Result<RuntimeSettingsApplyOutcome, String> {
    let payload =
        serde_json::to_vec(&request).map_err(|_| "Changes were not saved.".to_string())?;
    let (success, output) =
        run_supervisor_command(&app_handle, &["config", "apply"], Some(&payload))?;
    if !success {
        // The Go command emits only a safe result marker on persistence
        // failures. Do not surface child stderr/stdout or request contents.
        let _ = serde_json::from_slice::<ConfigApplyWire>(&output);
        return Err("Changes were not saved.".to_string());
    }
    let result: ConfigApplyWire =
        serde_json::from_slice(&output).map_err(|_| "Changes were not saved.".to_string())?;
    if !result.saved {
        return Err("Changes were not saved.".to_string());
    }

    if !result.restart_required {
        let settings = collect_snapshot(&app_handle, &state)?;
        return Ok(RuntimeSettingsApplyOutcome {
            saved: true,
            activation: "not-required".to_string(),
            restart_required: false,
            message: None,
            settings,
        });
    }

    let mut activated = false;
    if let Ok(db_path) = crate::sidecar::resolve_runtime_db_path() {
        let db_arg = db_path.to_string_lossy().into_owned();
        let stop_args = ["control", "stop", "--db", db_arg.as_str(), "--wait", "10s"];
        if let Ok((stop_ok, _)) = run_supervisor_command(&app_handle, &stop_args, None) {
            if stop_ok {
                if let Ok(mut supervisor) = state.supervisor.lock() {
                    supervisor.take();
                }
                if let Ok(bootstrap) = crate::bootstrap_supervisor(&app_handle, &db_path).await {
                    crate::store_supervisor_bootstrap(&state, bootstrap);
                    activated = true;
                }
            }
        }
    }

    let settings = collect_snapshot(&app_handle, &state)?;
    if activated {
        Ok(RuntimeSettingsApplyOutcome {
            saved: true,
            activation: "applied".to_string(),
            restart_required: true,
            message: None,
            settings,
        })
    } else {
        Ok(RuntimeSettingsApplyOutcome {
            saved: true,
            activation: "saved-pending-restart".to_string(),
            restart_required: true,
            message: Some("Changes were saved, but background collection could not restart. They will apply the next time the Runtime starts.".to_string()),
            settings,
        })
    }
}

fn collect_snapshot(
    app_handle: &AppHandle,
    state: &State<'_, AppState>,
) -> Result<RuntimeSettingsSnapshot, String> {
    let config: ConfigStatusWire = run_supervisor_json(app_handle, &["config", "status"])?;
    let db_path = crate::sidecar::resolve_runtime_db_path().ok();

    let installed = if config.installed_layout
        || std::env::var("PROXYLENS_E2E_TASK_NAME")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .is_some()
    {
        db_path.as_ref().and_then(|path| {
            let db_arg = path.to_string_lossy().into_owned();
            run_supervisor_json::<InstalledOwnerStatusWire>(
                app_handle,
                &["install", "status", "--db", db_arg.as_str()],
            )
            .ok()
        })
    } else {
        None
    };
    let control = db_path.as_ref().and_then(|path| {
        let db_arg = path.to_string_lossy().into_owned();
        run_supervisor_json::<ControlStatusWire>(
            app_handle,
            &["control", "status", "--db", db_arg.as_str()],
        )
        .ok()
    });

    let bootstrap = state.supervisor_status.lock().unwrap().clone();
    let supervisor_running = control
        .as_ref()
        .map(|value| value.supervisor_running)
        .unwrap_or(false)
        || installed
            .as_ref()
            .map(|value| value.supervisor_running)
            .unwrap_or(false)
        || matches!(
            &bootstrap.state,
            SupervisorBootstrapState::Started
                | SupervisorBootstrapState::Starting
                | SupervisorBootstrapState::AlreadyRunning
                | SupervisorBootstrapState::Installed
        );
    let runtime_running = control
        .as_ref()
        .map(|value| value.runtime_running)
        .unwrap_or(false)
        || installed
            .as_ref()
            .map(|value| value.runtime_running)
            .unwrap_or(false);
    let task_registered = installed
        .as_ref()
        .map(|value| value.task_registered)
        .unwrap_or(false);
    let task_enabled = installed
        .as_ref()
        .map(|value| value.task_enabled)
        .unwrap_or(false);
    let owner_mode = if task_registered && task_enabled && config.installed_layout {
        "installed-task"
    } else if supervisor_running {
        "direct-supervisor"
    } else {
        "unavailable"
    };

    Ok(RuntimeSettingsSnapshot {
        schema_version: config.schema_version,
        persisted_controller_url: config.persisted_controller_url,
        effective_controller_url: config.effective_controller_url,
        controller_source: config.controller_source,
        autostart_enabled: config.autostart_enabled,
        installed_layout: config.installed_layout,
        credential_stored: config.credential_stored,
        effective_secret_present: config.effective_secret_present,
        secret_source: config.secret_source,
        owner_mode: owner_mode.to_string(),
        task_registered,
        task_enabled,
        supervisor_running,
        runtime_running,
        bootstrap_state: bootstrap_state_name(&bootstrap),
    })
}

fn bootstrap_state_name(status: &SupervisorBootstrapStatus) -> String {
    match status.state {
        SupervisorBootstrapState::NotAttempted => "not-attempted",
        SupervisorBootstrapState::Started => "started",
        SupervisorBootstrapState::Starting => "starting",
        SupervisorBootstrapState::AlreadyRunning => "already-running",
        SupervisorBootstrapState::Installed => "installed",
        SupervisorBootstrapState::Failed => "failed",
    }
    .to_string()
}

fn run_supervisor_json<T: for<'de> Deserialize<'de>>(
    app_handle: &AppHandle,
    args: &[&str],
) -> Result<T, String> {
    let (success, output) = run_supervisor_command(app_handle, args, None)?;
    if !success {
        return Err("Supervisor command failed".to_string());
    }
    serde_json::from_slice(&output)
        .map_err(|_| "Supervisor command returned invalid status".to_string())
}

fn run_supervisor_command(
    app_handle: &AppHandle,
    args: &[&str],
    input: Option<&[u8]>,
) -> Result<(bool, Vec<u8>), String> {
    let executable = resolve_supervisor_executable(app_handle)?;
    let mut command = Command::new(executable);
    command
        .args(args)
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    if input.is_some() {
        command.stdin(Stdio::piped());
    } else {
        command.stdin(Stdio::null());
    }
    configure_supervisor_process(&mut command);
    let mut child = command
        .spawn()
        .map_err(|_| "Failed to invoke the installed Supervisor command".to_string())?;
    if let Some(payload) = input {
        if let Some(mut stdin) = child.stdin.take() {
            if stdin.write_all(payload).is_err() {
                let _ = child.kill();
                let _ = child.wait();
                return Err("Failed to send runtime settings request".to_string());
            }
        }
    }
    let output = child
        .wait_with_output()
        .map_err(|_| "Failed to observe the Supervisor command".to_string())?;
    Ok((output.status.success(), output.stdout))
}

#[cfg(test)]
mod tests {
    use super::{bootstrap_state_name, ConfigStatusWire};
    use crate::supervisor_process::{SupervisorBootstrapState, SupervisorBootstrapStatus};

    #[test]
    fn parses_safe_config_status_with_sources() {
        let value: ConfigStatusWire = serde_json::from_str(
            r#"{"schemaVersion":2,"persistedControllerUrl":"http://127.0.0.1:43127","effectiveControllerUrl":"http://127.0.0.1:43128","controllerSource":"PROXYLENS_CONTROLLER_URL","autostartEnabled":true,"credentialStored":true,"effectiveSecretPresent":true,"secretSource":"CREDENTIAL_MANAGER","installedLayout":true}"#,
        )
        .expect("safe config status should parse");
        assert_eq!(value.controller_source, "PROXYLENS_CONTROLLER_URL");
        assert!(value.credential_stored && value.effective_secret_present);
        assert!(value.installed_layout);
    }

    #[test]
    fn rejects_unknown_config_status_fields() {
        let result = serde_json::from_str::<ConfigStatusWire>(
            r#"{"schemaVersion":2,"effectiveControllerUrl":"http://127.0.0.1:43127","controllerSource":"PERSISTED","autostartEnabled":true,"secret":"do-not-accept"}"#,
        );
        assert!(result.is_err());
    }

    #[test]
    fn names_bootstrap_states_without_process_details() {
        let status = SupervisorBootstrapStatus {
            state: SupervisorBootstrapState::Starting,
            pid: Some(1234),
            error: None,
            runtime_state: Some("starting-retrying".to_string()),
            runtime_pid: None,
        };
        assert_eq!(bootstrap_state_name(&status), "starting");
    }
}
