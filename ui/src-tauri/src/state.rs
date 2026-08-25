use serde::{Deserialize, Serialize};
use std::process::Child;
use std::sync::Mutex;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct QueryApiSession {
    pub base_url: String,
    pub token: String,
    pub api_version: String,
}

pub struct AppState {
    pub session: Mutex<Option<QueryApiSession>>,
    pub child: Mutex<Option<Child>>,
}

impl AppState {
    pub fn new() -> Self {
        Self {
            session: Mutex::new(None),
            child: Mutex::new(None),
        }
    }
}
