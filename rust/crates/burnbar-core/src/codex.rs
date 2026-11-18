//! Codex usage (Go: `internal/usage/codex.go`).
//!
//! Uses only the documented `codex app-server` account RPCs. It never starts a
//! model turn, reads tokens itself or opens a login flow.

use crate::usage::{Provider, Usage, Window};
use chrono::{TimeZone, Utc};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

const RPC_TIMEOUT: Duration = Duration::from_secs(25);
const RATE_LIMIT_CODE: i64 = 429;

fn is_script(path: &Path) -> bool {
    path.extension()
        .and_then(|ext| ext.to_str())
        .map(|ext| ext.eq_ignore_ascii_case("cmd") || ext.eq_ignore_ascii_case("bat"))
        .unwrap_or(false)
}

fn find_in_path(name: &str) -> Option<PathBuf> {
    let path = std::env::var_os("PATH")?;
    std::env::split_paths(&path)
        .map(|dir| dir.join(name))
        .find(|candidate| candidate.is_file())
}

/// Locate the native Codex CLI, including the npm and desktop-app installs.
/// A command shell is never used, and an override must point at an executable.
pub fn find_executable() -> Result<PathBuf, String> {
    if let Some(path) = std::env::var_os("BURNBAR_CODEX_PATH") {
        if !path.is_empty() {
            return Ok(PathBuf::from(path));
        }
    }
    for name in ["codex.exe", "codex"] {
        if let Some(path) = find_in_path(name) {
            if !is_script(&path) {
                return Ok(path);
            }
        }
    }
    // The desktop app can install a CLI even when it is absent from PATH.
    if let Some(local) = std::env::var_os("LOCALAPPDATA") {
        let local = PathBuf::from(local);
        let direct = local
            .join("Programs")
            .join("OpenAI")
            .join("Codex")
            .join("bin")
            .join("codex.exe");
        if direct.is_file() {
            return Ok(direct);
        }
        let pattern = local
            .join("OpenAI")
            .join("Codex")
            .join("bin")
            .join("*")
            .join("codex.exe");
        if let Some(path) = newest_match(&pattern.to_string_lossy()) {
            return Ok(path);
        }
    }
    // npm installs keep the native binary under the shim's package directory.
    if let Some(shim) = find_in_path("codex.cmd") {
        let root = shim
            .parent()
            .map(|dir| dir.join("node_modules").join("@openai").join("codex"));
        if let Some(root) = root {
            for pattern in [
                root.join("node_modules")
                    .join("@openai")
                    .join("codex-win32-*")
                    .join("vendor")
                    .join("*")
                    .join("codex")
                    .join("codex.exe"),
                root.join("vendor")
                    .join("*")
                    .join("codex")
                    .join("codex.exe"),
            ] {
                if let Some(path) = newest_match(&pattern.to_string_lossy()) {
                    return Ok(path);
                }
            }
        }
    }
    Err("install Codex CLI, then run `codex login`".into())
}

fn newest_match(pattern: &str) -> Option<PathBuf> {
    let mut matches: Vec<(std::time::SystemTime, PathBuf)> = glob::glob(pattern)
        .ok()?
        .flatten()
        .filter(|path| path.is_file())
        .filter_map(|path| {
            let modified = std::fs::metadata(&path).ok()?.modified().ok()?;
            Some((modified, path))
        })
        .collect();
    matches.sort_by_key(|entry| std::cmp::Reverse(entry.0));
    matches.into_iter().next().map(|(_, path)| path)
}

#[derive(Debug)]
enum RpcError {
    RateLimited,
    Message(String),
}

impl RpcError {
    fn message(value: impl Into<String>) -> Self {
        RpcError::Message(value.into())
    }
}

struct Rpc {
    reader: BufReader<std::process::ChildStdout>,
    writer: std::process::ChildStdin,
}

impl Rpc {
    fn send(&mut self, value: &Value) -> Result<(), RpcError> {
        let mut line = serde_json::to_vec(value)
            .map_err(|_| RpcError::message("Codex usage connection closed"))?;
        line.push(b'\n');
        self.writer
            .write_all(&line)
            .and_then(|_| self.writer.flush())
            .map_err(|_| RpcError::message("Codex usage connection closed"))
    }

    fn call(&mut self, id: i64, method: &str, params: Value) -> Result<Value, RpcError> {
        self.send(&json!({"id": id, "method": method, "params": params}))?;
        let mut line = String::new();
        loop {
            line.clear();
            let read = self
                .reader
                .read_line(&mut line)
                .map_err(|_| RpcError::message("Codex usage connection closed"))?;
            if read == 0 {
                return Err(RpcError::message("Codex usage connection closed"));
            }
            let message: Value = serde_json::from_str(line.trim_end())
                .map_err(|_| RpcError::message("unexpected response from Codex CLI"))?;
            let server_method = message.get("method").and_then(Value::as_str).unwrap_or("");
            if !server_method.is_empty() {
                // Reject server requests instead of letting a local account
                // probe trigger tools, authentication or interactive actions.
                if let Some(request_id) = message.get("id") {
                    if !request_id.is_null() {
                        let reply = json!({
                            "id": request_id,
                            "error": {"code": -32601, "message": "unsupported method"}
                        });
                        self.send(&reply)?;
                    }
                }
                continue;
            }
            let response_id = message.get("id").and_then(Value::as_i64);
            if response_id != Some(id) {
                continue;
            }
            if let Some(error) = message.get("error") {
                let code = error.get("code").and_then(Value::as_i64).unwrap_or(0);
                if code == RATE_LIMIT_CODE {
                    return Err(RpcError::RateLimited);
                }
                // Do not surface arbitrary server text that could contain account data.
                return Err(RpcError::message(
                    "Codex usage unavailable - sign in or update Codex CLI",
                ));
            }
            return match message.get("result") {
                Some(result) if !result.is_null() => Ok(result.clone()),
                _ => Err(RpcError::message("unexpected response from Codex CLI")),
            };
        }
    }
}

fn spawn_codex(path: &Path) -> Result<Child, String> {
    let mut command = Command::new(path);
    command
        .arg("app-server")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    // Account requests do not need the caller's project configuration.
    if let Some(home) = dirs::home_dir() {
        command.current_dir(home);
    }
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        // CREATE_NO_WINDOW: no console flash when a tray app spawns the CLI.
        command.creation_flags(0x0800_0000);
    }
    command
        .spawn()
        .map_err(|_| "could not start Codex CLI - check its installation".to_string())
}

fn run_rpc(stdout: ChildStdout, stdin: ChildStdin) -> Result<Vec<u8>, RpcError> {
    let mut rpc = Rpc {
        reader: BufReader::with_capacity(2 << 20, stdout),
        writer: stdin,
    };
    let version = crate::version();
    rpc.call(
        1,
        "initialize",
        json!({"clientInfo": {"name": "burnbar", "title": "BurnBar", "version": version}}),
    )?;
    rpc.send(&json!({"method": "initialized", "params": {}}))?;
    let account = rpc.call(2, "account/read", json!({"refreshToken": false}))?;
    let account_type = account
        .get("account")
        .and_then(|a| a.get("type"))
        .and_then(Value::as_str);
    match account_type {
        None | Some("apiKey") => {
            return Err(RpcError::message(
                "sign in to Codex with ChatGPT: `codex login`",
            ))
        }
        Some(_) => {}
    }
    let body = rpc.call(3, "account/rateLimits/read", json!({}))?;
    serde_json::to_vec(&body).map_err(|_| RpcError::message("unexpected response from Codex CLI"))
}

/// Ask the Codex app-server for account rate limits.
pub fn fetch_raw() -> Result<Vec<u8>, FetchError> {
    let path = find_executable().map_err(FetchError::message)?;
    let mut child = spawn_codex(&path).map_err(FetchError::message)?;

    // The reader blocks on the pipe, so run the conversation on its own thread
    // and let the caller enforce the timeout by killing the child.
    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| FetchError::message("could not open Codex usage connection"))?;
    let stdin = child
        .stdin
        .take()
        .ok_or_else(|| FetchError::message("could not open Codex usage connection"))?;
    let (tx, rx) = mpsc::channel();
    let worker = std::thread::spawn(move || {
        let _ = tx.send(run_rpc(stdout, stdin));
    });

    let result = rx.recv_timeout(RPC_TIMEOUT);
    let _ = child.kill();
    let _ = child.wait();
    let _ = worker.join();

    match result {
        Ok(Ok(body)) => Ok(body),
        Ok(Err(RpcError::RateLimited)) => Err(FetchError {
            message: "usage API rate limited, backing off".into(),
            rate_limited: true,
            retry_at: None,
        }),
        Ok(Err(RpcError::Message(message))) => Err(FetchError::message(message)),
        Err(_) => Err(FetchError::message("Codex usage request timed out")),
    }
}

#[derive(Debug, Clone)]
pub struct FetchError {
    pub message: String,
    pub rate_limited: bool,
    pub retry_at: Option<chrono::DateTime<Utc>>,
}

impl FetchError {
    fn message(value: impl Into<String>) -> Self {
        Self {
            message: value.into(),
            rate_limited: false,
            retry_at: None,
        }
    }
}

impl std::fmt::Display for FetchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for FetchError {}

/// Fetch and parse, reporting failures inside `Usage::err`.
pub fn fetch() -> Usage {
    match fetch_raw() {
        Ok(body) => parse(&body),
        Err(err) => Usage {
            provider: Provider::Codex,
            err: err.message,
            rate_limited: err.rate_limited,
            retry_at: err.retry_at,
            ..Usage::new(Provider::Codex)
        },
    }
}

#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct CodexWindow {
    used_percent: Option<f64>,
    window_duration_mins: Option<i64>,
    resets_at: Option<i64>,
}

#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct CodexLimits {
    limit_id: Option<String>,
    limit_name: Option<String>,
    primary: Option<CodexWindow>,
    secondary: Option<CodexWindow>,
}

impl CodexLimits {
    fn limit_id(&self) -> &str {
        self.limit_id.as_deref().unwrap_or("")
    }

    fn limit_name(&self) -> &str {
        self.limit_name.as_deref().unwrap_or("")
    }
}

#[derive(Debug, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct RateLimits {
    rate_limits: Option<CodexLimits>,
    rate_limits_by_limit_id: Option<BTreeMap<String, CodexLimits>>,
}

fn valid_percent(value: f64) -> bool {
    (0.0..=100.0).contains(&value)
}

fn to_window(w: Option<&CodexWindow>, key: &str, label: &str) -> Option<Window> {
    let w = w?;
    let percent = w.used_percent?;
    if !valid_percent(percent) {
        return None;
    }
    let mut window = Window::new(key, label, percent);
    if let Some(minutes) = w.window_duration_mins {
        let max = 365 * 24 * 60;
        if minutes > 0 && minutes <= max {
            window = window.with_length(Duration::from_secs(minutes as u64 * 60));
            window.label = format!("{} ({})", window.label, window.short_label(""));
        }
    }
    if let Some(resets) = w.resets_at {
        if resets > 0 {
            window.resets_at = Utc.timestamp_opt(resets, 0).single();
        }
    }
    Some(window)
}

/// Decode `account/rateLimits/read` output. Supports the legacy single bucket
/// and the current multi-bucket response; durations come from Codex instead of
/// assuming every plan uses 5h/7d quotas.
pub fn parse(body: &[u8]) -> Usage {
    let mut usage = Usage {
        provider: Provider::Codex,
        updated_at: Some(Utc::now()),
        ..Usage::new(Provider::Codex)
    };
    let result: RateLimits = match serde_json::from_slice(body) {
        Ok(result) => result,
        Err(_) => {
            usage.err = "unexpected response from Codex usage".into();
            return usage;
        }
    };
    let buckets = result.rate_limits_by_limit_id.unwrap_or_default();
    let mut main = buckets.get("codex").cloned();
    let mut found = main.is_some();
    if !found {
        if let Some(rate_limits) = result.rate_limits.clone() {
            main = Some(rate_limits);
            found = true;
        }
    }
    let main_id = main
        .as_ref()
        .map(|m| m.limit_id().to_string())
        .unwrap_or_default();

    if let Some(main) = &main {
        if found {
            usage.five_hour = to_window(main.primary.as_ref(), "primary", "Session");
            usage.seven_day = to_window(main.secondary.as_ref(), "secondary", "Weekly");
            if let Some(weekly) = &mut usage.seven_day {
                if weekly.length_secs != 7 * 86_400 {
                    weekly.label = weekly.label.replacen("Weekly", "Secondary", 1);
                }
            }
        }
    }

    for (key, bucket) in &buckets {
        if key == "codex"
            || (found && !main_id.is_empty() && (key == &main_id || bucket.limit_id() == main_id))
        {
            continue;
        }
        let name = if bucket.limit_name().is_empty() {
            key.clone()
        } else {
            bucket.limit_name().to_string()
        };
        for window in [
            to_window(bucket.primary.as_ref(), &format!("{key}/primary"), &name),
            to_window(
                bucket.secondary.as_ref(),
                &format!("{key}/secondary"),
                &format!("{name} secondary"),
            ),
        ]
        .into_iter()
        .flatten()
        {
            usage.extra.push(window);
        }
    }

    if usage.is_empty() {
        usage.err = "Codex returned no plan usage limits".into();
    }
    usage
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn parses_current_multibucket_shape() {
        let body = json!({
            "rateLimits": {"limitId": "codex", "limitName": null,
                "primary": {"usedPercent": 38, "windowDurationMins": 300, "resetsAt": 1767225600},
                "secondary": {"usedPercent": 24, "windowDurationMins": 10080, "resetsAt": 1767830400}},
            "rateLimitsByLimitId": {
                "codex": {"limitId": "codex", "primary": {"usedPercent": 38, "windowDurationMins": 300, "resetsAt": 1767225600},
                    "secondary": {"usedPercent": 24, "windowDurationMins": 10080, "resetsAt": 1767830400}},
                "codex-spark": {"limitId": "codex-spark", "limitName": "Spark",
                    "primary": {"usedPercent": 9, "windowDurationMins": 300, "resetsAt": 1767225600},
                    "secondary": {"usedPercent": 4, "windowDurationMins": 10080, "resetsAt": 1767830400}}
            }
        })
        .to_string();
        let usage = parse(body.as_bytes());
        assert_eq!(usage.err, "");
        let five = usage.five_hour.as_ref().unwrap();
        assert_eq!(five.label, "Session (5h)");
        assert_eq!(five.percent, 38.0);
        assert_eq!(five.length_secs, 5 * 3600);
        assert_eq!(usage.seven_day.as_ref().unwrap().label, "Weekly (7d)");
        assert_eq!(usage.extra.len(), 2);
        assert_eq!(usage.extra[0].label, "Spark (5h)");
        assert_eq!(usage.extra[1].label, "Spark secondary (7d)");
    }

    #[test]
    fn parses_legacy_single_bucket_and_renames_nonweekly_secondary() {
        let usage = parse(
            json!({"rateLimits": {"primary": {"usedPercent": 12, "windowDurationMins": 60}, "secondary": {"usedPercent": 50, "windowDurationMins": 1440}}}).to_string().as_bytes(),
        );
        assert_eq!(usage.err, "");
        assert_eq!(usage.five_hour.as_ref().unwrap().label, "Session (1h)");
        assert_eq!(usage.seven_day.as_ref().unwrap().label, "Secondary (1d)");
    }

    #[test]
    fn missing_limits_is_reported() {
        assert_eq!(parse(b"{}").err, "Codex returned no plan usage limits");
        assert_eq!(parse(b"[").err, "unexpected response from Codex usage");
    }
}
