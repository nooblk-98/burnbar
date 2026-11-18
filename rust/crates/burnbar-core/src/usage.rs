//! Provider-neutral usage types (Go: `internal/usage/provider.go` and the
//! shared `Usage` / `Window` types in `internal/usage/usage.go`).

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::time::Duration;

/// The services BurnBar reads plan limits from.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Provider {
    Claude,
    Codex,
    Opencode,
    Cursor,
}

impl Provider {
    pub const ALL: [Provider; 4] = [
        Provider::Claude,
        Provider::Codex,
        Provider::Opencode,
        Provider::Cursor,
    ];

    /// Display name used in notifications and the UI.
    pub fn name(self) -> &'static str {
        match self {
            Provider::Claude => "Claude",
            Provider::Codex => "Codex",
            Provider::Opencode => "OpenCode",
            Provider::Cursor => "Cursor",
        }
    }

    /// Stable identifier used in JSON and history file names.
    pub fn key(self) -> &'static str {
        match self {
            Provider::Claude => "claude",
            Provider::Codex => "codex",
            Provider::Opencode => "opencode",
            Provider::Cursor => "cursor",
        }
    }

    /// JSONL file that holds this provider's readings.
    pub fn history_file(self) -> &'static str {
        match self {
            Provider::Claude => "history.jsonl",
            Provider::Codex => "history-codex.jsonl",
            Provider::Opencode => "history-opencode.jsonl",
            Provider::Cursor => "history-cursor.jsonl",
        }
    }
}

impl std::fmt::Display for Provider {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.name())
    }
}

/// True for the values accepted by the `providers` config key. `both` keeps its
/// original Claude + Codex meaning so existing profiles do not change; `all`
/// adds OpenCode.
pub fn valid_mode(mode: &str) -> bool {
    matches!(
        mode,
        "claude" | "codex" | "opencode" | "cursor" | "both" | "all"
    )
}

/// Resolve a provider mode into the providers that should be polled.
pub fn providers_for_mode(mode: &str) -> Vec<Provider> {
    match mode {
        "codex" => vec![Provider::Codex],
        "opencode" => vec![Provider::Opencode],
        "cursor" => vec![Provider::Cursor],
        "both" => vec![Provider::Claude, Provider::Codex],
        "all" => Provider::ALL.to_vec(),
        _ => vec![Provider::Claude],
    }
}

/// One usage limit, e.g. the 5-hour session or the 7-day week.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Window {
    pub key: String,
    pub label: String,
    pub percent: f64,
    pub resets_at: Option<DateTime<Utc>>,
    /// Length of the quota cycle in seconds (0 when the provider does not say).
    #[serde(default)]
    pub length_secs: i64,
}

impl Window {
    pub fn new(key: impl Into<String>, label: impl Into<String>, percent: f64) -> Self {
        Self {
            key: key.into(),
            label: label.into(),
            percent,
            resets_at: None,
            length_secs: 0,
        }
    }

    pub fn with_reset(mut self, resets_at: Option<DateTime<Utc>>) -> Self {
        self.resets_at = resets_at;
        self
    }

    pub fn with_length(mut self, length: Duration) -> Self {
        self.length_secs = length.as_secs().min(i64::MAX as u64) as i64;
        self
    }

    /// How far through the quota window we are (0..=1), for the pace marker.
    pub fn elapsed(&self) -> Option<f64> {
        self.elapsed_at(Utc::now())
    }

    pub fn elapsed_at(&self, now: DateTime<Utc>) -> Option<f64> {
        let resets = self.resets_at?;
        if self.length_secs <= 0 {
            return None;
        }
        let remaining = (resets - now).num_milliseconds() as f64 / 1000.0;
        let fraction = 1.0 - remaining / self.length_secs as f64;
        Some(fraction.clamp(0.0, 1.0))
    }

    /// Compact duration label: `5h`, `7d`, `90m`.
    pub fn short_label(&self, fallback: &str) -> String {
        match self.length_secs {
            n if n <= 0 => fallback.to_string(),
            n if n % 86_400 == 0 => format!("{}d", n / 86_400),
            n if n % 3_600 == 0 => format!("{}h", n / 3_600),
            n => format!("{}m", n / 60),
        }
    }
}

/// This week's usage split by product (Claude only).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Share {
    pub name: String,
    pub percent: f64,
}

/// A provider reading. `err` is data, not a panic: a failed fetch keeps the
/// previous good windows so the UI can explain what happened.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Usage {
    pub provider: Provider,
    pub five_hour: Option<Window>,
    pub seven_day: Option<Window>,
    #[serde(default)]
    pub extra: Vec<Window>,
    #[serde(default)]
    pub breakdown: Vec<Share>,
    pub updated_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub err: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub retry_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub rate_limited: bool,
}

impl Usage {
    pub fn new(provider: Provider) -> Self {
        Self {
            provider,
            five_hour: None,
            seven_day: None,
            extra: Vec::new(),
            breakdown: Vec::new(),
            updated_at: None,
            err: String::new(),
            retry_at: None,
            rate_limited: false,
        }
    }

    pub fn failed(provider: Provider, err: impl Into<String>) -> Self {
        Self {
            err: err.into(),
            ..Self::new(provider)
        }
    }

    pub fn all(&self) -> Vec<&Window> {
        self.five_hour
            .iter()
            .chain(self.seven_day.iter())
            .chain(self.extra.iter())
            .collect()
    }

    pub fn is_empty(&self) -> bool {
        self.all().is_empty()
    }
}

/// `PreserveLastGood` from Go: a failed attempt never relabels old data as fresh.
pub fn preserve_last_good(next: Usage, prev: Option<&Usage>) -> Usage {
    if next.err.is_empty() {
        return next;
    }
    let Some(prev) = prev else {
        return next;
    };
    let mut failed = next;
    failed.five_hour = prev.five_hour.clone();
    failed.seven_day = prev.seven_day.clone();
    failed.extra = prev.extra.clone();
    failed.breakdown = prev.breakdown.clone();
    failed.updated_at = if prev.is_empty() {
        None
    } else {
        prev.updated_at
    };
    failed
}

/// Sample numbers for `--demo` / `BURNBAR_DEMO=1`; lets the UI be exercised
/// without credentials and makes screenshot and smoke tests deterministic.
pub fn demo_usage(provider: Provider) -> Usage {
    let now = Utc::now();
    if provider == Provider::Cursor {
        return Usage {
            provider,
            five_hour: Some(
                Window::new("monthly", "Monthly usage", 54.0)
                    .with_reset(Some(now + chrono::Duration::days(20)))
                    .with_length(Duration::from_secs(30 * 86_400)),
            ),
            extra: vec![
                Window::new("auto", "Auto usage", 38.0)
                    .with_reset(Some(now + chrono::Duration::days(20)))
                    .with_length(Duration::from_secs(30 * 86_400)),
                Window::new("api", "API usage", 12.0)
                    .with_reset(Some(now + chrono::Duration::days(20)))
                    .with_length(Duration::from_secs(30 * 86_400)),
            ],
            updated_at: Some(now),
            ..Usage::new(provider)
        };
    }
    if provider == Provider::Opencode {
        return Usage {
            provider,
            five_hour: Some(
                Window::new("rolling", "Session (5h)", 46.0)
                    .with_reset(Some(now + chrono::Duration::hours(2)))
                    .with_length(Duration::from_secs(5 * 3_600)),
            ),
            seven_day: Some(
                Window::new("weekly", "Weekly (7d)", 24.0)
                    .with_reset(Some(now + chrono::Duration::days(3)))
                    .with_length(Duration::from_secs(7 * 86_400)),
            ),
            extra: vec![Window::new("monthly", "Monthly allowance", 61.0)
                .with_reset(Some(now + chrono::Duration::days(20)))],
            updated_at: Some(now),
            ..Usage::new(provider)
        };
    }
    if provider == Provider::Codex {
        return Usage {
            provider,
            five_hour: Some(
                Window::new("primary", "Session (5h)", 38.0)
                    .with_reset(Some(now + chrono::Duration::hours(2)))
                    .with_length(Duration::from_secs(5 * 3_600)),
            ),
            seven_day: Some(
                Window::new("secondary", "Weekly (7d)", 24.0)
                    .with_reset(Some(now + chrono::Duration::days(3)))
                    .with_length(Duration::from_secs(7 * 86_400)),
            ),
            updated_at: Some(now),
            ..Usage::new(provider)
        };
    }
    Usage {
        provider,
        five_hour: Some(
            Window::new("session", "Session (5h)", 62.0)
                .with_reset(Some(now + chrono::Duration::minutes(73)))
                .with_length(Duration::from_secs(5 * 3_600)),
        ),
        seven_day: Some(
            Window::new("weekly_all", "Weekly (all models)", 17.0)
                .with_reset(Some(
                    now + chrono::Duration::days(4) + chrono::Duration::hours(6),
                ))
                .with_length(Duration::from_secs(7 * 86_400)),
        ),
        extra: vec![Window::new("weekly_opus", "Weekly Opus", 35.0)
            .with_reset(Some(
                now + chrono::Duration::days(4) + chrono::Duration::hours(6),
            ))
            .with_length(Duration::from_secs(7 * 86_400))],
        breakdown: vec![
            Share {
                name: "Claude Code".into(),
                percent: 91.0,
            },
            Share {
                name: "Chats".into(),
                percent: 7.0,
            },
            Share {
                name: "Cowork".into(),
                percent: 2.0,
            },
        ],
        updated_at: Some(now),
        ..Usage::new(provider)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn elapsed_is_clamped_to_the_window() {
        let now = Utc::now();
        let w = Window::new("session", "Session", 50.0)
            .with_reset(Some(now + chrono::Duration::hours(2)))
            .with_length(Duration::from_secs(5 * 3_600));
        let elapsed = w.elapsed_at(now).unwrap();
        assert!((elapsed - 0.6).abs() < 0.01, "{elapsed}");
        assert_eq!(w.elapsed_at(now + chrono::Duration::days(1)), Some(1.0));
        assert_eq!(w.elapsed_at(now - chrono::Duration::days(1)), Some(0.0));
    }

    #[test]
    fn short_labels_match_go() {
        let cases = [
            (Duration::from_secs(7 * 86_400), "7d"),
            (Duration::from_secs(5 * 3_600), "5h"),
            (Duration::from_secs(90 * 60), "90m"),
        ];
        for (length, want) in cases {
            assert_eq!(
                Window::new("k", "l", 0.0)
                    .with_length(length)
                    .short_label("x"),
                want
            );
        }
        assert_eq!(Window::new("k", "l", 0.0).short_label("primary"), "primary");
    }

    #[test]
    fn provider_modes() {
        assert!(valid_mode("both"));
        assert!(valid_mode("all"));
        assert!(valid_mode("opencode"));
        assert!(valid_mode("cursor"));
        assert!(!valid_mode("neither"));
        assert_eq!(providers_for_mode("codex"), vec![Provider::Codex]);
        assert_eq!(providers_for_mode("opencode"), vec![Provider::Opencode]);
        assert_eq!(providers_for_mode("cursor"), vec![Provider::Cursor]);
        assert_eq!(
            providers_for_mode("both"),
            vec![Provider::Claude, Provider::Codex]
        );
        assert_eq!(
            providers_for_mode("all"),
            vec![
                Provider::Claude,
                Provider::Codex,
                Provider::Opencode,
                Provider::Cursor
            ]
        );
        assert_eq!(providers_for_mode("weird"), vec![Provider::Claude]);
    }

    #[test]
    fn failed_fetch_keeps_last_good_windows_without_freshening_them() {
        let mut prev = demo_usage(Provider::Claude);
        prev.updated_at = Some(Utc::now() - chrono::Duration::minutes(5));
        let failed = preserve_last_good(Usage::failed(Provider::Claude, "offline"), Some(&prev));
        assert_eq!(failed.err, "offline");
        assert_eq!(failed.five_hour, prev.five_hour);
        assert_eq!(failed.updated_at, prev.updated_at);
    }
}
