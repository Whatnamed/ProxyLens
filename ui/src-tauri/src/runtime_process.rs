use serde::{Deserialize, Serialize};
use std::env;
use std::path::Path;
use std::time::Duration;
use tauri::AppHandle;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

pub const RUNTIME_READY_TYPE: &str = "proxylens-runtime-ready";
pub const RUNTIME_ALREADY_RUNNING_TYPE: &str = "proxylens-runtime-already-running";
pub const RUNTIME_READY_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub enum RuntimeBootstrapState {
    NotAttempted,
    Started,
    AlreadyRunning,
    Failed,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeBootstrapStatus {
    pub state: RuntimeBootstrapState,
    pub pid: Option<u32>,
    pub error: Option<String>,
}

impl RuntimeBootstrapStatus {
    pub fn not_attempted() -> Self {
        Self {
            state: RuntimeBootstrapState::NotAttempted,
            pid: None,
            error: None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RuntimeSignal {
    Ready { runtime_version: String, pid: u32 },
    AlreadyRunning { runtime_version: String },
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct RuntimeSignalWire {
    #[serde(rename = "type")]
    signal_type: String,
    runtime_version: String,
    pid: Option<u32>,
}

// Only the two documented machine signals are parsed. All other logs and
// malformed lines are intentionally ignored without retaining their content.
pub fn parse_runtime_signal(line: &str) -> Option<RuntimeSignal> {
    let wire = serde_json::from_str::<RuntimeSignalWire>(line.trim()).ok()?;
    if wire.runtime_version.trim().is_empty() {
        return None;
    }
    match wire.signal_type.as_str() {
        RUNTIME_READY_TYPE => wire
            .pid
            .filter(|pid| *pid > 0)
            .map(|pid| RuntimeSignal::Ready {
                runtime_version: wire.runtime_version,
                pid,
            }),
        RUNTIME_ALREADY_RUNNING_TYPE => Some(RuntimeSignal::AlreadyRunning {
            runtime_version: wire.runtime_version,
        }),
        _ => None,
    }
}

pub struct RuntimeBootstrap {
    pub status: RuntimeBootstrapStatus,
    pub child: Option<CommandChild>,
}

pub async fn ensure_runtime(
    app_handle: &AppHandle,
    db_path: &Path,
) -> Result<RuntimeBootstrap, String> {
    let db_path_str = db_path.to_string_lossy().to_string();
    let sidecar_command = app_handle
        .shell()
        .sidecar("proxylens-runtime")
        .map_err(|e| format!("Failed to create Runtime sidecar command: {e}"))?;
    let mut args = vec!["--db".to_string(), db_path_str];
    if env::var("PROXYLENS_E2E_MODE").unwrap_or_default() == "1" {
        args.extend([
            "--connections-interval".to_string(),
            "50".to_string(),
            "--accounting-interval".to_string(),
            "50ms".to_string(),
            "--queue-capacity".to_string(),
            "8".to_string(),
        ]);
    }
    let (mut rx, child) = sidecar_command
        .args(&args)
        .spawn()
        .map_err(|e| format!("Failed to spawn Runtime sidecar: {e}"))?;
    let candidate_pid = child.pid();

    let signal_result = tokio::time::timeout(RUNTIME_READY_TIMEOUT, async {
        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(bytes) => {
                    let output = String::from_utf8_lossy(&bytes);
                    for line in output.lines() {
                        if let Some(signal) = parse_runtime_signal(line) {
                            return Ok(signal);
                        }
                    }
                }
                CommandEvent::Terminated(_) => {
                    return Err("Runtime terminated before emitting a readiness signal".to_string());
                }
                CommandEvent::Error(_) => {
                    return Err("Runtime process reported a startup error".to_string());
                }
                CommandEvent::Stderr(_) => {}
                _ => {}
            }
        }
        Err("Runtime output channel closed before emitting a readiness signal".to_string())
    })
    .await;

    match signal_result {
        Ok(Ok(RuntimeSignal::Ready { pid, .. })) if pid == candidate_pid => {
            let status = RuntimeBootstrapStatus {
                state: RuntimeBootstrapState::Started,
                pid: Some(candidate_pid),
                error: None,
            };
            tauri::async_runtime::spawn(drain_runtime_events(rx));
            Ok(RuntimeBootstrap {
                status,
                child: Some(child),
            })
        }
        Ok(Ok(RuntimeSignal::Ready { .. })) => {
            let _ = child.kill();
            Err("Runtime readiness signal PID did not match the spawned candidate".to_string())
        }
        Ok(Ok(RuntimeSignal::AlreadyRunning { .. })) => {
            let status = RuntimeBootstrapStatus {
                state: RuntimeBootstrapState::AlreadyRunning,
                pid: None,
                error: None,
            };
            // The duplicate candidate exits by itself. Drain its pipes without
            // storing it as the Runtime owned by this Tauri process.
            tauri::async_runtime::spawn(drain_runtime_events(rx));
            Ok(RuntimeBootstrap {
                status,
                child: None,
            })
        }
        Ok(Err(error)) => {
            let _ = child.kill();
            Err(error)
        }
        Err(_) => {
            // This is the exact candidate child created above. Existing
            // Runtime instances are never reachable through this handle.
            let _ = child.kill();
            Err("Timed out waiting 5s for Runtime readiness signal".to_string())
        }
    }
}

async fn drain_runtime_events(mut rx: tauri::async_runtime::Receiver<CommandEvent>) {
    while rx.recv().await.is_some() {}
}

#[cfg(test)]
mod tests {
    use super::{parse_runtime_signal, RuntimeSignal};

    #[test]
    fn parses_ready_signal() {
        let signal = parse_runtime_signal(
            r#"{"type":"proxylens-runtime-ready","runtimeVersion":"0.7.0-phase3e2a","pid":1234}"#,
        );
        assert_eq!(
            signal,
            Some(RuntimeSignal::Ready {
                runtime_version: "0.7.0-phase3e2a".to_string(),
                pid: 1234,
            })
        );
    }

    #[test]
    fn parses_already_running_signal_without_pid() {
        let signal = parse_runtime_signal(
            r#"{"type":"proxylens-runtime-already-running","runtimeVersion":"0.7.0-phase3e2a"}"#,
        );
        assert_eq!(
            signal,
            Some(RuntimeSignal::AlreadyRunning {
                runtime_version: "0.7.0-phase3e2a".to_string(),
            })
        );
    }

    #[test]
    fn ignores_logs_malformed_json_and_secret_like_payloads() {
        assert_eq!(parse_runtime_signal("[runtime] controller=mock"), None);
        assert_eq!(parse_runtime_signal("{not-json}"), None);
        assert_eq!(
            parse_runtime_signal(
                r#"{"type":"proxylens-runtime-ready","runtimeVersion":"v","pid":7,"secret":"must-not-be-read"}"#,
            ),
            None
        );
    }
}
