use crate::state::QueryApiSession;
use rand::RngCore;
use serde::Deserialize;
use std::env;
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

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
    // 1. 优先读取环境变量 PROXYLENS_DB_PATH
    if let Ok(env_path) = env::var("PROXYLENS_DB_PATH") {
        let p = PathBuf::from(&env_path);
        if p.exists() {
            return Ok(p);
        }
        return Err(format!("PROXYLENS_DB_PATH is set to '{}' but the file does not exist", env_path));
    }

    // 2. 尝试寻找当前仓库或开发目录下的默认测试数据库
    let dev_candidates = [
        Path::new("collector").join("testdata").join("proxylens.db"),
        Path::new("..").join("collector").join("testdata").join("proxylens.db"),
        Path::new("..").join("..").join("collector").join("testdata").join("proxylens.db"),
    ];

    for c in &dev_candidates {
        if c.exists() {
            if let Ok(canonical) = c.canonicalize() {
                return Ok(canonical);
            }
        }
    }

    Err("DB_NOT_CONFIGURED: Set PROXYLENS_DB_PATH environment variable to an existing SQLite DB".to_string())
}

pub fn find_sidecar_binary() -> Result<PathBuf, String> {
    // 1. 如果指定了 PROXYLENS_SIDECAR_PATH
    if let Ok(p) = env::var("PROXYLENS_SIDECAR_PATH") {
        let path = PathBuf::from(&p);
        if path.exists() {
            return Ok(path);
        }
    }

    // 2. 在 src-tauri/binaries 目录按目标架构寻找
    let exe_ext = if cfg!(windows) { ".exe" } else { "" };
    let candidates = [
        format!("binaries/proxylens-query-api-x86_64-pc-windows-msvc{}", exe_ext),
        format!("../src-tauri/binaries/proxylens-query-api-x86_64-pc-windows-msvc{}", exe_ext),
        format!("src-tauri/binaries/proxylens-query-api-x86_64-pc-windows-msvc{}", exe_ext),
        format!("../../ui/src-tauri/binaries/proxylens-query-api-x86_64-pc-windows-msvc{}", exe_ext),
        format!("collector/proxylens-query-api{}", exe_ext),
        format!("../collector/proxylens-query-api{}", exe_ext),
        format!("../../collector/proxylens-query-api{}", exe_ext),
    ];

    for c in &candidates {
        let p = PathBuf::from(c);
        if p.exists() {
            if let Ok(canonical) = p.canonicalize() {
                return Ok(canonical);
            }
            return Ok(p);
        }
    }

    Err("Sidecar binary 'proxylens-query-api' not found. Run 'npm run sidecar:build' first.".to_string())
}

pub fn spawn_query_sidecar(db_path: &Path) -> Result<(QueryApiSession, Child), String> {
    let sidecar_path = find_sidecar_binary()?;
    let token = generate_high_entropy_token();

    let mut cmd = Command::new(&sidecar_path);
    cmd.arg("--db")
        .arg(db_path)
        .arg("--listen")
        .arg("127.0.0.1:0")
        .arg("--token")
        .arg(&token)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit());

    let mut child = cmd.spawn().map_err(|e| format!("Failed to spawn query API sidecar: {}", e))?;

    let stdout = child.stdout.take().ok_or_else(|| "Failed to capture sidecar stdout".to_string())?;
    let mut reader = BufReader::new(stdout);
    let mut ready_line = String::new();

    let start_time = Instant::now();
    let timeout = Duration::from_secs(5);

    // 读取第一行 stdout 进行就绪握手
    while start_time.elapsed() < timeout {
        ready_line.clear();
        match reader.read_line(&mut ready_line) {
            Ok(0) => {
                // EOF
                break;
            }
            Ok(_) => {
                let trimmed = ready_line.trim();
                if trimmed.is_empty() {
                    continue;
                }
                if let Ok(signal) = serde_json::from_str::<ReadySignal>(trimmed) {
                    if signal.signal_type == "proxylens-query-api-ready" {
                        let session = QueryApiSession {
                            base_url: format!("http://{}:{}", signal.host, signal.port),
                            token,
                            api_version: signal.api_version,
                        };
                        return Ok((session, child));
                    }
                }
            }
            Err(e) => {
                let _ = child.kill();
                return Err(format!("Error reading sidecar handshake: {}", e));
            }
        }
    }

    let _ = child.kill();
    Err("Timeout waiting for query API ready signal".to_string())
}

pub fn stop_query_sidecar(mut child: Child) {
    let _ = child.kill();
    let _ = child.wait();
}
