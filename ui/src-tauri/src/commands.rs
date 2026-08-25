use crate::state::{AppState, QueryApiSession};
use serde::Deserialize;
use std::env;
use tauri::State;

#[tauri::command]
pub fn get_query_api_session(state: State<'_, AppState>) -> Result<QueryApiSession, String> {
    let session_guard = state.session.lock().unwrap();
    if let Some(session) = session_guard.as_ref() {
        return Ok(session.clone());
    }
    Err("Query API session not initialized. Make sure DB is configured and sidecar is running.".to_string())
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct E2EProbeReport {
    pub meta_ok: bool,
    pub summary_ok: bool,
    pub connections_ok: bool,
    pub error: Option<String>,
}

#[tauri::command]
pub fn report_e2e_probe(report: E2EProbeReport) {
    if env::var("PROXYLENS_E2E_MODE").unwrap_or_default() == "1" {
        let meta_int = if report.meta_ok { 1 } else { 0 };
        let sum_int = if report.summary_ok { 1 } else { 0 };
        let conns_int = if report.connections_ok { 1 } else { 0 };
        if let Some(err) = report.error {
            eprintln!("[ProxyLens E2E Probe Error] {}", err);
        }
        // 打印纯安全无 Token 标记
        println!("PROXYLENS_WEBVIEW_E2E_READY meta={} summary={} connections={}", meta_int, sum_int, conns_int);
    }
}

