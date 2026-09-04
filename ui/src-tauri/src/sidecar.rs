use crate::state::QueryApiSession;
use rand::RngCore;
use serde::Deserialize;
use std::env;
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
    use super::resolve_db_path_from_values;
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
}
