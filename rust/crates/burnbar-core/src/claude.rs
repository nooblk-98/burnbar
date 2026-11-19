//! Claude Code usage (Go: `internal/usage/usage.go`).
//!
//! BurnBar reuses Claude Code's login and renews expired OAuth tokens.

use crate::usage::{Share, Usage, Window};
use chrono::{DateTime, TimeZone, Utc};
use serde_json::Value;
use std::path::{Path, PathBuf};
use std::time::Duration;

pub const API_URL: &str = "https://api.anthropic.com/api/oauth/usage";
const TOKEN_URL: &str = "https://platform.claude.com/v1/oauth/token";
const CLIENT_ID: &str = "9d1c250a-e61b-44d9-88ed-5944d1962f5e";
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
            message: "usage API rate limited, backing off".into(),
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

/// Claude Code's configuration folder.
fn claude_dir() -> PathBuf {
    if let Some(dir) = std::env::var_os("CLAUDE_CONFIG_DIR") {
        if !dir.is_empty() {
            return PathBuf::from(dir);
        }
    }
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join(".claude")
}

fn credentials_path() -> PathBuf {
    claude_dir().join(".credentials.json")
}

fn read_credentials(path: &Path) -> Result<Value, String> {
    let body = std::fs::read(path)
        .map_err(|_| format!("no Claude Code login found ({})", path.display()))?;
    serde_json::from_slice(&body).map_err(|_| "Claude Code credentials file is invalid".to_string())
}

fn token_from_credentials(json: &Value, now: DateTime<Utc>) -> Result<String, String> {
    let oauth = &json["claudeAiOauth"];
    let token = oauth["accessToken"].as_str().unwrap_or("").trim();
    if token.is_empty() {
        return Err("Claude Code credentials file has no OAuth token".into());
    }
    let expires_at = oauth["expiresAt"].as_i64().unwrap_or(0);
    if expires_at > 0 {
        if let Some(expiry) = Utc.timestamp_millis_opt(expires_at).single() {
            if expiry < now {
                return Err("login token expired - run `claude` once to refresh it".into());
            }
        }
    }
    Ok(token.to_string())
}

fn read_token_at(path: &Path, now: DateTime<Utc>) -> Result<String, String> {
    token_from_credentials(&read_credentials(path)?, now)
}

fn refresh_token_at(path: &Path, token_url: &str, now: DateTime<Utc>) -> Result<String, String> {
    let mut credentials = read_credentials(path)?;
    if let Ok(token) = token_from_credentials(&credentials, now) {
        return Ok(token);
    }
    let refresh = credentials["claudeAiOauth"]["refreshToken"]
        .as_str()
        .filter(|value| !value.is_empty())
        .ok_or("Claude Code login expired - run `claude` to sign in again")?
        .to_string();
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|_| "could not renew Claude Code login".to_string())?;
    let response = client
        .post(token_url)
        .json(&serde_json::json!({
            "grant_type": "refresh_token",
            "refresh_token": refresh,
            "client_id": CLIENT_ID,
        }))
        .send()
        .map_err(|_| "could not renew Claude Code login".to_string())?;
    if !response.status().is_success() {
        // Claude may have refreshed the same login while our request was in flight.
        if let Ok(current) = read_credentials(path) {
            if current["claudeAiOauth"]["refreshToken"] != refresh {
                return token_from_credentials(&current, Utc::now());
            }
        }
        return Err(
            "Claude Code login could not be renewed - run `claude` to sign in again".into(),
        );
    }
    let renewal: Value = response
        .json()
        .map_err(|_| "invalid Claude login renewal response")?;
    let access = renewal["access_token"]
        .as_str()
        .filter(|v| !v.is_empty())
        .ok_or("invalid Claude login renewal response")?;
    let next_refresh = renewal["refresh_token"]
        .as_str()
        .filter(|v| !v.is_empty())
        .ok_or("invalid Claude login renewal response")?;
    let expires_in = renewal["expires_in"]
        .as_i64()
        .filter(|v| *v > 0)
        .ok_or("invalid Claude login renewal response")?;
    // Preserve any newer login written by Claude while the network request ran.
    let current = read_credentials(path)?;
    if current["claudeAiOauth"]["refreshToken"] != refresh {
        return token_from_credentials(&current, Utc::now());
    }
    credentials["claudeAiOauth"]["accessToken"] = Value::String(access.into());
    credentials["claudeAiOauth"]["refreshToken"] = Value::String(next_refresh.into());
    credentials["claudeAiOauth"]["expiresAt"] =
        Value::from((Utc::now() + chrono::Duration::seconds(expires_in)).timestamp_millis());
    let body = serde_json::to_vec(&credentials).map_err(|_| "could not save Claude login")?;
    let temporary = path.with_extension(format!("burnbar-{}.tmp", std::process::id()));
    let saved = (|| -> std::io::Result<()> {
        use std::io::Write;
        let mut file = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&temporary)?;
        file.write_all(&body)?;
        file.sync_all()?;
        std::fs::rename(&temporary, path)
    })();
    if saved.is_err() {
        let _ = std::fs::remove_file(&temporary);
    }
    saved.map_err(|_| "could not save Claude login")?;
    Ok(access.into())
}

/// Read the OAuth token Claude Code stores after `claude` login.
pub fn read_token() -> Result<String, String> {
    let path = credentials_path();
    let now = Utc::now();
    match read_token_at(&path, now) {
        Ok(token) => Ok(token),
        Err(err) if err.contains("expired") => refresh_token_at(&path, TOKEN_URL, now),
        Err(err) => Err(err),
    }
}

fn user_agent() -> String {
    format!("burnbar/{}", crate::version())
}

fn retry_after(value: Option<&str>, now: DateTime<Utc>) -> Option<DateTime<Utc>> {
    let value = value?.trim();
    if let Ok(seconds) = value.parse::<u64>() {
        return Some(now + chrono::Duration::seconds(seconds.min(i64::MAX as u64) as i64));
    }
    [
        DateTime::parse_from_rfc2822(value).map(|d| d.with_timezone(&Utc)),
        DateTime::parse_from_rfc3339(value).map(|d| d.with_timezone(&Utc)),
    ]
    .into_iter()
    .flatten()
    .find(|at| *at > now)
}

/// GET the usage API. `base_url` is overridable so tests can run a local server.
pub fn fetch_raw_from(base_url: &str, token: &str) -> Result<Vec<u8>, FetchError> {
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    let response = client
        .get(base_url)
        .header("Authorization", format!("Bearer {token}"))
        .header("anthropic-beta", "oauth-2025-04-20")
        .header("Accept", "application/json")
        .header("User-Agent", user_agent())
        .send()
        .map_err(|e| FetchError::new(format!("network error: {e}")))?;
    let status = response.status();
    let retry_at = retry_after(
        response
            .headers()
            .get("retry-after")
            .and_then(|v| v.to_str().ok()),
        Utc::now(),
    );
    if status == reqwest::StatusCode::UNAUTHORIZED {
        return Err(FetchError::new(
            "login rejected - run `claude` to sign in again",
        ));
    }
    if status == reqwest::StatusCode::TOO_MANY_REQUESTS {
        return Err(FetchError::rate_limited(retry_at));
    }
    if !status.is_success() {
        return Err(FetchError::new(format!(
            "usage API returned HTTP {}",
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

/// Fetch the raw usage API response using the stored Claude Code login.
pub fn fetch_raw() -> Result<Vec<u8>, FetchError> {
    let token = read_token().map_err(FetchError::new)?;
    fetch_raw_from(API_URL, &token)
}

/// Fetch and parse, reporting failures inside `Usage::err` like the Go app.
pub fn fetch() -> Usage {
    match fetch_raw() {
        Ok(body) => parse(&body),
        Err(err) => Usage {
            provider: crate::usage::Provider::Claude,
            err: err.message,
            rate_limited: err.rate_limited,
            retry_at: err.retry_at,
            ..Usage::new(crate::usage::Provider::Claude)
        },
    }
}

fn parse_time(value: Option<&Value>) -> Option<DateTime<Utc>> {
    let raw = value?.as_str()?;
    DateTime::parse_from_rfc3339(raw)
        .ok()
        .map(|d| d.with_timezone(&Utc))
}

fn valid_percent(value: f64) -> bool {
    (0.0..=100.0).contains(&value)
}

pub fn limit_label(kind: &str) -> String {
    match kind {
        "session" => return "Session (5h)".into(),
        "weekly_all" => return "Weekly (all models)".into(),
        _ => {}
    }
    kind.split('_')
        .filter(|word| !word.is_empty())
        .map(|word| {
            let mut chars = word.chars();
            match chars.next() {
                Some(first) => first.to_uppercase().collect::<String>() + chars.as_str(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

fn window_length(kind: &str, group: &str) -> Duration {
    if group == "session" || kind == "session" {
        Duration::from_secs(5 * 3_600)
    } else if group == "weekly" || kind.starts_with("weekly") {
        Duration::from_secs(7 * 86_400)
    } else {
        Duration::ZERO
    }
}

fn invalid(updated_at: Option<DateTime<Utc>>) -> Usage {
    Usage {
        provider: crate::usage::Provider::Claude,
        err: "invalid usage percentage from Claude".into(),
        updated_at,
        ..Usage::new(crate::usage::Provider::Claude)
    }
}

/// Decode a usage API response. Mirrors `Parse` in the Go package, including
/// its handling of older responses that only carry `five_hour` / `seven_day`.
pub fn parse(body: &[u8]) -> Usage {
    let now = Utc::now();
    let value: Value = match serde_json::from_slice(body) {
        Ok(value) => value,
        Err(_) => {
            return Usage {
                provider: crate::usage::Provider::Claude,
                err: "unexpected response from usage API".into(),
                updated_at: Some(now),
                ..Usage::new(crate::usage::Provider::Claude)
            }
        }
    };

    let util = |object: &Value| object.get("utilization").and_then(Value::as_f64);
    let legacy_five = value.get("five_hour");
    let legacy_seven = value.get("seven_day");

    for legacy in [legacy_five, legacy_seven].into_iter().flatten() {
        if let Some(percent) = util(legacy) {
            if !valid_percent(percent) {
                return invalid(Some(now));
            }
        }
    }

    let mut usage = Usage {
        provider: crate::usage::Provider::Claude,
        updated_at: Some(now),
        ..Usage::new(crate::usage::Provider::Claude)
    };

    if let Some(limits) = value.get("limits").and_then(Value::as_array) {
        for limit in limits {
            let Some(percent) = limit.get("percent").and_then(Value::as_f64) else {
                continue;
            };
            if !valid_percent(percent) {
                return invalid(Some(now));
            }
            let kind = limit.get("kind").and_then(Value::as_str).unwrap_or("");
            if kind.is_empty() {
                continue;
            }
            let group = limit.get("group").and_then(Value::as_str).unwrap_or("");
            let window = Window::new(kind, limit_label(kind), percent)
                .with_reset(parse_time(limit.get("resets_at")))
                .with_length(window_length(kind, group));
            match kind {
                "session" => usage.five_hour = Some(window),
                "weekly_all" => usage.seven_day = Some(window),
                _ => usage.extra.push(window),
            }
        }
    }

    // Older responses only carry the five_hour / seven_day objects.
    if usage.five_hour.is_none() {
        if let Some(percent) = legacy_five.and_then(util) {
            usage.five_hour = Some(
                Window::new("session", limit_label("session"), percent)
                    .with_reset(parse_time(legacy_five.and_then(|v| v.get("resets_at"))))
                    .with_length(window_length("session", "")),
            );
        }
    }
    if usage.seven_day.is_none() {
        if let Some(percent) = legacy_seven.and_then(util) {
            usage.seven_day = Some(
                Window::new("weekly_all", limit_label("weekly_all"), percent)
                    .with_reset(parse_time(legacy_seven.and_then(|v| v.get("resets_at"))))
                    .with_length(window_length("weekly_all", "")),
            );
        }
    }

    if let Some(rows) = value
        .get("seven_day_breakdown")
        .and_then(|b| b.get("rows"))
        .and_then(Value::as_array)
    {
        for row in rows {
            let percent = row.get("percent").and_then(Value::as_f64).unwrap_or(0.0);
            let name = row
                .get("display_name")
                .and_then(Value::as_str)
                .unwrap_or("")
                .to_string();
            if percent > 0.0 && valid_percent(percent) && !name.is_empty() {
                usage.breakdown.push(Share { name, percent });
            }
        }
    }

    if usage.is_empty() {
        usage.err = "Claude returned no plan usage limits".into();
    }
    usage
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn parses_current_limits_shape() {
        let body = json!({
            "five_hour": {"utilization": 62.0, "resets_at": "2026-01-02T03:04:05.123Z"},
            "seven_day": {"utilization": 17.0, "resets_at": "2026-01-05T03:04:05Z"},
            "limits": [
                {"kind": "session", "group": "session", "percent": 62.0, "resets_at": "2026-01-02T03:04:05Z"},
                {"kind": "weekly_all", "group": "weekly", "percent": 17.0, "resets_at": "2026-01-05T03:04:05Z"},
                {"kind": "weekly_opus", "group": "weekly", "percent": 35.0, "resets_at": "2026-01-05T03:04:05Z"}
            ],
            "seven_day_breakdown": {"rows": [
                {"display_name": "Claude Code", "percent": 91},
                {"display_name": "Chats", "percent": 7},
                {"display_name": "Zero", "percent": 0}
            ]}
        })
        .to_string();
        let usage = parse(body.as_bytes());
        assert_eq!(usage.err, "");
        let five = usage.five_hour.as_ref().unwrap();
        assert_eq!(five.key, "session");
        assert_eq!(five.label, "Session (5h)");
        assert_eq!(five.percent, 62.0);
        assert_eq!(five.length_secs, 5 * 3600);
        assert_eq!(
            usage.seven_day.as_ref().unwrap().label,
            "Weekly (all models)"
        );
        assert_eq!(usage.extra.len(), 1);
        assert_eq!(usage.extra[0].label, "Weekly Opus");
        assert_eq!(usage.breakdown.len(), 2);
        assert!(usage.updated_at.is_some());
    }

    #[test]
    fn parses_legacy_shape() {
        let body = br#"{"five_hour":{"utilization":10,"resets_at":"2026-01-02T03:04:05Z"},"seven_day":{"utilization":20,"resets_at":"2026-01-05T03:04:05Z"}}"#;
        let usage = parse(body);
        assert_eq!(usage.err, "");
        assert_eq!(usage.five_hour.unwrap().percent, 10.0);
        assert_eq!(usage.seven_day.unwrap().percent, 20.0);
    }

    #[test]
    fn rejects_out_of_range_percentages() {
        let usage = parse(br#"{"five_hour":{"utilization":101}}"#);
        assert_eq!(usage.err, "invalid usage percentage from Claude");
        assert!(usage.five_hour.is_none());
    }

    #[test]
    fn reports_missing_and_malformed_bodies() {
        assert_eq!(parse(b"{}").err, "Claude returned no plan usage limits");
        assert_eq!(parse(b"not json").err, "unexpected response from usage API");
    }

    #[test]
    fn labels_match_go() {
        assert_eq!(limit_label("session"), "Session (5h)");
        assert_eq!(limit_label("weekly_all"), "Weekly (all models)");
        assert_eq!(limit_label("weekly_opus"), "Weekly Opus");
    }

    #[test]
    fn token_reading_reports_expiry_and_missing_token() {
        let dir = std::env::temp_dir().join(format!("burnbar-claude-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join(".credentials.json");
        std::fs::write(
            &path,
            br#"{"claudeAiOauth":{"accessToken":"tok","expiresAt":0}}"#,
        )
        .unwrap();
        assert_eq!(read_token_at(&path, Utc::now()).unwrap(), "tok");
        std::fs::write(
            &path,
            br#"{"claudeAiOauth":{"accessToken":"tok","expiresAt":1}}"#,
        )
        .unwrap();
        assert!(read_token_at(&path, Utc::now())
            .unwrap_err()
            .contains("expired"));
        std::fs::write(&path, br#"{"claudeAiOauth":{}}"#).unwrap();
        assert!(read_token_at(&path, Utc::now())
            .unwrap_err()
            .contains("no OAuth token"));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn renews_expired_login_and_preserves_other_credential_fields() {
        use std::io::{Read, Write};
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}/token", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(5)))
                .unwrap();
            let mut request = Vec::new();
            let mut buffer = [0; 4096];
            loop {
                let count = stream.read(&mut buffer).unwrap();
                request.extend_from_slice(&buffer[..count]);
                if request
                    .windows(b"old-refresh".len())
                    .any(|part| part == b"old-refresh")
                {
                    break;
                }
            }
            let response =
                r#"{"access_token":"new-access","refresh_token":"new-refresh","expires_in":28800}"#;
            write!(
                stream,
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nContent-Type: application/json\r\n\r\n{}",
                response.len(),
                response
            )
            .unwrap();
        });
        let dir = std::env::temp_dir().join(format!("burnbar-renew-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join(".credentials.json");
        std::fs::write(&path, br#"{"other":"keep","claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1}}"#).unwrap();
        let token = refresh_token_at(&path, &url, Utc::now()).unwrap();
        server.join().unwrap();
        let updated = read_credentials(&path).unwrap();
        assert_eq!(token, "new-access");
        assert_eq!(updated["other"], "keep");
        assert_eq!(updated["claudeAiOauth"]["refreshToken"], "new-refresh");
        assert!(
            updated["claudeAiOauth"]["expiresAt"].as_i64().unwrap() > Utc::now().timestamp_millis()
        );
        let _ = std::fs::remove_dir_all(dir);
    }
}
