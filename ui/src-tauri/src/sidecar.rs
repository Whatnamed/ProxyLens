use crate::state::QueryApiSession;
use rand::RngCore;
use serde::Deserialize;
use std::env;
use std::path::{Path, PathBuf};
use std::time::Duration;
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

pub fn resolve_db_path() -> Result<PathBuf, String> {
    // 严格按 G12 规范: 仅读取显式环境变量 PROXYLENS_DB_PATH，杜绝自动开发目录猜测导致连错 DB
    if let Ok(env_path) = env::var("PROXYLENS_DB_PATH") {
        let p = PathBuf::from(&env_path);
        if p.exists() {
            return Ok(p);
        }
        return Err(format!(
            "PROXYLENS_DB_PATH is set to '{}' but the file does not exist",
            env_path
        ));
    }

    Err("DB_NOT_CONFIGURED: Set PROXYLENS_DB_PATH environment variable to an existing SQLite DB path".to_string())
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
                        if trimmed.starts_with('{') && trimmed.contains("proxylens-query-api-ready") {
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
