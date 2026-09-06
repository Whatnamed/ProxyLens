use crate::state::QueryApiSession;
use rand::RngCore;
use serde::Deserialize;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use tauri::AppHandle;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ReadySignal {
    #[serde(rename = "type")]
    signal_type: String,
    api_version: String,
    host: String,
    port: u16,
}

pub fn generate_high_entropy_token() -> String {
    let mut bytes = [0u8; 32]; // 256-bit entropy
    rand::thread_rng().fill_bytes(&mut bytes);
    let mut s = String::with_capacity(64);
    for b in bytes {
        use std::fmt::Write;
        let _ = write!(s, "{:02x}", b);
    }
    s
}

pub fn resolve_runtime_db_path() -> Result<PathBuf, String> {
    resolve_db_path_from_values(
        env_value("PROXYLENS_DB_PATH"),
        env_value("PROXYLENS_DATA_DIR"),
        env_value("LOCALAPPDATA"),
        false,
        |path| path.is_file(),
    )
}

/// Visual acceptance is deliberately query-only. A missing Visual QA flag is
/// not a request and therefore preserves the existing product/E2E behavior;
/// once the flag is present, however, an incomplete gate must be refused
/// rather than falling through to owner/Supervisor/Runtime bootstrap.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VisualQaGateState {
    Disabled,
    Active,
    RefusedIncomplete,
}

pub fn visual_qa_gate_from_values(
    e2e_mode: Option<&str>,
    visual_qa_flag: Option<&str>,
) -> VisualQaGateState {
    if visual_qa_flag.is_none() {
        return VisualQaGateState::Disabled;
    }
    if e2e_mode == Some("1") && visual_qa_flag == Some("1") {
        VisualQaGateState::Active
    } else {
        VisualQaGateState::RefusedIncomplete
    }
}

pub fn visual_qa_gate() -> VisualQaGateState {
    visual_qa_gate_from_values(
        env::var("PROXYLENS_E2E_MODE").ok().as_deref(),
        env::var("PROXYLENS_VISUAL_QA_QUERY_ONLY").ok().as_deref(),
    )
}

pub fn visual_qa_query_only_enabled() -> bool {
    visual_qa_gate() == VisualQaGateState::Active
}

pub fn settings_lifecycle_allowed_for_gate(gate: VisualQaGateState) -> bool {
    gate == VisualQaGateState::Disabled
}

/// Reject inherited production authority before opening the visual fixture.
pub fn validate_visual_qa_environment() -> Result<(), String> {
    validate_visual_qa_authority_values(
        env_value("PROXYLENS_CONTROLLER_URL").as_deref(),
        env_value("MIHOMO_SECRET").as_deref(),
    )
}

pub fn validate_visual_qa_authority_values(
    controller_url: Option<&str>,
    mihomo_secret: Option<&str>,
) -> Result<(), String> {
    if controller_url.is_some() {
        return Err(
            "VISUAL_QA_NOT_SAFE: PROXYLENS_CONTROLLER_URL must be unset in query-only visual QA mode"
                .to_string(),
        );
    }
    if mihomo_secret.is_some() {
        return Err(
            "VISUAL_QA_NOT_SAFE: MIHOMO_SECRET must be unset in query-only visual QA mode"
                .to_string(),
        );
    }
    Ok(())
}

pub fn resolve_visual_qa_db_path() -> Result<PathBuf, String> {
    resolve_visual_qa_db_path_from_values(
        env_value("PROXYLENS_DB_PATH"),
        env_value("LOCALAPPDATA"),
        |path| path.is_file(),
    )
}

fn resolve_visual_qa_db_path_from_values(
    explicit_db_path: Option<String>,
    local_app_data: Option<String>,
    path_exists: impl Fn(&Path) -> bool,
) -> Result<PathBuf, String> {
    let Some(explicit_db_path) = explicit_db_path else {
        return Err(
            "VISUAL_QA_NOT_SAFE: PROXYLENS_DB_PATH must explicitly point to an existing fixture DB"
                .to_string(),
        );
    };
    let path = PathBuf::from(&explicit_db_path);
    if !path.is_absolute() {
        return Err(
            "VISUAL_QA_NOT_SAFE: PROXYLENS_DB_PATH must be an absolute fixture path".to_string(),
        );
    }
    if !path_exists(&path) {
        return Err(format!(
            "DB_NOT_READY: visual QA fixture does not exist at '{}'",
            explicit_db_path
        ));
    }

    if let Some(local_app_data) = local_app_data {
        let canonical = PathBuf::from(local_app_data)
            .join("ProxyLens")
            .join("data")
            .join("proxylens.db");
        if paths_equal_for_safety(&path, &canonical) {
            return Err(
                "VISUAL_QA_NOT_SAFE: canonical production database is not allowed in query-only visual QA mode"
                    .to_string(),
            );
        }
    }

    Ok(path)
}

fn paths_equal_for_safety(left: &Path, right: &Path) -> bool {
    let left = fs::canonicalize(left).unwrap_or_else(|_| left.to_path_buf());
    let right = fs::canonicalize(right).unwrap_or_else(|_| right.to_path_buf());
    left.to_string_lossy()
        .replace('\\', "/")
        .to_ascii_lowercase()
        == right
            .to_string_lossy()
            .replace('\\', "/")
            .to_ascii_lowercase()
}

pub fn resolve_query_db_path() -> Result<PathBuf, String> {
    resolve_db_path_from_values(
        env_value("PROXYLENS_DB_PATH"),
        env_value("PROXYLENS_DATA_DIR"),
        env_value("LOCALAPPDATA"),
        true,
        |path| path.is_file(),
    )
}

pub fn resolve_db_path() -> Result<PathBuf, String> {
    resolve_query_db_path()
}

pub async fn wait_for_query_db_path(timeout: Duration) -> Result<PathBuf, String> {
    let deadline = Instant::now() + timeout;
    loop {
        match resolve_query_db_path() {
            Ok(path) => return Ok(path),
            Err(error) => {
                if Instant::now() >= deadline {
                    return Err(error);
                }
            }
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
}

fn env_value(name: &str) -> Option<String> {
    env::var(name).ok().filter(|value| !value.trim().is_empty())
}

fn resolve_db_path_from_values(
    explicit_db_path: Option<String>,
    data_dir: Option<String>,
    local_app_data: Option<String>,
    require_existing: bool,
    path_exists: impl Fn(&Path) -> bool,
) -> Result<PathBuf, String> {
    if let Some(env_path) = explicit_db_path {
        let path = PathBuf::from(&env_path);
        if !require_existing || path_exists(&path) {
            return Ok(path);
        }
        return Err(format!(
            "DB_NOT_READY: PROXYLENS_DB_PATH is set to '{}' but the file does not exist",
            env_path
        ));
    }

    if let Some(data_dir) = data_dir {
        let path = PathBuf::from(&data_dir).join("proxylens.db");
        if !require_existing || path_exists(&path) {
            return Ok(path);
        }
        return Err(format!(
            "DB_NOT_READY: PROXYLENS_DATA_DIR resolves to '{}' but the database file does not exist",
            path.display()
        ));
    }

    let Some(local_app_data) = local_app_data else {
        return Err(
            "DB_NOT_READY: LOCALAPPDATA is not set; cannot resolve the canonical ProxyLens database path"
                .to_string(),
        );
    };
    let path = PathBuf::from(local_app_data)
        .join("ProxyLens")
        .join("data")
        .join("proxylens.db");
    if !require_existing || path_exists(&path) {
        return Ok(path);
    }
    Err(format!(
        "DB_NOT_READY: canonical database is not initialized at '{}'",
        path.display()
    ))
}

pub async fn spawn_query_sidecar(
    app_handle: &AppHandle,
    db_path: &Path,
) -> Result<(QueryApiSession, CommandChild), String> {
    let token = generate_high_entropy_token();
    let db_path_str = db_path.to_string_lossy().to_string();

    // 使用 Tauri 官方 shell extension sidecar resolver 寻找与启动目标架构二进制
    let sidecar_command = app_handle
        .shell()
        .sidecar("proxylens-query-api")
        .map_err(|e| format!("Failed to create sidecar command: {}", e))?;

    let (mut rx, mut child) = sidecar_command
        .args(["--db", &db_path_str, "--listen", "127.0.0.1:0"])
        .spawn()
        .map_err(|e| format!("Failed to spawn query API sidecar: {}", e))?;

    // 安全通道: 通过 anonymous stdin pipe 发送 token，避免通过 argv 暴露给系统进程列表
    let token_payload = format!("{}\n", token);
    child
        .write(token_payload.as_bytes())
        .map_err(|e| format!("Failed to write session token to sidecar stdin: {}", e))?;

    // 真正非阻塞的异步 5 秒就绪超时检测
    let ready_res = tokio::time::timeout(Duration::from_secs(5), async {
        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(line_bytes) => {
                    let line = String::from_utf8_lossy(&line_bytes);
                    for sub in line.lines() {
                        let trimmed = sub.trim();
                        if trimmed.starts_with('{') && trimmed.contains("proxylens-query-api-ready")
                        {
                            if let Ok(sig) = serde_json::from_str::<ReadySignal>(trimmed) {
                                if sig.signal_type == "proxylens-query-api-ready" {
                                    return Ok(sig);
                                }
                            }
                        }
                    }
                }
                CommandEvent::Stderr(err_bytes) => {
                    let err_str = String::from_utf8_lossy(&err_bytes);
                    eprintln!("[ProxyLens Sidecar Stderr] {}", err_str);
                }
                CommandEvent::Error(err) => {
                    return Err(format!("Sidecar error event: {}", err));
                }
                CommandEvent::Terminated(term) => {
                    return Err(format!(
                        "Sidecar terminated prematurely with code {:?}",
                        term.code
                    ));
                }
                _ => {}
            }
        }
        Err("Sidecar stdout channel closed without outputting ready signal".to_string())
    })
    .await;

    match ready_res {
        Ok(Ok(sig)) => {
            let session = QueryApiSession {
                base_url: format!("http://{}:{}", sig.host, sig.port),
                token,
                api_version: sig.api_version,
            };
            Ok((session, child))
        }
        Ok(Err(e)) => {
            let _ = child.kill();
            Err(e)
        }
        Err(_) => {
            let _ = child.kill();
            Err("Timed out waiting 5s for proxylens-query-api ready signal".to_string())
        }
    }
}

pub fn stop_query_sidecar(mut child: CommandChild) {
    // 优雅停止: 先尝试发送 STOP\n，随后调用 kill
    let _ = child.write(b"STOP\n");
    let _ = child.kill();
}

#[cfg(test)]
mod tests {
    use super::{
        resolve_db_path_from_values, resolve_visual_qa_db_path_from_values,
        settings_lifecycle_allowed_for_gate, validate_visual_qa_authority_values,
        visual_qa_gate_from_values, VisualQaGateState,
    };
    use std::path::{Path, PathBuf};

    #[test]
    fn explicit_path_has_highest_precedence() {
        let resolved = resolve_db_path_from_values(
            Some(r"C:\fixture\explicit.db".to_string()),
            Some(r"C:\fixture\data".to_string()),
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            true,
            |_| true,
        )
        .expect("explicit path should resolve");
        assert_eq!(resolved, PathBuf::from(r"C:\fixture\explicit.db"));
    }

    #[test]
    fn data_dir_is_second_precedence() {
        let resolved = resolve_db_path_from_values(
            None,
            Some(r"C:\fixture\data".to_string()),
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            true,
            |_| true,
        )
        .expect("data dir path should resolve");
        assert_eq!(resolved, PathBuf::from(r"C:\fixture\data\proxylens.db"));
    }

    #[test]
    fn default_path_is_local_app_data_proxy_lens_data() {
        let resolved = resolve_db_path_from_values(
            None,
            None,
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            true,
            |_| true,
        )
        .expect("default path should resolve");
        assert_eq!(
            resolved,
            PathBuf::from(r"C:\Users\tester\AppData\Local\ProxyLens\data\proxylens.db")
        );
    }

    #[test]
    fn missing_database_is_db_not_ready_and_does_not_create_anything() {
        let resolved = resolve_db_path_from_values(
            None,
            None,
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            true,
            |_path: &Path| false,
        );
        let error = resolved.expect_err("missing database should be deferred to runtime");
        assert!(
            error.starts_with("DB_NOT_READY:"),
            "unexpected error: {error}"
        );
    }

    #[test]
    fn explicit_missing_database_names_the_override() {
        let error = resolve_db_path_from_values(
            Some(r"C:\fixture\missing.db".to_string()),
            None,
            None,
            true,
            |_path: &Path| false,
        )
        .expect_err("missing explicit database should fail closed");
        assert!(
            error.contains("PROXYLENS_DB_PATH"),
            "unexpected error: {error}"
        );
    }

    #[test]
    fn data_dir_missing_database_is_db_not_ready() {
        let error = resolve_db_path_from_values(
            None,
            Some(r"C:\fixture\data".to_string()),
            None,
            true,
            |_path: &Path| false,
        )
        .expect_err("missing data-dir database should fail closed");
        assert!(
            error.contains("PROXYLENS_DATA_DIR"),
            "unexpected error: {error}"
        );
    }

    #[test]
    fn writer_resolution_allows_missing_database_without_creating_it() {
        let resolved = resolve_db_path_from_values(
            Some(r"C:\fixture\missing.db".to_string()),
            None,
            None,
            false,
            |_path: &Path| false,
        )
        .expect("writer resolution should allow the first-run database");
        assert_eq!(resolved, PathBuf::from(r"C:\fixture\missing.db"));
    }

    #[test]
    fn visual_qa_requires_explicit_existing_absolute_noncanonical_fixture() {
        let resolved = resolve_visual_qa_db_path_from_values(
            Some(r"C:\qa\fixture.db".to_string()),
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            |_| true,
        )
        .expect("isolated fixture should resolve");
        assert_eq!(resolved, PathBuf::from(r"C:\qa\fixture.db"));

        assert!(resolve_visual_qa_db_path_from_values(
            None,
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            |_| true,
        )
        .is_err());
        assert!(resolve_visual_qa_db_path_from_values(
            Some(r"fixtures\fixture.db".to_string()),
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            |_| true,
        )
        .is_err());
    }

    #[test]
    fn visual_qa_rejects_canonical_production_database() {
        let canonical = r"C:\Users\tester\AppData\Local\ProxyLens\data\proxylens.db";
        let error = resolve_visual_qa_db_path_from_values(
            Some(canonical.to_string()),
            Some(r"C:\Users\tester\AppData\Local".to_string()),
            |_| true,
        )
        .expect_err("production DB must be rejected");
        assert!(error.contains("canonical production database"));
    }

    #[test]
    fn visual_qa_gate_preserves_normal_and_existing_e2e_modes() {
        assert_eq!(
            visual_qa_gate_from_values(None, None),
            VisualQaGateState::Disabled
        );
        assert_eq!(
            visual_qa_gate_from_values(Some("1"), None),
            VisualQaGateState::Disabled
        );
        assert_eq!(
            visual_qa_gate_from_values(Some("1"), Some("1")),
            VisualQaGateState::Active
        );
    }

    #[test]
    fn incomplete_visual_qa_requests_are_refused_without_falling_through() {
        assert_eq!(
            visual_qa_gate_from_values(None, Some("1")),
            VisualQaGateState::RefusedIncomplete
        );
        assert_eq!(
            visual_qa_gate_from_values(Some("1"), Some("0")),
            VisualQaGateState::RefusedIncomplete
        );
        assert!(!settings_lifecycle_allowed_for_gate(
            VisualQaGateState::Active
        ));
        assert!(!settings_lifecycle_allowed_for_gate(
            VisualQaGateState::RefusedIncomplete
        ));
        assert!(settings_lifecycle_allowed_for_gate(
            VisualQaGateState::Disabled
        ));
    }

    #[test]
    fn visual_qa_rejects_inherited_controller_and_secret_authority() {
        assert!(validate_visual_qa_authority_values(Some("http://127.0.0.1:43127"), None).is_err());
        assert!(validate_visual_qa_authority_values(None, Some("synthetic-secret")).is_err());
        assert!(validate_visual_qa_authority_values(None, None).is_ok());
    }
}
