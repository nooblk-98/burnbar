//! The portable application service that ties providers, history, config and
//! alerts together (the logic half of Go's `internal/app`).
//!
//! The Tauri shell owns windows and tray state; everything here can run in a
//! unit test without a display.

use crate::alerts::{self, Milestone};
use crate::config::{AlertState, Config};
use crate::history::{History, Sample};
use crate::insights;
use crate::usage::{self, Provider, Usage};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::PathBuf;
use std::time::Duration;

const INITIAL_RATE_LIMIT_BACKOFF_SECS: i64 = 2 * 60;
const MAX_RATE_LIMIT_BACKOFF_SECS: i64 = 16 * 60;

#[derive(Clone, Copy, Debug)]
struct Cooldown {
    until: DateTime<Utc>,
    backoff_secs: i64,
}

/// Settings the tray configuration surface is allowed to edit. Internal state
/// (`milestoneState`, `alertState`) is never replaced by a settings save.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Settings {
    pub providers: String,
    pub popup_seconds: i32,
    pub milestone_popups: bool,
    pub always_show_popup: bool,
    pub interval_minutes: i32,
    pub alerts: bool,
    pub start_with_windows: bool,
    pub primary_thresholds: Vec<i32>,
    pub secondary_thresholds: Vec<i32>,
    pub quiet_hours: bool,
    pub quiet_start: i32,
    pub quiet_end: i32,
    pub respect_windows_quiet: bool,
    pub compact_notices: bool,
    pub native_notices: bool,
    pub show_forecast: bool,
    pub theme: String,
    pub text_scale: i32,
    pub hotkey: bool,
    pub check_updates: bool,
}

impl From<&Config> for Settings {
    fn from(config: &Config) -> Self {
        Self {
            providers: config.providers.clone(),
            popup_seconds: config.popup_seconds,
            milestone_popups: config.milestone_popups,
            always_show_popup: config.always_show_popup,
            interval_minutes: config.interval_minutes,
            alerts: config.alerts,
            start_with_windows: config.start_with_windows.unwrap_or(true),
            primary_thresholds: config.primary_thresholds.clone(),
            secondary_thresholds: config.secondary_thresholds.clone(),
            quiet_hours: config.quiet_hours,
            quiet_start: config.quiet_start,
            quiet_end: config.quiet_end,
            respect_windows_quiet: config.respect_windows_quiet,
            compact_notices: config.compact_notices,
            native_notices: config.native_notices,
            show_forecast: config.show_forecast,
            theme: config.theme.clone(),
            text_scale: config.text_scale,
            hotkey: config.hotkey,
            check_updates: config.check_updates,
        }
    }
}

impl Settings {
    fn apply_to(&self, config: &mut Config) {
        config.providers = self.providers.clone();
        config.popup_seconds = self.popup_seconds;
        config.milestone_popups = self.milestone_popups;
        config.always_show_popup = self.always_show_popup;
        config.interval_minutes = self.interval_minutes;
        config.alerts = self.alerts;
        config.start_with_windows = Some(self.start_with_windows);
        config.primary_thresholds = self.primary_thresholds.clone();
        config.secondary_thresholds = self.secondary_thresholds.clone();
        config.quiet_hours = self.quiet_hours;
        config.quiet_start = self.quiet_start;
        config.quiet_end = self.quiet_end;
        config.respect_windows_quiet = self.respect_windows_quiet;
        config.compact_notices = self.compact_notices;
        config.native_notices = self.native_notices;
        config.show_forecast = self.show_forecast;
        config.theme = self.theme.clone();
        config.text_scale = self.text_scale;
        config.hotkey = self.hotkey;
        config.check_updates = self.check_updates;
    }
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AlertNotice {
    pub title: String,
    pub body: String,
    pub warn: bool,
}

#[derive(Clone, Debug, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RefreshReport {
    pub fetched: Vec<Usage>,
    pub milestones: Vec<Milestone>,
    pub alerts: Vec<AlertNotice>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProviderView {
    pub provider: Provider,
    pub usage: Usage,
    pub freshness: String,
    pub health: String,
    pub primary_forecast: Option<String>,
    pub secondary_forecast: Option<String>,
    pub history24: Vec<Sample>,
    pub history7d: Vec<Sample>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UiState {
    pub settings: Settings,
    pub providers: Vec<ProviderView>,
    pub version: String,
    pub demo: bool,
    /// Operating system name shown in About; the UI has no per-platform layout.
    pub platform: String,
}

pub struct Service {
    pub dir: PathBuf,
    pub config: Config,
    usages: BTreeMap<Provider, Usage>,
    histories: BTreeMap<Provider, History>,
    cooldowns: BTreeMap<Provider, Cooldown>,
    first_run: bool,
    demo: bool,
}

impl Service {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        let dir = dir.into();
        let first_run = !Config::path_in(&dir).exists();
        let config = Config::load_from(&dir);
        let mut histories = BTreeMap::new();
        for provider in Provider::ALL {
            histories.insert(provider, History::load(&dir, provider));
        }
        Self {
            dir,
            config,
            usages: BTreeMap::new(),
            histories,
            cooldowns: BTreeMap::new(),
            first_run,
            demo: false,
        }
    }

    /// Demo mode makes the UI fully explorable without credentials and is used
    /// by the smoke tests. Nothing is written to disk.
    pub fn demo(dir: impl Into<PathBuf>) -> Self {
        let mut service = Self::new(dir);
        service.demo = true;
        service.config.providers = "all".into();
        for provider in Provider::ALL {
            service.usages.insert(provider, usage::demo_usage(provider));
            service.histories.insert(provider, History::demo());
        }
        service
    }

    pub fn first_run(&self) -> bool {
        self.first_run
    }

    pub fn usage(&self, provider: Provider) -> Option<&Usage> {
        self.usages.get(&provider)
    }

    pub fn history(&self, provider: Provider) -> Option<&History> {
        self.histories.get(&provider)
    }

    /// Refresh both configured providers in parallel.
    pub fn refresh(&mut self) -> RefreshReport {
        if self.demo {
            return self.refresh_with(usage::demo_usage);
        }
        self.refresh_with(|provider| match provider {
            Provider::Claude => crate::claude::fetch(),
            Provider::Codex => crate::codex::fetch(),
            Provider::Opencode => crate::opencode::fetch(),
            Provider::Cursor => crate::cursor::fetch(),
        })
    }

    /// Injectable refresh used by tests and the demo mode.
    pub fn refresh_with<F>(&mut self, fetch: F) -> RefreshReport
    where
        F: Fn(Provider) -> Usage + Sync,
    {
        let now = Utc::now();
        let providers: Vec<Provider> = self
            .config
            .providers()
            .into_iter()
            .filter(|provider| {
                self.cooldowns
                    .get(provider)
                    .map_or(true, |cooldown| cooldown.until <= now)
            })
            .collect();
        let fetched: Vec<Usage> = std::thread::scope(|scope| {
            let handles: Vec<_> = providers
                .iter()
                .map(|provider| (*provider, scope.spawn(|| fetch(*provider))))
                .collect();
            handles
                .into_iter()
                .map(|(provider, handle)| {
                    handle
                        .join()
                        .unwrap_or_else(|_| Usage::failed(provider, "usage fetch panicked"))
                })
                .collect()
        });

        let mut report = RefreshReport::default();
        let mut config_changed = false;

        for mut usage in fetched {
            let provider = usage.provider;
            if usage.rate_limited {
                let previous_backoff = self
                    .cooldowns
                    .get(&provider)
                    .map(|cooldown| cooldown.backoff_secs)
                    .unwrap_or(0);
                let backoff_secs = if previous_backoff == 0 {
                    INITIAL_RATE_LIMIT_BACKOFF_SECS
                } else {
                    (previous_backoff * 2).min(MAX_RATE_LIMIT_BACKOFF_SECS)
                };
                let fallback = now + chrono::Duration::seconds(backoff_secs);
                let until = usage
                    .retry_at
                    .filter(|at| *at > fallback)
                    .unwrap_or(fallback);
                usage.retry_at = Some(until);
                self.cooldowns.insert(
                    provider,
                    Cooldown {
                        until,
                        backoff_secs,
                    },
                );
            } else {
                self.cooldowns.remove(&provider);
            }
            let previous = self.usages.get(&provider);
            let preserved = usage::preserve_last_good(usage, previous);
            let healthy = preserved.err.is_empty() && !preserved.rate_limited;

            if healthy {
                if let Some(history) = self.histories.get_mut(&provider) {
                    history.add(&preserved);
                }
                if !self.demo {
                    let (notices, alerts_changed) = self.check_alerts(&preserved, now);
                    if alerts_changed {
                        config_changed = true;
                    }
                    report.alerts.extend(notices);

                    let (events, milestones_changed) =
                        self.config.milestone_state.observe_thresholds(
                            &preserved,
                            &self.config.primary_thresholds,
                            &self.config.secondary_thresholds,
                        );
                    if milestones_changed {
                        config_changed = true;
                    }
                    report.milestones.extend(events);
                }
            }

            self.usages.insert(provider, preserved.clone());
            report.fetched.push(preserved);
        }

        if config_changed && !self.demo {
            let _ = self.config.save_to(&self.dir);
        }
        report
    }

    /// 80% / 95% and reset alerts. State advances even when alerts are muted so
    /// a later enable does not replay old events.
    fn check_alerts(&mut self, usage: &Usage, now: DateTime<Utc>) -> (Vec<AlertNotice>, bool) {
        let mut notices = Vec::new();
        let mut changed = false;
        let mut state = self.config.alert_state.clone();
        for window in usage.all() {
            let cycle = window
                .resets_at
                .map(|at| at.timestamp() / 60 * 60)
                .unwrap_or(0);
            // Claude's unqualified keys predate multi-provider support and must
            // remain readable. Every newer provider is namespaced so equal
            // window names (for example OpenCode and Cursor `monthly`) cannot
            // overwrite one another.
            let key = match usage.provider {
                Provider::Claude => window.key.clone(),
                provider => format!("{}/{}", provider.key(), window.key),
            };
            let previous = state.get(&key).cloned();
            let mut current = AlertState {
                resets_at: cycle,
                level: 0,
                percent: window.percent,
            };
            if let Some(previous) = &previous {
                if previous.resets_at == cycle {
                    current.level = previous.level;
                }
                if previous.resets_at != cycle
                    && previous.level > 0
                    && window.percent < previous.percent
                    && self.config.alerts
                {
                    notices.push(AlertNotice {
                        title: format!(
                            "{} {} limit has reset",
                            usage.provider.name(),
                            window.label
                        ),
                        body: format!("You're back to {:.0}% used.", window.percent),
                        warn: false,
                    });
                }
            }
            let level = if window.percent >= 95.0 {
                95
            } else if window.percent >= 80.0 {
                80
            } else {
                0
            };
            if level > current.level {
                if self.config.alerts {
                    notices.push(AlertNotice {
                        title: format!(
                            "{} {} at {:.0}%",
                            usage.provider.name(),
                            window.label,
                            window.percent
                        ),
                        body: alerts::reset_text(window.resets_at, now),
                        warn: true,
                    });
                }
                current.level = level;
            }
            if previous.as_ref() != Some(&current) {
                state.insert(key, current);
                changed = true;
            }
        }
        if changed {
            self.config.alert_state = state;
        }
        (notices, changed)
    }

    /// Apply validated edits from the native tray menu.
    pub fn apply_settings(&mut self, settings: Settings) -> Result<(), String> {
        let mut next = self.config.clone();
        settings.apply_to(&mut next);
        next.validate()?;
        next.normalize();
        self.config = next;
        if self.demo {
            return Ok(());
        }
        self.config.save_to(&self.dir)
    }

    pub fn set_provider_mode(&mut self, mode: &str) -> Result<(), String> {
        if !usage::valid_mode(mode) {
            return Err("Providers must be claude, codex or both".into());
        }
        let mut settings = Settings::from(&self.config);
        settings.providers = mode.to_string();
        self.apply_settings(settings)
    }

    pub fn toggle_milestones(&mut self) -> Result<bool, String> {
        let mut settings = Settings::from(&self.config);
        settings.milestone_popups = !settings.milestone_popups;
        let enabled = settings.milestone_popups;
        self.apply_settings(settings)?;
        Ok(enabled)
    }

    pub fn snooze(&mut self, minutes: i64) -> Result<(), String> {
        self.config.snooze_until =
            (Utc::now() + chrono::Duration::minutes(minutes.max(0))).timestamp();
        if self.demo {
            return Ok(());
        }
        self.config.save_to(&self.dir)
    }

    pub fn clear_snooze(&mut self) -> Result<(), String> {
        self.config.snooze_until = 0;
        if self.demo {
            return Ok(());
        }
        self.config.save_to(&self.dir)
    }

    pub fn settings(&self) -> Settings {
        Settings::from(&self.config)
    }

    pub fn snapshot(&self) -> UiState {
        self.snapshot_at(Utc::now())
    }

    pub fn snapshot_at(&self, now: DateTime<Utc>) -> UiState {
        let mut providers = Vec::new();
        for provider in self.config.providers() {
            let usage = self
                .usages
                .get(&provider)
                .cloned()
                .unwrap_or_else(|| Usage::new(provider));
            let history = self.histories.get(&provider);
            let samples24 = history
                .map(|h| h.since(now - chrono::Duration::hours(24)))
                .unwrap_or_default();
            let samples7d = history
                .map(|h| h.since(now - chrono::Duration::days(7)))
                .unwrap_or_default();
            let (primary_forecast, secondary_forecast) = if self.config.show_forecast {
                let estimate = |secondary| {
                    insights::estimate(&usage, &samples7d, secondary, now)
                        .map(|forecast| forecast.text())
                };
                (estimate(false), estimate(true))
            } else {
                (None, None)
            };
            providers.push(ProviderView {
                provider,
                freshness: insights::freshness(&usage, now),
                health: insights::health(&usage, now, Duration::from_secs(15 * 60)),
                primary_forecast,
                secondary_forecast,
                history24: downsample(&samples24, 180),
                history7d: downsample(&samples7d, 180),
                usage,
            });
        }
        UiState {
            settings: Settings::from(&self.config),
            providers,
            version: crate::version().to_string(),
            demo: self.demo,
            platform: std::env::consts::OS.to_string(),
        }
    }
}

fn downsample(samples: &[Sample], max: usize) -> Vec<Sample> {
    if samples.len() <= max {
        return samples.to_vec();
    }
    let stride = samples.len().div_ceil(max);
    let mut out: Vec<Sample> = samples.iter().step_by(stride).copied().collect();
    if let Some(last) = samples.last() {
        if out.last().map(|s| s.t) != Some(last.t) {
            out.push(*last);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::usage::Window;

    fn temp_dir(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("burnbar-service-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    fn healthy(provider: Provider, percent: f64) -> Usage {
        let now = Utc::now();
        let mut usage = Usage::new(provider);
        usage.updated_at = Some(now);
        usage.five_hour = Some(
            Window::new("session", "Session", percent)
                .with_reset(Some(now + chrono::Duration::hours(1)))
                .with_length(Duration::from_secs(5 * 3600)),
        );
        usage.seven_day = Some(
            Window::new("weekly_all", "Weekly", percent / 2.0)
                .with_reset(Some(now + chrono::Duration::days(3)))
                .with_length(Duration::from_secs(7 * 86400)),
        );
        usage
    }

    #[test]
    fn refresh_records_history_and_milestones_then_persists() {
        let dir = temp_dir("refresh");
        let mut service = Service::new(&dir);
        let report = service.refresh_with(|p| healthy(p, 25.0));
        assert_eq!(report.fetched.len(), 2, "both providers by default");
        assert!(report.milestones.is_empty(), "first reading is a baseline");
        let report = service.refresh_with(|p| healthy(p, 35.0));
        // Each provider's session window crosses 30%; the weekly window's
        // 12.5% -> 17.5% move does not reach the next 10% milestone.
        assert_eq!(report.milestones.len(), 2);
        assert!(report
            .milestones
            .iter()
            .any(|event| event.level == 30 && event.label == "5h"));
        assert!(dir.join("config.json").exists());
        assert!(dir.join("history.jsonl").exists());
        assert!(dir.join("history-codex.jsonl").exists());

        let reloaded = Service::new(&dir);
        assert_eq!(reloaded.config.milestone_state.len(), 4);
        assert_eq!(reloaded.config.milestone_state["claude/session"].level, 30);
        assert_eq!(reloaded.history(Provider::Claude).unwrap().all().len(), 2);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn failed_reads_keep_last_good_and_do_not_add_history() {
        let dir = temp_dir("failure");
        let mut service = Service::new(&dir);
        service.refresh_with(|p| healthy(p, 42.0));
        let before = service.history(Provider::Claude).unwrap().all().len();
        let report = service.refresh_with(|p| Usage::failed(p, "network error: offline"));
        assert_eq!(report.fetched.len(), 2);
        assert_eq!(
            service
                .usage(Provider::Claude)
                .unwrap()
                .five_hour
                .as_ref()
                .unwrap()
                .percent,
            42.0
        );
        assert!(service
            .usage(Provider::Claude)
            .unwrap()
            .updated_at
            .is_some());
        assert_eq!(
            service.history(Provider::Claude).unwrap().all().len(),
            before
        );
        assert!(service
            .snapshot()
            .providers
            .iter()
            .all(|p| p.health == "Offline / connection error"));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn rate_limit_cooldown_skips_repeated_provider_calls() {
        use std::sync::atomic::{AtomicUsize, Ordering};

        let dir = temp_dir("rate-limit");
        let mut service = Service::new(&dir);
        service.config.providers = "claude".into();
        let calls = AtomicUsize::new(0);
        let rate_limited = |provider| {
            calls.fetch_add(1, Ordering::SeqCst);
            Usage {
                provider,
                err: "usage API rate limited, backing off".into(),
                rate_limited: true,
                ..Usage::new(provider)
            }
        };

        let first = service.refresh_with(rate_limited);
        assert_eq!(first.fetched.len(), 1);
        assert!(first.fetched[0].retry_at > Some(Utc::now()));
        let second = service.refresh_with(rate_limited);
        assert!(
            second.fetched.is_empty(),
            "cooldown must suppress another call"
        );
        assert_eq!(calls.load(Ordering::SeqCst), 1);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn alerts_fire_at_80_and_reset_once_per_cycle() {
        let dir = temp_dir("alerts");
        let mut service = Service::new(&dir);
        let report = service.refresh_with(|p| healthy(p, 79.0));
        assert!(report.alerts.is_empty());
        let report = service.refresh_with(|p| healthy(p, 82.0));
        assert_eq!(report.alerts.len(), 2);
        assert!(report.alerts[0].title.contains("at 82%"));
        let report = service.refresh_with(|p| healthy(p, 83.0));
        assert!(report.alerts.is_empty(), "the 80% alert is not repeated");
        let report = service.refresh_with(|p| healthy(p, 96.0));
        assert_eq!(report.alerts.len(), 2, "95% is the next level");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn alert_state_namespaces_equal_window_keys_by_provider() {
        let dir = temp_dir("alert-provider-keys");
        let mut service = Service::new(&dir);
        service.config.providers = "all".into();
        service.refresh_with(|provider| {
            let mut usage = Usage::new(provider);
            usage.five_hour = Some(Window::new("monthly", "Monthly", 50.0));
            usage
        });

        let keys: Vec<_> = service.config.alert_state.keys().cloned().collect();
        assert_eq!(
            keys,
            [
                "codex/monthly",
                "cursor/monthly",
                "monthly",
                "opencode/monthly",
            ]
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn settings_never_wipe_internal_milestone_state() {
        let dir = temp_dir("settings");
        let mut service = Service::new(&dir);
        service.refresh_with(|p| healthy(p, 30.0));
        let before = service.config.milestone_state.clone();
        let mut settings = service.settings();
        settings.theme = "dark".into();
        settings.interval_minutes = 7;
        settings.start_with_windows = false;
        service.apply_settings(settings).unwrap();
        assert_eq!(service.config.theme, "dark");
        assert_eq!(service.config.interval_minutes, 7);
        assert_eq!(service.config.start_with_windows, Some(false));
        assert_eq!(service.config.milestone_state, before);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn invalid_settings_are_rejected_without_saving() {
        let dir = temp_dir("invalid");
        let mut service = Service::new(&dir);
        let mut settings = service.settings();
        settings.popup_seconds = 0;
        assert!(service.apply_settings(settings).is_err());
        assert_eq!(service.config.popup_seconds, 6);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn snapshot_includes_history_and_forecast() {
        let dir = temp_dir("snapshot");
        let mut service = Service::new(&dir);
        service.refresh_with(|p| healthy(p, 20.0));
        let snapshot = service.snapshot();
        assert_eq!(snapshot.providers.len(), 2);
        assert_eq!(snapshot.version, crate::version());
        assert!(!snapshot.providers[0].history24.is_empty());
        assert_eq!(snapshot.settings.primary_thresholds[0], 10);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn demo_mode_needs_no_credentials_and_writes_nothing() {
        let dir = temp_dir("demo");
        let mut service = Service::demo(&dir);
        service.refresh();
        let snapshot = service.snapshot();
        assert!(snapshot.demo);
        assert_eq!(snapshot.providers.len(), 4);
        assert!(snapshot.providers.iter().all(|view| !view.usage.is_empty()));
        assert!(!dir.join("config.json").exists());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
