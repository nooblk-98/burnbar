//! Milestone tracking and notification preferences (Go: `internal/alerts/`).

use crate::usage::{Provider, Usage};
use chrono::{DateTime, TimeZone, Timelike, Utc};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::ops::{Deref, DerefMut};

pub const DEFAULT_POPUP_SECONDS: i32 = 6;

pub fn default_thresholds() -> Vec<i32> {
    vec![10, 20, 30, 40, 50, 60, 70, 80, 90, 100]
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MilestoneState {
    pub reset: i64,
    pub length: i64,
    pub level: i32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub high_water: Option<f64>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Milestone {
    pub provider: Provider,
    pub label: String,
    pub level: i32,
    /// 0 = primary/session, 1 = secondary/weekly.
    pub window: usize,
    pub percent: f64,
    pub resets_at: Option<DateTime<Utc>>,
}

/// Persisted per-window milestone state. Serialized as the same JSON object the
/// Go app writes (`{"claude/session": {...}}`).
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(transparent)]
pub struct Tracker(pub BTreeMap<String, MilestoneState>);

impl Deref for Tracker {
    type Target = BTreeMap<String, MilestoneState>;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl DerefMut for Tracker {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl Tracker {
    pub fn new() -> Self {
        Self(BTreeMap::new())
    }

    pub fn observe(&mut self, usage: &Usage) -> (Vec<Milestone>, bool) {
        self.observe_thresholds(usage, &default_thresholds(), &default_thresholds())
    }

    /// Use the first valid reading of each quota window as a baseline, not a
    /// backlog of alerts. Only a new high-water milestone in the same window
    /// alerts; persisting the state prevents polling, corrections and restarts
    /// from replaying it.
    pub fn observe_thresholds(
        &mut self,
        usage: &Usage,
        primary: &[i32],
        secondary: &[i32],
    ) -> (Vec<Milestone>, bool) {
        let mut events = Vec::new();
        if !usage.err.is_empty() || usage.rate_limited {
            return (events, false);
        }
        let mut changed = false;
        let windows = [usage.five_hour.as_ref(), usage.seven_day.as_ref()];
        for (index, window) in windows.into_iter().enumerate() {
            let Some(window) = window else { continue };
            if !(0.0..=100.0).contains(&window.percent) {
                continue;
            }
            let fallback = if index == 0 { "primary" } else { "secondary" };
            let key_part = if window.key.is_empty() {
                fallback.to_string()
            } else {
                window.key.clone()
            };
            let key = format!("{}/{}", usage.provider.key(), key_part);
            let reset = window
                .resets_at
                .map(|at| {
                    at.with_second(0)
                        .unwrap_or(at)
                        .with_nanosecond(0)
                        .unwrap_or(at)
                        .timestamp()
                })
                .unwrap_or(0);
            let length = window.length_secs;

            let previous = self.0.get(&key).cloned();
            let seen = previous.is_some();
            let high = previous
                .as_ref()
                .map(|prev| prev.high_water.unwrap_or(prev.level as f64))
                .unwrap_or(0.0);

            let mut same = seen
                && previous.as_ref().map(|prev| prev.reset) == Some(reset)
                && previous.as_ref().map(|prev| prev.length) == Some(length);
            if same && reset == 0 && window.percent < 10.0 && high >= 10.0 {
                same = false;
            }

            let mut next_high = window.percent;
            if same {
                let thresholds = if index == 0 { primary } else { secondary };
                let mut level = 0;
                for value in thresholds {
                    let threshold = *value as f64;
                    if threshold > high && threshold <= window.percent {
                        level = level.max(*value);
                    }
                }
                if level > 0 {
                    events.push(Milestone {
                        provider: usage.provider,
                        label: window.short_label(fallback),
                        level,
                        window: index,
                        percent: window.percent,
                        resets_at: window.resets_at,
                    });
                }
                next_high = high.max(window.percent);
            }

            let current = MilestoneState {
                reset,
                length,
                level: (next_high / 10.0) as i32 * 10,
                high_water: Some(next_high),
            };
            let differs = previous.as_ref().map_or(true, |prev| {
                prev.reset != current.reset
                    || prev.length != current.length
                    || high != next_high
                    || prev.high_water.is_none()
            });
            if differs {
                self.0.insert(key, current);
                changed = true;
            }
        }
        (events, changed)
    }
}

pub fn parse_thresholds(text: &str) -> Result<Vec<i32>, String> {
    let mut values = Vec::new();
    let mut seen = BTreeMap::new();
    for part in text.split([',', ';', ' ']).filter(|p| !p.is_empty()) {
        let part = part.trim_end_matches('%');
        let value: i32 = part
            .parse()
            .map_err(|_| "thresholds must be whole percentages from 1 to 100".to_string())?;
        if !(1..=100).contains(&value) {
            return Err("thresholds must be whole percentages from 1 to 100".into());
        }
        if seen.insert(value, true).is_none() {
            values.push(value);
        }
    }
    values.sort_unstable();
    Ok(values)
}

pub fn threshold_text(values: &[i32]) -> String {
    values
        .iter()
        .map(i32::to_string)
        .collect::<Vec<_>>()
        .join(", ")
}

pub fn normalize_thresholds(values: &[i32]) -> Vec<i32> {
    let clean: Vec<i32> = values
        .iter()
        .copied()
        .filter(|v| (1..=100).contains(v))
        .collect();
    parse_thresholds(&threshold_text(&clean)).unwrap_or_default()
}

/// Local wall-clock quiet check, including overnight ranges. Equal endpoints
/// mean a full quiet day; the enable switch distinguishes this from disabled.
pub fn quiet<Tz: TimeZone>(
    now: DateTime<Tz>,
    snooze_until: i64,
    enabled: bool,
    start: i32,
    end: i32,
) -> bool {
    if snooze_until > now.timestamp() {
        return true;
    }
    if !enabled {
        return false;
    }
    let minutes = (now.hour() * 60 + now.minute()) as i32;
    if start == end {
        return true;
    }
    if start < end {
        minutes >= start && minutes < end
    } else {
        minutes >= start || minutes < end
    }
}

/// Reset text used by milestone and alert notifications.
pub fn reset_text(resets_at: Option<DateTime<Utc>>, now: DateTime<Utc>) -> String {
    let Some(resets_at) = resets_at else {
        return String::new();
    };
    let seconds = (resets_at - now).num_seconds();
    if seconds <= 0 {
        return "Resetting now".into();
    }
    let minutes = seconds / 60;
    if minutes < 60 {
        return format!("Resets in {}m", minutes.max(1));
    }
    let hours = minutes / 60;
    if hours < 24 {
        let extra = minutes % 60;
        return if extra > 0 {
            format!("Resets in {}h {:02}m", hours, extra)
        } else {
            format!("Resets in {}h", hours)
        };
    }
    let days = hours / 24;
    let extra = hours % 24;
    if extra > 0 {
        format!("Resets in {}d {}h", days, extra)
    } else {
        format!("Resets in {}d", days)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::usage::Window;
    use chrono::TimeZone;

    fn usage(percent: f64, reset: DateTime<Utc>) -> Usage {
        let mut u = Usage::new(Provider::Claude);
        u.updated_at = Some(reset);
        u.five_hour = Some(
            Window::new("session", "Session (5h)", percent)
                .with_reset(Some(reset))
                .with_length(std::time::Duration::from_secs(5 * 3600)),
        );
        u
    }

    #[test]
    fn first_reading_is_a_baseline_not_a_backlog() {
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 3, 4, 5).unwrap();
        let mut tracker = Tracker::new();
        let (events, changed) = tracker.observe(&usage(75.0, now + chrono::Duration::hours(1)));
        assert!(events.is_empty());
        assert!(changed);
        assert_eq!(tracker["claude/session"].level, 70);
    }

    #[test]
    fn crossing_a_threshold_emits_one_event_and_high_water_wins() {
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 3, 4, 5).unwrap();
        let reset = now + chrono::Duration::hours(1);
        let mut tracker = Tracker::new();
        tracker.observe(&usage(12.0, reset));
        let (events, changed) = tracker.observe(&usage(35.0, reset));
        assert!(changed);
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].level, 30);
        assert_eq!(events[0].provider, Provider::Claude);
        assert_eq!(events[0].label, "5h");
        // A correction downwards does not replay the same threshold.
        let (events, _) = tracker.observe(&usage(31.0, reset));
        assert!(events.is_empty());
        let (events, _) = tracker.observe(&usage(45.0, reset));
        assert_eq!(events[0].level, 40);
    }

    #[test]
    fn a_new_reset_rearms_the_tracker() {
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 3, 4, 5).unwrap();
        let mut tracker = Tracker::new();
        tracker.observe(&usage(50.0, now + chrono::Duration::hours(1)));
        let (events, _) = tracker.observe(&usage(15.0, now + chrono::Duration::hours(6)));
        assert!(
            events.is_empty(),
            "a new window starts from a baseline again"
        );
        let (events, _) = tracker.observe(&usage(25.0, now + chrono::Duration::hours(6)));
        assert_eq!(events[0].level, 20);
    }

    #[test]
    fn custom_thresholds_are_respected() {
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 3, 4, 5).unwrap();
        let reset = now + chrono::Duration::hours(1);
        let mut tracker = Tracker::new();
        tracker.observe_thresholds(&usage(10.0, reset), &[50, 80, 95], &[50, 80, 95]);
        let (events, _) =
            tracker.observe_thresholds(&usage(60.0, reset), &[50, 80, 95], &[50, 80, 95]);
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].level, 50);
    }

    #[test]
    fn errors_and_rate_limits_never_move_state() {
        let now = Utc::now();
        let mut u = usage(70.0, now + chrono::Duration::hours(1));
        u.err = "offline".into();
        let mut tracker = Tracker::new();
        let (events, changed) = tracker.observe(&u);
        assert!(events.is_empty() && !changed);
    }

    #[test]
    fn parses_sorts_and_dedupes_thresholds() {
        assert_eq!(
            parse_thresholds("80, 10, 20% ; 10 50").unwrap(),
            vec![10, 20, 50, 80]
        );
        assert_eq!(parse_thresholds("").unwrap(), Vec::<i32>::new());
        assert!(parse_thresholds("0,50").is_err());
        assert!(parse_thresholds("abc").is_err());
        assert_eq!(normalize_thresholds(&[150, 0, 75, 75]), vec![75]);
    }

    #[test]
    fn quiet_hours_cover_overnight_ranges() {
        // Equal endpoints mean the whole day is quiet.
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 12, 0, 0).unwrap();
        assert!(quiet(now, 0, true, 600, 600));
        assert!(!quiet(now, 0, false, 600, 600));
        assert!(quiet(now, now.timestamp() + 1, false, 0, 0));
    }

    #[test]
    fn reset_text_is_human_readable() {
        let now = Utc.with_ymd_and_hms(2026, 1, 2, 12, 0, 0).unwrap();
        assert_eq!(
            reset_text(Some(now + chrono::Duration::minutes(42)), now),
            "Resets in 42m"
        );
        assert_eq!(
            reset_text(Some(now + chrono::Duration::hours(3)), now),
            "Resets in 3h"
        );
        assert_eq!(
            reset_text(Some(now + chrono::Duration::days(2)), now),
            "Resets in 2d"
        );
    }
}
