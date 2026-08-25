use crate::state::{AppState, QueryApiSession};
use tauri::State;

#[tauri::command]
pub fn get_query_api_session(state: State<'_, AppState>) -> Result<QueryApiSession, String> {
    let session_guard = state.session.lock().unwrap();
    if let Some(session) = session_guard.as_ref() {
        return Ok(session.clone());
    }
    Err("Query API session not initialized. Make sure DB is configured and sidecar is running.".to_string())
}
