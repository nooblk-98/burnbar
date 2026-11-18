//! Settings and alert state (Go: `internal/config/config.go`).
//!
//! The on-disk JSON stays compatible with the Go app so existing BurnBar
//! profiles keep their preferences, history and milestone state.

use crate::alerts::{self, Tracker};
use crate::usage;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AlertState {
    pub resets_at: i64,
    #[serde(default)]
    pub level: i32,
    #[serde(default)]
    pub percent: f64,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Config {
    /// A legacy mode name or a comma-separated set of provider keys.
    #[serde(default = "default_providers")]
    pub providers: String,
    #[serde(default = "default_popup_seconds")]
    pub popup_seconds: i32,
    /// One-time behaviour migrations version.
    #[serde(default)]
    pub popup_behavior_version: i32,
    #[serde(default = "default_true")]
    pub milestone_popups: bool,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub milestone_state: Tracker,
    #[serde(default)]
    pub always_show_popup: bool,
    #[serde(default = "default_interval")]
    pub interval_minutes: i32,
    #[serde(default = "default_true")]
    pub alerts: bool,
    /// `None` identifies a legacy profile whose preference still lives in the registry.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub start_with_windows: Option<bool>,

    #[serde(default = "default_thresholds")]
    pub primary_thresholds: Vec<i32>,
    #[serde(default = "default_thresholds")]
    pub secondary_thresholds: Vec<i32>,
    #[serde(default, skip_serializing_if = "is_zero_i64")]
    pub snooze_until: i64,
    #[serde(default)]
    pub quiet_hours: bool,
    #[serde(default = "default_quiet_start")]
    pub quiet_start: i32,
    #[serde(default = "default_quiet_end")]
    pub quiet_end: i32,
    #[serde(default = "default_true")]
    pub respect_windows_quiet: bool,
    #[serde(default = "default_true")]
    pub compact_notices: bool,
    #[serde(default)]
    pub native_notices: bool,
    #[serde(default = "default_true")]
    pub show_forecast: bool,
    #[serde(default = "default_theme")]
    pub theme: String,
    #[serde(default = "default_text_scale")]
    pub text_scale: i32,
    #[serde(default)]
    pub hotkey: bool,
    #[serde(default = "default_true")]
    pub check_updates: bool,
    #[serde(default)]
    pub alert_state: BTreeMap<String, AlertState>,
}

fn default_providers() -> String {
    "both".into()
}
fn default_popup_seconds() -> i32 {
    alerts::DEFAULT_POPUP_SECONDS
}
fn default_interval() -> i32 {
    3
}
fn default_true() -> bool {
    true
}
fn default_theme() -> String {
    "system".into()
}
fn default_text_scale() -> i32 {
    100
}
fn default_quiet_start() -> i32 {
    22 * 60
}
fn default_quiet_end() -> i32 {
    8 * 60
}
fn default_thresholds() -> Vec<i32> {
    alerts::default_thresholds()
}
fn is_zero_i64(value: &i64) -> bool {
    *value == 0
}

impl Default for Config {
    fn default() -> Self {
        Self {
            providers: default_providers(),
            popup_seconds: default_popup_seconds(),
            popup_behavior_version: 1,
            milestone_popups: true,
            milestone_state: Tracker::new(),
            always_show_popup: false,
            interval_minutes: 3,
            alerts: true,
            start_with_windows: Some(true),
            primary_thresholds: default_thresholds(),
            secondary_thresholds: default_thresholds(),
            snooze_until: 0,
            quiet_hours: false,
            quiet_start: default_quiet_start(),
            quiet_end: default_quiet_end(),
            respect_windows_quiet: true,
            compact_notices: true,
            native_notices: false,
            show_forecast: true,
            theme: default_theme(),
            text_scale: default_text_scale(),
            hotkey: false,
            check_updates: true,
            alert_state: BTreeMap::new(),
        }
    }
}

impl Config {
    /// Folder holding settings and history; created if missing.
    pub fn dir() -> PathBuf {
        if let Some(dir) = std::env::var_os("BURNBAR_HOME") {
            let dir = PathBuf::from(dir);
            let _ = std::fs::create_dir_all(&dir);
            return dir;
        }
        let base = dirs::config_dir().unwrap_or_else(|| PathBuf::from("."));
        let dir = base.join("BurnBar");
        if !dir.exists() {
            // Pre-rename builds used "ClaudeUsageMeter"; carry their settings and history over.
            let legacy = base.join("ClaudeUsageMeter");
            if legacy.exists() {
                let _ = std::fs::rename(&legacy, &dir);
            }
        }
        let _ = std::fs::create_dir_all(&dir);
        dir
    }

    pub fn path_in(dir: &Path) -> PathBuf {
        dir.join("config.json")
    }

    pub fn load() -> Config {
        Self::load_from(&Self::dir())
    }

    /// Load from an explicit folder. Kept separate so tests never touch the
    /// user's real profile, and so the desktop app can use `BURNBAR_HOME`.
    pub fn load_from(dir: &Path) -> Config {
        let path = Self::path_in(dir);
        let mut config = Config::default();
        if let Ok(bytes) = std::fs::read(&path) {
            // Existing profiles without these fields keep their registry choice
            // and go through the one-time popup migration.
            config.start_with_windows = None;
            config.popup_behavior_version = 0;
            if let Ok(parsed) = serde_json::from_slice::<Config>(&bytes) {
                config = parsed;
            }
        }
        // One-time migration away from the previous always-visible default. A
        // later explicit "always show" choice is retained by this version.
        if config.popup_behavior_version < 1 {
            config.always_show_popup = false;
            config.popup_behavior_version = 1;
        }
        config.normalize();
        config
    }

    pub fn save_to(&self, dir: &Path) -> Result<(), String> {
        std::fs::create_dir_all(dir).map_err(|e| e.to_string())?;
        let path = Self::path_in(dir);
        let bytes = serde_json::to_vec_pretty(self).map_err(|e| e.to_string())?;
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, bytes).map_err(|e| e.to_string())?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o600));
        }
        std::fs::rename(&tmp, &path).map_err(|e| e.to_string())
    }

    /// Reproduce `Load`'s tolerant validation: bad values fall back to safe
    /// defaults instead of making the whole profile unreadable.
    pub fn normalize(&mut self) {
        if !(1..=300).contains(&self.popup_seconds) {
            self.popup_seconds = alerts::DEFAULT_POPUP_SECONDS;
        }
        self.primary_thresholds = alerts::normalize_thresholds(&self.primary_thresholds);
        self.secondary_thresholds = alerts::normalize_thresholds(&self.secondary_thresholds);
        if !(0..1440).contains(&self.quiet_start) {
            self.quiet_start = default_quiet_start();
        }
        if !(0..1440).contains(&self.quiet_end) {
            self.quiet_end = default_quiet_end();
        }
        if !matches!(self.text_scale, 100 | 125 | 150) {
            self.text_scale = 100;
        }
        if !matches!(self.theme.as_str(), "system" | "light" | "dark") {
            self.theme = "system".into();
        }
        if !usage::valid_mode(&self.providers) {
            self.providers = "both".into();
        }
        if self.interval_minutes < 1 {
            self.interval_minutes = 3;
        }
    }

    pub fn providers(&self) -> Vec<usage::Provider> {
        usage::providers_for_mode(&self.providers)
    }

    /// True when snooze or scheduled quiet hours should suppress notices.
    /// Quiet hours are wall-clock times in the user's local time zone.
    pub fn quiet_now(&self) -> bool {
        alerts::quiet(
            chrono::Local::now(),
            self.snooze_until,
            self.quiet_hours,
            self.quiet_start,
            self.quiet_end,
        )
    }

    /// Validate values edited from the tray menu before persisting them.
    pub fn validate(&mut self) -> Result<(), String> {
        self.primary_thresholds = alerts::normalize_thresholds(&self.primary_thresholds);
        self.secondary_thresholds = alerts::normalize_thresholds(&self.secondary_thresholds);
        if !(1..=300).contains(&self.popup_seconds) {
            return Err("Popup seconds must be between 1 and 300".into());
        }
        if self.interval_minutes < 1 || self.interval_minutes > 1440 {
            return Err("Refresh interval must be between 1 and 1440 minutes".into());
        }
        if !(0..1440).contains(&self.quiet_start) || !(0..1440).contains(&self.quiet_end) {
            return Err("Quiet hours must be valid times of day".into());
        }
        if !usage::valid_mode(&self.providers) {
            return Err("At least one recognized provider must be enabled".into());
        }
        if !matches!(self.theme.as_str(), "system" | "light" | "dark") {
            return Err("Theme must be system, light or dark".into());
        }
        if !matches!(self.text_scale, 100 | 125 | 150) {
            return Err("Text size must be 100, 125 or 150".into());
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("burnbar-config-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn defaults_match_the_go_app() {
        let c = Config::default();
        assert_eq!(c.providers, "both");
        assert_eq!(c.popup_seconds, 6);
        assert_eq!(c.interval_minutes, 3);
        assert!(c.milestone_popups);
        assert_eq!(c.start_with_windows, Some(true));
        assert_eq!(
            c.primary_thresholds,
            vec![10, 20, 30, 40, 50, 60, 70, 80, 90, 100]
        );
        assert_eq!(c.quiet_start, 22 * 60);
        assert_eq!(c.quiet_end, 8 * 60);
    }

    #[test]
    fn round_trips_and_normalizes_bad_values() {
        let dir = temp_dir("roundtrip");
        let c = Config {
            providers: "codex".into(),
            popup_seconds: 900,
            primary_thresholds: vec![50, 50, 0, 101, 80],
            text_scale: 33,
            ..Config::default()
        };
        c.save_to(&dir).unwrap();
        let loaded = Config::load_from(&dir);
        assert_eq!(loaded.providers, "codex");
        assert_eq!(loaded.popup_seconds, 6);
        assert_eq!(loaded.primary_thresholds, vec![50, 80]);
        assert_eq!(loaded.text_scale, 100);
        assert!(Config::path_in(&dir).exists());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn reads_a_go_profile_and_applies_the_popup_migration() {
        let dir = temp_dir("legacy");
        std::fs::write(
            Config::path_in(&dir),
            r#"{
              "providers": "claude",
              "popupSeconds": 10,
              "milestonePopups": true,
              "alwaysShowPopup": true,
              "intervalMinutes": 5,
              "alerts": true,
              "primaryThresholds": [25, 50, 75, 100],
              "secondaryThresholds": [50, 80, 95],
              "snoozeUntil": 123,
              "quietHours": true,
              "quietStart": 1380,
              "quietEnd": 420,
              "theme": "dark",
              "textScale": 125,
              "milestoneState": {"claude/session": {"reset": 42, "length": 18000, "level": 20, "highWater": 23.5}},
              "alertState": {"session": {"resetsAt": 42, "level": 80, "percent": 82}}
            }"#,
        )
        .unwrap();
        let c = Config::load_from(&dir);
        assert_eq!(c.providers, "claude");
        assert_eq!(c.popup_seconds, 10);
        assert!(!c.always_show_popup, "legacy always-show migrates off");
        assert_eq!(c.start_with_windows, None);
        assert_eq!(c.quiet_start, 1380, "overnight quiet hours survive");
        assert_eq!(c.milestone_state["claude/session"].high_water, Some(23.5));
        assert_eq!(c.alert_state["session"].level, 80);
        assert_eq!(c.interval_minutes, 5);
        assert_eq!(c.popup_behavior_version, 1);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn validation_rejects_out_of_range_edits() {
        let mut c = Config {
            popup_seconds: 0,
            ..Config::default()
        };
        assert!(c.validate().is_err());
        let mut c = Config {
            interval_minutes: 0,
            ..Config::default()
        };
        assert!(c.validate().is_err());
        let mut c = Config {
            providers: "nope".into(),
            ..Config::default()
        };
        assert!(c.validate().is_err());
    }

    #[test]
    fn quiet_hours_cover_windows_and_snooze() {
        use chrono::{Local, TimeZone};
        let at = |h, m| Local.with_ymd_and_hms(2026, 1, 2, h, m, 0).unwrap();
        assert!(alerts::quiet(at(23, 0), 0, true, 22 * 60, 8 * 60));
        assert!(alerts::quiet(at(7, 59), 0, true, 22 * 60, 8 * 60));
        assert!(!alerts::quiet(at(8, 0), 0, true, 22 * 60, 8 * 60));
        assert!(!alerts::quiet(at(12, 0), 0, true, 22 * 60, 8 * 60));
        assert!(alerts::quiet(at(12, 0), at(12, 1).timestamp(), false, 0, 0));
    }
}
