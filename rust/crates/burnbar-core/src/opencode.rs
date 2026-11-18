//! OpenCode Go usage.
//!
//! OpenCode stores a read-only API key for its Go subscription in its local
//! credential store. BurnBar reads that key without modifying anything, then
//! asks the public Go usage endpoint for the rolling, weekly and monthly
//! allowance meters.
//!
//! The credential store is SQLite (`opencode.db`). Older OpenCode releases used
//! `auth.json` beside it; both are supported. Nothing but the `opencode-go` key
//! is read.

use crate::usage::{Provider, Usage, Window};
use chrono::{DateTime, Utc};
use rusqlite::{Connection, OpenFlags};
use serde_json::Value;
use std::path::{Path, PathBuf};
use std::time::Duration;

pub const API_URL: &str = "https://opencode.ai/zen/go/v1/usage";
const MAX_BODY: u64 = 1 << 20;

#[derive(Debug, Clone)]
pub struct FetchError {
    pub message: String,
    pub rate_limited: bool,
    pub retry_at: Option<DateTime<Utc>>,
}

impl FetchError {
    fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
            rate_limited: false,
            retry_at: None,
        }
    }

    fn rate_limited(retry_at: Option<DateTime<Utc>>) -> Self {
        Self {
            message: "OpenCode usage is rate limited, backing off".into(),
            rate_limited: true,
            retry_at,
        }
    }
}

impl std::fmt::Display for FetchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for FetchError {}

/// Folders OpenCode may use for its data, in preference order.
pub fn data_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    let mut push = |path: PathBuf| {
        if !dirs.contains(&path) {
            dirs.push(path);
        }
    };
    if let Some(explicit) = std::env::var_os("OPENCODE_DATA_DIR") {
        push(PathBuf::from(explicit));
    }
    for base in [
        dirs::data_local_dir(),
        dirs::data_dir(),
        dirs::home_dir().map(|home| home.join(".local").join("share")),
    ]
    .into_iter()
    .flatten()
    {
        push(base.join("opencode"));
        // OpenCode's Windows data folder is `%LOCALAPPDATA%\opencode\Data`.
        push(base.join("opencode").join("Data"));
    }
    dirs
}

fn push_unique(paths: &mut Vec<PathBuf>, path: PathBuf) {
    if path.is_file() && !paths.contains(&path) {
        paths.push(path);
    }
}

/// Candidate SQLite databases, newest first so an active channel wins.
pub fn database_paths() -> Vec<PathBuf> {
    let mut paths = Vec::new();
    for dir in data_dirs() {
        if let Some(explicit) = std::env::var_os("OPENCODE_DB") {
            let explicit = PathBuf::from(explicit);
            if explicit != Path::new(":memory:") {
                let candidate = if explicit.is_absolute() {
                    explicit
                } else {
                    dir.join(explicit)
                };
                push_unique(&mut paths, candidate);
            }
        }
        push_unique(&mut paths, dir.join("opencode.db"));
        let pattern = dir.join("opencode*.db");
        let mut matches: Vec<(std::time::SystemTime, PathBuf)> =
            glob::glob(&pattern.to_string_lossy())
                .map(|entries| {
                    entries
                        .flatten()
                        .filter(|path| path.is_file())
                        .filter_map(|path| {
                            let modified = std::fs::metadata(&path).ok()?.modified().ok()?;
                            Some((modified, path))
                        })
                        .collect()
                })
                .unwrap_or_default();
        matches.sort_by_key(|entry| std::cmp::Reverse(entry.0));
        for (_, path) in matches {
            push_unique(&mut paths, path);
        }
    }
    paths
}

fn key_from_value(value: &Value) -> Option<String> {
    for field in ["key", "access"] {
        if let Some(key) = value.get(field).and_then(Value::as_str) {
            let key = key.trim();
            if !key.is_empty() {
                return Some(key.to_string());
            }
        }
    }
    None
}

fn read_key_from_database(path: &Path) -> Result<String, String> {
    let flags = OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX;
    let connection = Connection::open_with_flags(path, flags)
        .map_err(|_| "OpenCode credential store could not be opened".to_string())?;
    let _ = connection.busy_timeout(Duration::from_secs(2));
    let mut statement = connection
        // OpenCode marks the selected credential with `active`; newer legacy
        // rows without that flag fall back to the most recently created row.
        .prepare(
            "SELECT value FROM credential
             WHERE integration_id = 'opencode-go'
             ORDER BY active DESC, time_created DESC, id DESC",
        )
        .map_err(|_| "OpenCode credential store has no readable credentials".to_string())?;
    let rows = statement
        .query_map([], |row| row.get::<_, String>(0))
        .map_err(|_| "OpenCode credential store could not be queried".to_string())?;
    for value in rows.flatten() {
        if let Ok(json) = serde_json::from_str::<Value>(&value) {
            if let Some(key) = key_from_value(&json) {
                return Ok(key);
            }
        }
    }
    Err("OpenCode Go is not signed in - connect it in OpenCode, then refresh".into())
}

fn read_key_from_auth_json(path: &Path) -> Result<String, String> {
    let body = std::fs::read(path).map_err(|_| {
        "OpenCode Go is not signed in - connect it in OpenCode, then refresh".to_string()
    })?;
    let json: Value = serde_json::from_slice(&body)
        .map_err(|_| "OpenCode credentials could not be read".to_string())?;
    for id in ["opencode-go", "opencode"] {
        if let Some(key) = json.get(id).and_then(key_from_value) {
            return Ok(key);
        }
    }
    Err("OpenCode Go is not signed in - connect it in OpenCode, then refresh".into())
}

/// Read the OpenCode Go API key without modifying OpenCode's data.
/// `OPENCODE_GO_API_KEY` is an explicit override for unusual installs.
pub fn read_key() -> Result<String, String> {
    if let Ok(key) = std::env::var("OPENCODE_GO_API_KEY") {
        if !key.trim().is_empty() {
            return Ok(key.trim().to_string());
        }
    }
    let mut first_error = None;
    for database in database_paths() {
        match read_key_from_database(&database) {
            Ok(key) => return Ok(key),
            Err(err) => {
                if first_error.is_none() {
                    first_error = Some(err);
                }
            }
        }
        if let Some(dir) = database.parent() {
            if let Ok(key) = read_key_from_auth_json(&dir.join("auth.json")) {
                return Ok(key);
            }
        }
    }
    for dir in data_dirs() {
        if let Ok(key) = read_key_from_auth_json(&dir.join("auth.json")) {
            return Ok(key);
        }
    }
    Err(first_error.unwrap_or_else(|| {
        "OpenCode Go is not signed in - connect it in OpenCode, then refresh".into()
    }))
}

fn retry_after(value: Option<&str>, now: DateTime<Utc>) -> Option<DateTime<Utc>> {
    let value = value?.trim();
    if let Ok(seconds) = value.parse::<u64>() {
        return Some(now + chrono::Duration::seconds(seconds.min(i64::MAX as u64) as i64));
    }
    DateTime::parse_from_rfc2822(value)
        .ok()
        .map(|at| at.with_timezone(&Utc))
        .filter(|at| *at > now)
}

fn user_agent() -> String {
    // OpenCode's edge rejects generic HTTP-library signatures, so identify the
    // application as BurnBar on every request.
    format!("burnbar/{}", crate::version())
}

/// GET the Go usage endpoint. `base_url` is overridable so tests can serve a
/// local response.
pub fn fetch_raw_from(base_url: &str, key: &str) -> Result<Vec<u8>, FetchError> {
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    let response = client
        .get(base_url)
        .header("Authorization", format!("Bearer {key}"))
        .header("Accept", "application/json")
        .header("User-Agent", user_agent())
        .send()
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    let status = response.status();
    let retry_at = retry_after(
        response
            .headers()
            .get("retry-after")
            .and_then(|value| value.to_str().ok()),
        Utc::now(),
    );
    if status == reqwest::StatusCode::UNAUTHORIZED {
        return Err(FetchError::new(
            "OpenCode sign-in rejected - reconnect OpenCode Go",
        ));
    }
    if status == reqwest::StatusCode::FORBIDDEN {
        return Err(FetchError::new(
            "OpenCode Go subscription not found - subscribe in OpenCode Console",
        ));
    }
    if status == reqwest::StatusCode::TOO_MANY_REQUESTS {
        return Err(FetchError::rate_limited(retry_at));
    }
    if !status.is_success() {
        return Err(FetchError::new(format!(
            "OpenCode usage API returned HTTP {}",
            status.as_u16()
        )));
    }
    let mut body = Vec::new();
    use std::io::Read;
    response
        .take(MAX_BODY)
        .read_to_end(&mut body)
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    Ok(body)
}

pub fn fetch_raw() -> Result<Vec<u8>, FetchError> {
    let key = read_key().map_err(FetchError::new)?;
    fetch_raw_from(API_URL, &key)
}

/// Fetch and parse, reporting failures inside `Usage::err`.
pub fn fetch() -> Usage {
    match fetch_raw() {
        Ok(body) => parse(&body),
        Err(err) => Usage {
            provider: Provider::Opencode,
            err: err.message,
            rate_limited: err.rate_limited,
            retry_at: err.retry_at,
            ..Usage::new(Provider::Opencode)
        },
    }
}

#[derive(Debug, Clone, Default, serde::Deserialize)]
struct Meter {
    status: Option<String>,
    percent: Option<f64>,
    #[serde(rename = "resetsAt")]
    resets_at: Option<String>,
}

#[derive(Debug, Default, serde::Deserialize)]
struct Buckets {
    rolling: Option<Meter>,
    weekly: Option<Meter>,
    monthly: Option<Meter>,
}

#[derive(Debug, serde::Deserialize)]
struct Payload {
    usage: Option<Buckets>,
}

fn valid_percent(value: f64) -> bool {
    (0.0..=100.0).contains(&value)
}

fn meter_window(
    meter: Option<&Meter>,
    key: &str,
    label: &str,
    length: Duration,
) -> Result<Option<Window>, String> {
    let Some(meter) = meter else {
        return Ok(None);
    };
    let Some(percent) = meter.percent else {
        return Ok(None);
    };
    if !valid_percent(percent) {
        return Err("invalid usage percentage from OpenCode".into());
    }
    let mut window = Window::new(key, label, percent);
    if length > Duration::ZERO {
        window = window.with_length(length);
    }
    if meter.status.as_deref() == Some("rate-limited") {
        window.label = format!("{} (limited)", window.label);
    }
    if let Some(resets) = meter.resets_at.as_deref() {
        window.resets_at = DateTime::parse_from_rfc3339(resets)
            .ok()
            .map(|at| at.with_timezone(&Utc));
    }
    Ok(Some(window))
}

/// Decode `/zen/go/v1/usage`. The endpoint reports the rolling (5-hour),
/// weekly (resets Monday 00:00 UTC) and monthly allowance percentages.
pub fn parse(body: &[u8]) -> Usage {
    let mut usage = Usage {
        provider: Provider::Opencode,
        updated_at: Some(Utc::now()),
        ..Usage::new(Provider::Opencode)
    };
    let payload: Payload = match serde_json::from_slice(body) {
        Ok(payload) => payload,
        Err(_) => {
            usage.err = "unexpected response from OpenCode usage".into();
            return usage;
        }
    };
    let Some(buckets) = payload.usage.as_ref() else {
        usage.err = "OpenCode returned no plan usage limits".into();
        return usage;
    };
    let rolling = meter_window(
        buckets.rolling.as_ref(),
        "rolling",
        "Session (5h)",
        Duration::from_secs(5 * 3600),
    );
    let weekly = meter_window(
        buckets.weekly.as_ref(),
        "weekly",
        "Weekly (7d)",
        Duration::from_secs(7 * 86_400),
    );
    let monthly = meter_window(
        buckets.monthly.as_ref(),
        "monthly",
        "Monthly allowance",
        Duration::ZERO,
    );
    match (rolling, weekly, monthly) {
        (Ok(rolling), Ok(weekly), Ok(monthly)) => {
            usage.five_hour = rolling;
            usage.seven_day = weekly;
            usage.extra.extend(monthly);
        }
        (Err(err), _, _) | (_, Err(err), _) | (_, _, Err(err)) => {
            usage.err = err;
            return usage;
        }
    }
    if usage.is_empty() {
        usage.err = "OpenCode returned no plan usage limits".into();
    }
    usage
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn parses_the_three_allowance_windows() {
        let body = json!({
            "usage": {
                "rolling": {"status": "ok", "percent": 1, "resetsAt": "2026-11-18T08:57:48.000Z"},
                "weekly": {"status": "ok", "percent": 2, "resetsAt": "2026-11-23T00:00:00.000Z"},
                "monthly": {"status": "ok", "percent": 29, "resetsAt": "2026-12-07T11:05:00.000Z"}
            }
        })
        .to_string();
        let usage = parse(body.as_bytes());
        assert_eq!(usage.provider, Provider::Opencode);
        assert_eq!(usage.err, "");
        let session = usage.five_hour.as_ref().unwrap();
        assert_eq!(session.percent, 1.0);
        assert_eq!(session.label, "Session (5h)");
        assert_eq!(session.length_secs, 5 * 3600);
        assert_eq!(usage.seven_day.as_ref().unwrap().percent, 2.0);
        assert_eq!(usage.extra.len(), 1);
        assert_eq!(usage.extra[0].label, "Monthly allowance");
        assert!(usage.extra[0].resets_at.is_some());
    }

    #[test]
    fn marks_a_limited_window_without_hiding_its_percentage() {
        let usage = parse(
            br#"{"usage":{"rolling":{"status":"rate-limited","percent":100,"resetsAt":"2026-11-18T08:57:48Z"}}}"#,
        );
        assert_eq!(usage.err, "");
        assert_eq!(
            usage.five_hour.as_ref().unwrap().label,
            "Session (5h) (limited)"
        );
        assert_eq!(usage.five_hour.as_ref().unwrap().percent, 100.0);
    }

    #[test]
    fn rejects_invalid_or_missing_bodies() {
        assert_eq!(
            parse(br#"{"usage":{"rolling":{"percent":101}}}"#).err,
            "invalid usage percentage from OpenCode"
        );
        assert_eq!(parse(b"{}").err, "OpenCode returned no plan usage limits");
        assert_eq!(
            parse(b"nope").err,
            "unexpected response from OpenCode usage"
        );
    }

    #[test]
    fn reads_the_active_key_from_an_opencode_database() {
        let dir = std::env::temp_dir().join(format!("burnbar-opencode-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("opencode.db");
        {
            let connection = Connection::open(&path).unwrap();
            connection
                .execute(
                    "CREATE TABLE credential (
                        id TEXT PRIMARY KEY,
                        integration_id TEXT,
                        label TEXT NOT NULL,
                        value TEXT NOT NULL,
                        active INTEGER,
                        time_created INTEGER,
                        time_updated INTEGER
                    )",
                    [],
                )
                .unwrap();
            connection
                .execute(
                    "INSERT INTO credential VALUES ('inactive', 'opencode-go', 'newer', ?1, 0, 2, 2)",
                    [json!({"type": "key", "key": "inactive-key"}).to_string()],
                )
                .unwrap();
            connection
                .execute(
                    "INSERT INTO credential VALUES ('active', 'opencode-go', 'selected', ?1, 1, 1, 1)",
                    [json!({"type": "key", "key": "active-key"}).to_string()],
                )
                .unwrap();
        }
        assert_eq!(read_key_from_database(&path).unwrap(), "active-key");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn reads_the_legacy_auth_json() {
        let dir =
            std::env::temp_dir().join(format!("burnbar-opencode-auth-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("auth.json");
        std::fs::write(
            &path,
            br#"{"opencode-go":{"type":"api","key":"legacy-key"}}"#,
        )
        .unwrap();
        assert_eq!(read_key_from_auth_json(&path).unwrap(), "legacy-key");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
