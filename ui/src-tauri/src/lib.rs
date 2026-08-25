pub mod commands;
pub mod sidecar;
pub mod state;

use state::AppState;
use tauri::{Manager, State};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app_state = AppState::new();

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(app_state)
        .invoke_handler(tauri::generate_handler![
            commands::get_query_api_session,
            commands::report_e2e_probe
        ])
        .setup(|app| {
            let handle = app.handle().clone();
            let db_res = sidecar::resolve_db_path();
            if let Ok(db_path) = db_res {
                println!("[ProxyLens Tauri] Resolved DB path: {:?}", db_path);
                let state: State<AppState> = app.state();
                match tauri::async_runtime::block_on(sidecar::spawn_query_sidecar(&handle, &db_path)) {
                    Ok((session, child)) => {
                        println!("[ProxyLens Tauri] Query sidecar ready at: {}", session.base_url);
                        *state.session.lock().unwrap() = Some(session);
                        *state.child.lock().unwrap() = Some(child);
                    }
                    Err(e) => {
                        eprintln!("[ProxyLens Tauri] Failed to start query sidecar: {}", e);
                    }
                }
            } else if let Err(e) = db_res {
                println!("[ProxyLens Tauri] Notice: {}", e);
            }
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
                    println!("[ProxyLens Tauri] Stopping query sidecar on window destroy...");
                    sidecar::stop_query_sidecar(child);
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running proxylens desktop application");
}
