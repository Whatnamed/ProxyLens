pub mod commands;
pub mod installed_owner;
pub mod runtime_settings;
pub mod sidecar;
pub mod state;
pub mod supervisor_process;

use state::AppState;
use std::env;
use std::path::Path;
use std::time::Duration;
use tauri::{Manager, State};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app_state = AppState::new();

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(app_state)
        .invoke_handler(tauri::generate_handler![
            commands::get_query_api_session,
            commands::get_supervisor_bootstrap_status,
            commands::get_e2e_mode,
            commands::get_settings_e2e_mode,
            commands::report_e2e_probe,
            commands::report_settings_e2e_probe,
            runtime_settings::get_runtime_settings,
            runtime_settings::apply_runtime_settings
        ])
        .setup(|app| {
            let handle = app.handle().clone();
            let state: State<AppState> = app.state();

            // An installed current-user Task Scheduler task is the preferred
            // owner. Developer checkouts without that exact owner retain the
            // direct Supervisor bootstrap below. Query remains a separate,
            // UI-owned, read-only sidecar.
            match sidecar::resolve_runtime_db_path() {
                Ok(runtime_db_path) => {
                    let bootstrap = tauri::async_runtime::block_on(bootstrap_supervisor(
                        &handle,
                        &runtime_db_path,
                    ));
                    match bootstrap {
                        Ok(bootstrap) => {
                            eprintln!(
                                "PROXYLENS_SUPERVISOR_BOOTSTRAP {}",
                                serde_json::to_string(&bootstrap.status)
                                    .unwrap_or_else(|_| "{}".to_string())
                            );
                            store_supervisor_bootstrap(&state, bootstrap);
                        }
                        Err(error) => {
                            let status = supervisor_process::SupervisorBootstrapStatus {
                                state: supervisor_process::SupervisorBootstrapState::Failed,
                                pid: None,
                                error: Some(error),
                                runtime_state: None,
                                runtime_pid: None,
                            };
                            eprintln!(
                                "PROXYLENS_SUPERVISOR_BOOTSTRAP {}",
                                serde_json::to_string(&status).unwrap_or_else(|_| "{}".to_string())
                            );
                            *state.supervisor_status.lock().unwrap() = status;
                        }
                    }
                }
                Err(error) => {
                    let status = supervisor_process::SupervisorBootstrapStatus {
                        state: supervisor_process::SupervisorBootstrapState::Failed,
                        pid: None,
                        error: Some(error),
                        runtime_state: None,
                        runtime_pid: None,
                    };
                    eprintln!(
                        "PROXYLENS_SUPERVISOR_BOOTSTRAP {}",
                        serde_json::to_string(&status).unwrap_or_else(|_| "{}".to_string())
                    );
                    *state.supervisor_status.lock().unwrap() = status;
                }
            }

            let db_res = if supervisor_bootstrap_succeeded(&state) {
                tauri::async_runtime::block_on(sidecar::wait_for_query_db_path(
                    Duration::from_secs(2),
                ))
            } else {
                sidecar::resolve_query_db_path()
            };
            if let Ok(db_path) = db_res {
                eprintln!("[ProxyLens Tauri] Resolved Query DB path: {:?}", db_path);
                match tauri::async_runtime::block_on(sidecar::spawn_query_sidecar(
                    &handle, &db_path,
                )) {
                    Ok((session, child)) => {
                        eprintln!(
                            "[ProxyLens Tauri] Query sidecar ready at: {}",
                            session.base_url
                        );
                        *state.session.lock().unwrap() = Some(session);
                        *state.child.lock().unwrap() = Some(child);
                    }
                    Err(e) => {
                        eprintln!("[ProxyLens Tauri] Failed to start query sidecar: {}", e);
                    }
                }
            } else if let Err(e) = db_res {
                eprintln!("[ProxyLens Tauri] Notice: {}", e);
            }

            schedule_e2e_auto_exit(&handle);
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Destroyed = event {
                // 当主窗口被销毁时优雅停止 sidecar
                let state: State<AppState> = window.state();
                let child_opt = {
                    if let Ok(mut child_guard) = state.child.lock() {
                        child_guard.take()
                    } else {
                        None
                    }
                };
                if let Some(child) = child_opt {
                    eprintln!("[ProxyLens Tauri] Stopping query sidecar on window destroy...");
                    sidecar::stop_query_sidecar(child);
                }
                // Supervisor and its Runtime child intentionally outlive this
                // UI window. They are held in independent AppState state and
                // are never stopped by normal UI-close handling.
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running proxylens desktop application");
}

pub(crate) async fn bootstrap_supervisor(
    app_handle: &tauri::AppHandle,
    db_path: &Path,
) -> Result<supervisor_process::SupervisorBootstrap, String> {
    match installed_owner::ensure_installed_owner(app_handle, db_path)? {
        Some((status, child)) => Ok(supervisor_process::SupervisorBootstrap { status, child }),
        None => supervisor_process::ensure_supervisor(app_handle, db_path).await,
    }
}

pub(crate) fn store_supervisor_bootstrap(
    state: &State<'_, AppState>,
    bootstrap: supervisor_process::SupervisorBootstrap,
) {
    *state.supervisor_status.lock().unwrap() = bootstrap.status;
    *state.supervisor.lock().unwrap() = bootstrap.child;
}

fn schedule_e2e_auto_exit(app_handle: &tauri::AppHandle) {
    if env::var("PROXYLENS_E2E_MODE").unwrap_or_default() != "1" {
        return;
    }
    let Ok(delay_ms) = env::var("PROXYLENS_E2E_AUTO_EXIT_MS")
        .ok()
        .and_then(|value| value.parse::<u64>().ok())
        .ok_or(())
    else {
        return;
    };
    if delay_ms == 0 {
        return;
    }

    let handle = app_handle.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(Duration::from_millis(delay_ms)).await;
        if let Some(window) = handle.get_webview_window("main") {
            let _ = window.close();
        }
    });
}

fn supervisor_bootstrap_succeeded(state: &State<AppState>) -> bool {
    matches!(
        state.supervisor_status.lock().unwrap().state.clone(),
        supervisor_process::SupervisorBootstrapState::Started
            | supervisor_process::SupervisorBootstrapState::Starting
            | supervisor_process::SupervisorBootstrapState::AlreadyRunning
            | supervisor_process::SupervisorBootstrapState::Installed
    )
}
