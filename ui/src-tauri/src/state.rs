use crate::supervisor_process::{SupervisorBootstrapStatus, SupervisorChild};
use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use tauri_plugin_shell::process::CommandChild;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct QueryApiSession {
    pub base_url: String,
    pub token: String,
    pub api_version: String,
}

pub struct AppState {
    pub session: Mutex<Option<QueryApiSession>>,
    pub child: Mutex<Option<CommandChild>>,
    pub supervisor: Mutex<Option<SupervisorChild>>,
    pub supervisor_status: Mutex<SupervisorBootstrapStatus>,
}

impl AppState {
    pub fn new() -> Self {
        Self {
            session: Mutex::new(None),
            child: Mutex::new(None),
            supervisor: Mutex::new(None),
            supervisor_status: Mutex::new(SupervisorBootstrapStatus::not_attempted()),
        }
    }
}
