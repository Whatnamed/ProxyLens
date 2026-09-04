use serde::{Deserialize, Serialize};
use std::env;
use std::io::{BufRead, BufReader, Read};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{self, Receiver, TryRecvError};
use std::thread;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Manager};

pub const SUPERVISOR_READY_TYPE: &str = "proxylens-supervisor-ready";
pub const SUPERVISOR_ALREADY_RUNNING_TYPE: &str = "proxylens-supervisor-already-running";
pub const SUPERVISOR_READY_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub enum SupervisorBootstrapState {
    NotAttempted,
    Started,
    AlreadyRunning,
    Failed,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct SupervisorBootstrapStatus {
    pub state: SupervisorBootstrapState,
    pub pid: Option<u32>,
    pub error: Option<String>,
    pub runtime_state: Option<String>,
    pub runtime_pid: Option<u32>,
}

impl SupervisorBootstrapStatus {
    pub fn not_attempted() -> Self {
        Self {
            state: SupervisorBootstrapState::NotAttempted,
            pid: None,
            error: None,
            runtime_state: None,
            runtime_pid: None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SupervisorSignal {
    Ready {
        supervisor_version: String,
        pid: u32,
        runtime_state: String,
        runtime_pid: Option<u32>,
    },
    AlreadyRunning {
        supervisor_version: String,
    },
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct SupervisorSignalWire {
    #[serde(rename = "type")]
    signal_type: String,
    supervisor_version: String,
    pid: Option<u32>,
    runtime_state: Option<String>,
    runtime_pid: Option<u32>,
}

pub fn parse_supervisor_signal(line: &str) -> Option<SupervisorSignal> {
    let wire = serde_json::from_str::<SupervisorSignalWire>(line.trim()).ok()?;
    if wire.supervisor_version.trim().is_empty() {
        return None;
    }
    match wire.signal_type.as_str() {
        SUPERVISOR_READY_TYPE => {
            let pid = wire.pid.filter(|pid| *pid > 0)?;
            let runtime_state = wire.runtime_state?;
            if runtime_state == "started" {
                let runtime_pid = wire.runtime_pid.filter(|pid| *pid > 0)?;
                Some(SupervisorSignal::Ready {
                    supervisor_version: wire.supervisor_version,
                    pid,
                    runtime_state,
                    runtime_pid: Some(runtime_pid),
                })
            } else if runtime_state == "already-running" && wire.runtime_pid.is_none() {
                Some(SupervisorSignal::Ready {
                    supervisor_version: wire.supervisor_version,
                    pid,
                    runtime_state,
                    runtime_pid: None,
                })
            } else {
                None
            }
        }
        SUPERVISOR_ALREADY_RUNNING_TYPE
            if wire.pid.is_none() && wire.runtime_state.is_none() && wire.runtime_pid.is_none() =>
        {
            Some(SupervisorSignal::AlreadyRunning {
                supervisor_version: wire.supervisor_version,
            })
        }
        _ => None,
    }
}

// This handle deliberately contains a std::process::Child rather than a
// tauri-plugin-shell CommandChild. The shell plugin kills every registered
// child on RunEvent::Exit; Supervisor must survive Tauri UI exit. Dropping a
// standard Child does not terminate its process.
pub struct SupervisorChild {
    child: Child,
}

impl SupervisorChild {
    pub fn pid(&self) -> u32 {
        self.child.id()
    }
}

pub struct SupervisorBootstrap {
    pub status: SupervisorBootstrapStatus,
    pub child: Option<SupervisorChild>,
}

pub async fn ensure_supervisor(
    app_handle: &AppHandle,
    db_path: &Path,
) -> Result<SupervisorBootstrap, String> {
    let executable = resolve_supervisor_executable(app_handle)?;
    let mut command = Command::new(&executable);
    command
        .arg("--db")
        .arg(db_path)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    configure_supervisor_process(&mut command);
    let mut child = command
        .spawn()
        .map_err(|error| format!("Failed to spawn Supervisor sidecar: {error}"))?;
    let candidate_pid = child.id();
    let (line_tx, line_rx) = mpsc::channel();
    if let Some(stdout) = child.stdout.take() {
        spawn_output_reader(stdout, line_tx.clone());
    }
    if let Some(stderr) = child.stderr.take() {
        spawn_output_reader(stderr, line_tx.clone());
    }
    drop(line_tx);

    let deadline = Instant::now() + SUPERVISOR_READY_TIMEOUT;
    loop {
        match receive_supervisor_signal(&line_rx) {
            Some(SupervisorSignal::Ready {
                pid,
                runtime_state,
                runtime_pid,
                ..
            }) if pid == candidate_pid => {
                return Ok(SupervisorBootstrap {
                    status: SupervisorBootstrapStatus {
                        state: SupervisorBootstrapState::Started,
                        pid: Some(candidate_pid),
                        error: None,
                        runtime_state: Some(runtime_state),
                        runtime_pid,
                    },
                    child: Some(SupervisorChild { child }),
                });
            }
            Some(SupervisorSignal::Ready { .. }) => {
                terminate_candidate(&mut child);
                return Err(
                    "Supervisor readiness signal PID did not match the spawned candidate"
                        .to_string(),
                );
            }
            Some(SupervisorSignal::AlreadyRunning { .. }) => {
                wait_duplicate_candidate(child);
                return Ok(SupervisorBootstrap {
                    status: SupervisorBootstrapStatus {
                        state: SupervisorBootstrapState::AlreadyRunning,
                        pid: None,
                        error: None,
                        runtime_state: None,
                        runtime_pid: None,
                    },
                    child: None,
                });
            }
            None => {}
        }

        match child.try_wait() {
            Ok(Some(_)) => {
                return Err("Supervisor terminated before emitting a readiness signal".to_string());
            }
            Ok(None) => {}
            Err(error) => {
                terminate_candidate(&mut child);
                return Err(format!("Failed to observe Supervisor candidate: {error}"));
            }
        }
        if Instant::now() >= deadline {
            terminate_candidate(&mut child);
            return Err("Timed out waiting 5s for Supervisor readiness signal".to_string());
        }
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
}

fn resolve_supervisor_executable(app_handle: &AppHandle) -> Result<PathBuf, String> {
    let file_name = if cfg!(windows) {
        "proxylens-supervisor.exe"
    } else {
        "proxylens-supervisor"
    };
    let mut candidates = Vec::new();
    if let Ok(current_exe) = env::current_exe() {
        if let Some(parent) = current_exe.parent() {
            candidates.push(parent.join(file_name));
        }
    }
    if let Ok(resource_dir) = app_handle.path().resource_dir() {
        candidates.push(resource_dir.join(file_name));
    }
    for candidate in candidates {
        if is_regular_file(&candidate) {
            return Ok(candidate);
        }
    }
    Err(format!(
        "Supervisor executable is not available beside the desktop binary or in its resource directory: {file_name}"
    ))
}

fn is_regular_file(path: &Path) -> bool {
    path.metadata()
        .map(|metadata| metadata.is_file())
        .unwrap_or(false)
}

#[cfg(windows)]
fn configure_supervisor_process(command: &mut Command) {
    use std::os::windows::process::CommandExt;
    const CREATE_NEW_PROCESS_GROUP: u32 = 0x0000_0200;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    command.creation_flags(CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
fn configure_supervisor_process(_command: &mut Command) {}

fn spawn_output_reader<R>(reader: R, sender: mpsc::Sender<String>)
where
    R: Read + Send + 'static,
{
    thread::spawn(move || {
        for line in BufReader::new(reader).lines().flatten() {
            if sender.send(line).is_err() {
                break;
            }
        }
    });
}

fn receive_supervisor_signal(receiver: &Receiver<String>) -> Option<SupervisorSignal> {
    loop {
        match receiver.try_recv() {
            Ok(line) => {
                if let Some(signal) = parse_supervisor_signal(&line) {
                    return Some(signal);
                }
            }
            Err(TryRecvError::Empty) | Err(TryRecvError::Disconnected) => return None,
        }
    }
}

fn terminate_candidate(child: &mut Child) {
    let _ = child.kill();
    let _ = child.wait();
}

fn wait_duplicate_candidate(mut child: Child) {
    thread::spawn(move || {
        let _ = child.wait();
    });
}

#[cfg(test)]
mod tests {
    use super::{parse_supervisor_signal, SupervisorSignal};

    #[test]
    fn parses_started_ready_signal() {
        let signal = parse_supervisor_signal(
            r#"{"type":"proxylens-supervisor-ready","supervisorVersion":"v1","pid":1234,"runtimeState":"started","runtimePid":5678}"#,
        );
        assert_eq!(
            signal,
            Some(SupervisorSignal::Ready {
                supervisor_version: "v1".to_string(),
                pid: 1234,
                runtime_state: "started".to_string(),
                runtime_pid: Some(5678),
            })
        );
    }

    #[test]
    fn parses_existing_runtime_ready_signal() {
        let signal = parse_supervisor_signal(
            r#"{"type":"proxylens-supervisor-ready","supervisorVersion":"v1","pid":1234,"runtimeState":"already-running"}"#,
        );
        assert_eq!(
            signal,
            Some(SupervisorSignal::Ready {
                supervisor_version: "v1".to_string(),
                pid: 1234,
                runtime_state: "already-running".to_string(),
                runtime_pid: None,
            })
        );
    }

    #[test]
    fn parses_duplicate_signal_without_pid() {
        assert_eq!(
            parse_supervisor_signal(
                r#"{"type":"proxylens-supervisor-already-running","supervisorVersion":"v1"}"#,
            ),
            Some(SupervisorSignal::AlreadyRunning {
                supervisor_version: "v1".to_string(),
            })
        );
    }

    #[test]
    fn rejects_invalid_pid_malformed_and_sensitive_payloads() {
        for line in [
            r#"{"type":"proxylens-supervisor-ready","supervisorVersion":"v1","pid":0,"runtimeState":"started","runtimePid":7}"#,
            r#"{"type":"proxylens-supervisor-ready","supervisorVersion":"v1","pid":7,"runtimeState":"started"}"#,
            r#"{"type":"proxylens-supervisor-ready","supervisorVersion":"v1","pid":7,"runtimeState":"already-running","runtimePid":8}"#,
            r#"{"type":"proxylensupervisor-ready","supervisorVersion":"v1","pid":7,"runtimeState":"started","runtimePid":8,"secret":"x"}"#,
            "not-json",
        ] {
            assert_eq!(
                parse_supervisor_signal(line),
                None,
                "line should be rejected: {line}"
            );
        }
    }
}
