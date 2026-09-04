use crate::runtime_process::RuntimeBootstrapStatus;
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
    pub runtime: Mutex<Option<CommandChild>>,
    pub runtime_status: Mutex<RuntimeBootstrapStatus>,
}

impl AppState {
    pub fn new() -> Self {
        Self {
            session: Mutex::new(None),
            child: Mutex::new(None),
            runtime: Mutex::new(None),
            runtime_status: Mutex::new(RuntimeBootstrapStatus::not_attempted()),
        }
    }
}
