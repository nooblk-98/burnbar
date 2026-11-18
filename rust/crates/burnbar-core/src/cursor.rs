//! Cursor plan usage.
//!
//! Cursor stores the signed-in session in its own local SQLite state. BurnBar
//! reads only `cursorAuth/accessToken`, then asks Cursor's dashboard service for
//! the current billing-period usage. Nothing is written back.
//!
//! State paths come from `dirs` on every platform. On macOS, Cursor's encrypted
//! Safe Storage token is unlocked through its existing login-keychain item.

use crate::usage::{Provider, Usage, Window};
#[cfg(any(target_os = "macos", test))]
use aes::Aes128;
#[cfg(any(target_os = "macos", test))]
use base64::{engine::general_purpose::STANDARD, Engine as _};
#[cfg(any(target_os = "macos", test))]
use cbc::cipher::{block_padding::Pkcs7, BlockDecryptMut, KeyIvInit};
use chrono::{DateTime, Utc};
#[cfg(any(target_os = "macos", test))]
use pbkdf2::pbkdf2_hmac;
use rusqlite::{Connection, OpenFlags};
#[cfg(any(target_os = "macos", test))]
use sha1::Sha1;
use std::path::{Path, PathBuf};
use std::time::Duration;

pub const API_URL: &str =
    "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage";
const MAX_BODY: u64 = 1 << 20;
const SAFE_STORAGE_PREFIX: &str = "cursor-safe-storage:v1:";

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
            message: "Cursor usage is rate limited, backing off".into(),
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

/// Cursor's global state databases, in preference order. `dirs::config_dir()`
/// resolves to `%APPDATA%`, `~/Library/Application Support` and `~/.config`.
pub fn state_paths() -> Vec<PathBuf> {
    let mut paths = Vec::new();
    let mut push = |path: PathBuf| {
        if path.is_file() && !paths.contains(&path) {
            paths.push(path);
        }
    };
    if let Some(explicit) = std::env::var_os("CURSOR_STATE_DB") {
        push(PathBuf::from(explicit));
    }
    if let Some(base) = dirs::config_dir() {
        push(
            base.join("Cursor")
                .join("User")
                .join("globalStorage")
                .join("state.vscdb"),
        );
    }
    if let Some(home) = dirs::home_dir() {
        push(
            home.join(".config")
                .join("Cursor")
                .join("User")
                .join("globalStorage")
                .join("state.vscdb"),
        );
    }
    paths
}

#[cfg(any(target_os = "macos", test))]
fn decrypt_safe_storage(encoded: &str, password: &str) -> Result<String, String> {
    let mut encrypted = STANDARD
        .decode(encoded)
        .map_err(|_| "Cursor Safe Storage token is malformed".to_string())?;
    if !encrypted.starts_with(b"v10") {
        return Err("Cursor Safe Storage format is unsupported".into());
    }
    let ciphertext = &mut encrypted[3..];
    let mut key = [0_u8; 16];
    pbkdf2_hmac::<Sha1>(password.as_bytes(), b"saltysalt", 1003, &mut key);
    let plaintext = cbc::Decryptor::<Aes128>::new(&key.into(), (&[b' '; 16]).into())
        .decrypt_padded_mut::<Pkcs7>(ciphertext)
        .map_err(|_| "Cursor Safe Storage could not be unlocked".to_string())?;
    let token = std::str::from_utf8(plaintext)
        .map_err(|_| "Cursor Safe Storage token is malformed".to_string())?
        .trim();
    if token.is_empty() {
        return Err("Cursor is not signed in - sign in to Cursor, then refresh".into());
    }
    Ok(token.to_string())
}

#[cfg(target_os = "macos")]
fn safe_storage_password() -> Result<String, String> {
    use std::process::Command;

    for account in ["Cursor Key", "Cursor"] {
        let output = Command::new("/usr/bin/security")
            .args([
                "find-generic-password",
                "-s",
                "Cursor Safe Storage",
                "-a",
                account,
                "-w",
            ])
            .output();
        if let Ok(output) = output {
            if output.status.success() {
                let password = String::from_utf8_lossy(&output.stdout).trim().to_string();
                if !password.is_empty() {
                    return Ok(password);
                }
            }
        }
    }
    Err("Cursor credentials are locked - allow BurnBar keychain access, then refresh".into())
}

fn decode_stored_token(value: &str) -> Result<String, String> {
    let value = value.trim();
    let value = serde_json::from_str::<String>(value).unwrap_or_else(|_| value.to_string());
    let value = value.trim();
    if value.is_empty() {
        return Err("Cursor is not signed in - sign in to Cursor, then refresh".into());
    }
    let Some(encoded) = value.strip_prefix(SAFE_STORAGE_PREFIX) else {
        return Ok(value.to_string());
    };

    #[cfg(target_os = "macos")]
    {
        return decrypt_safe_storage(encoded, &safe_storage_password()?);
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = encoded;
        Err("Cursor Safe Storage credentials can only be unlocked on macOS".into())
    }
}

fn token_from_database(path: &Path) -> Result<String, String> {
    let flags = OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX;
    let connection = Connection::open_with_flags(path, flags)
        .map_err(|_| "Cursor state could not be opened".to_string())?;
    let _ = connection.busy_timeout(Duration::from_secs(2));
    let value: String = connection
        .query_row(
            "SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken' LIMIT 1",
            [],
            |row| row.get(0),
        )
        .map_err(|_| "Cursor is not signed in - sign in to Cursor, then refresh".to_string())?;
    decode_stored_token(&value)
}

/// Read Cursor's access token without modifying its state.
pub fn read_token() -> Result<String, String> {
    if let Ok(token) = std::env::var("CURSOR_ACCESS_TOKEN") {
        if !token.trim().is_empty() {
            return Ok(token.trim().to_string());
        }
    }
    let mut first_error = None;
    for path in state_paths() {
        match token_from_database(&path) {
            Ok(token) => return Ok(token),
            Err(err) => {
                if first_error.is_none() {
                    first_error = Some(err);
                }
            }
        }
    }
    Err(first_error
        .unwrap_or_else(|| "Cursor is not signed in - sign in to Cursor, then refresh".into()))
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
    format!("burnbar/{}", crate::version())
}

/// POST the dashboard query. `base_url` is overridable for local tests.
pub fn fetch_raw_from(base_url: &str, token: &str) -> Result<Vec<u8>, FetchError> {
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    let response = client
        .post(base_url)
        .header("Authorization", format!("Bearer {token}"))
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .header("Connect-Protocol-Version", "1")
        .header("User-Agent", user_agent())
        .body("{}")
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
    if status == reqwest::StatusCode::UNAUTHORIZED || status == reqwest::StatusCode::FORBIDDEN {
        return Err(FetchError::new(
            "Cursor sign-in rejected - sign in to Cursor again",
        ));
    }
    if status == reqwest::StatusCode::TOO_MANY_REQUESTS {
        return Err(FetchError::rate_limited(retry_at));
    }
    if !status.is_success() {
        return Err(FetchError::new(format!(
            "Cursor usage API returned HTTP {}",
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
    let token = read_token().map_err(FetchError::new)?;
    fetch_raw_from(API_URL, &token)
}

/// Fetch and parse, reporting failures inside `Usage::err`.
pub fn fetch() -> Usage {
    match fetch_raw() {
        Ok(body) => parse(&body),
        Err(err) => Usage {
            provider: Provider::Cursor,
            err: err.message,
            rate_limited: err.rate_limited,
            retry_at: err.retry_at,
            ..Usage::new(Provider::Cursor)
        },
    }
}

#[derive(Debug, Default, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct PlanUsage {
    limit: Option<f64>,
    remaining: Option<f64>,
    used: Option<f64>,
    total_percent_used: Option<f64>,
    auto_percent_used: Option<f64>,
    api_percent_used: Option<f64>,
}

#[derive(Debug, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct Payload {
    billing_cycle_start: Option<String>,
    billing_cycle_end: Option<String>,
    plan_usage: Option<PlanUsage>,
}

fn valid_percent(value: f64) -> bool {
    (0.0..=100.0).contains(&value)
}

fn parse_time(value: Option<&str>) -> Option<DateTime<Utc>> {
    let raw = value?.trim();
    if raw.is_empty() {
        return None;
    }
    if let Ok(at) = DateTime::parse_from_rfc3339(raw) {
        return Some(at.with_timezone(&Utc));
    }
    let number: i64 = raw.parse().ok()?;
    // Cursor reports milliseconds in some responses and seconds in others.
    if number > 10_000_000_000 {
        DateTime::from_timestamp_millis(number)
    } else {
        DateTime::from_timestamp(number, 0)
    }
}

fn cycle_length(start: Option<DateTime<Utc>>, end: Option<DateTime<Utc>>) -> Duration {
    match (start, end) {
        (Some(start), Some(end)) if end > start => {
            Duration::from_secs_f64((end - start).num_milliseconds() as f64 / 1000.0)
        }
        _ => Duration::from_secs(30 * 86_400),
    }
}

/// Decode the dashboard response. Cursor bills in a monthly cycle, so the
/// overall plan percentage is the primary window and the auto/API splits are
/// extra limits.
pub fn parse(body: &[u8]) -> Usage {
    let mut usage = Usage {
        provider: Provider::Cursor,
        updated_at: Some(Utc::now()),
        ..Usage::new(Provider::Cursor)
    };
    let payload: Payload = match serde_json::from_slice(body) {
        Ok(payload) => payload,
        Err(_) => {
            usage.err = "unexpected response from Cursor usage".into();
            return usage;
        }
    };
    let Some(plan) = payload.plan_usage.as_ref() else {
        usage.err = "Cursor returned no plan usage limits".into();
        return usage;
    };
    let percent = plan
        .total_percent_used
        .or_else(|| match (plan.used, plan.limit) {
            (Some(used), Some(limit)) if limit > 0.0 => Some(used / limit * 100.0),
            _ => match (plan.remaining, plan.limit) {
                (Some(remaining), Some(limit)) if limit > 0.0 => {
                    Some((limit - remaining) / limit * 100.0)
                }
                _ => None,
            },
        });
    let Some(percent) = percent else {
        usage.err = "Cursor returned no plan usage limits".into();
        return usage;
    };
    if !valid_percent(percent) {
        usage.err = "invalid usage percentage from Cursor".into();
        return usage;
    }

    let start = parse_time(payload.billing_cycle_start.as_deref());
    let end = parse_time(payload.billing_cycle_end.as_deref());
    let length = cycle_length(start, end);
    usage.five_hour = Some(
        Window::new("monthly", "Monthly usage", percent)
            .with_reset(end)
            .with_length(length),
    );
    for (key, label, value) in [
        ("auto", "Auto usage", plan.auto_percent_used),
        ("api", "API usage", plan.api_percent_used),
    ] {
        if let Some(value) = value {
            if valid_percent(value) {
                usage.extra.push(
                    Window::new(key, label, value)
                        .with_reset(end)
                        .with_length(length),
                );
            }
        }
    }
    usage
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn parses_monthly_usage_and_splits() {
        let body = json!({
            "billingCycleStart": "2026-11-01T00:00:00.000Z",
            "billingCycleEnd": "2026-12-01T00:00:00.000Z",
            "planUsage": {
                "limit": 2000,
                "remaining": 900,
                "totalPercentUsed": 55,
                "autoPercentUsed": 40,
                "apiPercentUsed": 15
            }
        })
        .to_string();
        let usage = parse(body.as_bytes());
        assert_eq!(usage.provider, Provider::Cursor);
        assert_eq!(usage.err, "");
        let monthly = usage.five_hour.as_ref().unwrap();
        assert_eq!(monthly.label, "Monthly usage");
        assert_eq!(monthly.percent, 55.0);
        assert_eq!(monthly.length_secs, 30 * 86_400);
        assert_eq!(usage.extra.len(), 2);
        assert_eq!(usage.extra[0].label, "Auto usage");
    }

    #[test]
    fn computes_percent_when_the_server_omits_it() {
        let usage = parse(
            br#"{"planUsage":{"limit":100,"used":25},"billingCycleEnd":"2026-12-01T00:00:00Z"}"#,
        );
        assert_eq!(usage.five_hour.as_ref().unwrap().percent, 25.0);
    }

    #[test]
    fn reports_missing_and_malformed_bodies() {
        assert_eq!(
            parse(br#"{"planUsage":{}}"#).err,
            "Cursor returned no plan usage limits"
        );
        assert_eq!(parse(b"{}").err, "Cursor returned no plan usage limits");
        assert_eq!(parse(b"nope").err, "unexpected response from Cursor usage");
    }

    #[test]
    fn reads_the_token_from_cursor_state() {
        let dir = std::env::temp_dir().join(format!("burnbar-cursor-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("state.vscdb");
        {
            let connection = Connection::open(&path).unwrap();
            connection
                .execute("CREATE TABLE ItemTable (key TEXT, value TEXT)", [])
                .unwrap();
            connection
                .execute(
                    "INSERT INTO ItemTable VALUES ('cursorAuth/accessToken', ?1)",
                    [json!("test-token").to_string()],
                )
                .unwrap();
        }
        assert_eq!(token_from_database(&path).unwrap(), "test-token");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn decrypts_cursor_safe_storage_tokens() {
        let token =
            decrypt_safe_storage("djEwiSWYXaBIRVFFzU6zHsfmkA==", "fixture-password").unwrap();
        assert_eq!(token, "fixture-token");
    }
}
